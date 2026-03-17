package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(nil, dir)

	// Save
	if err := store.Save("oc_test123"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file permissions
	info, err := os.Stat(filepath.Join(dir, "api-key"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}

	// Load
	key, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if key != "oc_test123" {
		t.Errorf("Load = %q, want %q", key, "oc_test123")
	}
}

func TestStoreLoadNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(nil, dir)

	_, err := store.Load()
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Errorf("expected ErrAPIKeyNotFound, got %v", err)
	}
}

func TestStoreDeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(nil, dir)

	_ = store.Save("oc_test123")
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := store.Load()
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Errorf("after delete, Load should return ErrAPIKeyNotFound, got %v", err)
	}
}

func TestStoreDeleteWhenNothingStored(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(nil, dir)

	err := store.Delete()
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Errorf("expected ErrAPIKeyNotFound, got %v", err)
	}
}

func TestStoreSaveOverwrites(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(nil, dir)

	_ = store.Save("oc_first")
	_ = store.Save("oc_second")

	key, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if key != "oc_second" {
		t.Errorf("got %q, want %q", key, "oc_second")
	}
}

func TestStoreLoadEmptyFileReturnsNotFound(t *testing.T) {
	dir := t.TempDir()
	// Write empty file
	_ = os.WriteFile(filepath.Join(dir, "api-key"), []byte(""), 0o600)

	store := NewStore(nil, dir)
	_, err := store.Load()
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Errorf("empty file should return ErrAPIKeyNotFound, got %v", err)
	}
}

func TestStoreEnvVarTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(nil, dir)

	_ = store.Save("oc_fromfile")
	t.Setenv("ONECLI_API_KEY", "oc_fromenv")

	key, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if key != "oc_fromenv" {
		t.Errorf("got %q, want env var value %q", key, "oc_fromenv")
	}
}

// mockKeychain implements the Keychain interface for testing.
type mockKeychain struct {
	store map[string]string
}

func (m *mockKeychain) Get(service string) (string, error) {
	v, ok := m.store[service]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func (m *mockKeychain) Set(service string, value string) error {
	m.store[service] = value
	return nil
}

func (m *mockKeychain) Delete(service string) error {
	if _, ok := m.store[service]; !ok {
		return errors.New("not found")
	}
	delete(m.store, service)
	return nil
}

func TestStoreKeychainPreferredOverFile(t *testing.T) {
	dir := t.TempDir()
	kc := &mockKeychain{store: make(map[string]string)}
	store := NewStore(kc, dir)

	if err := store.Save("oc_keychain123"); err != nil {
		t.Fatal(err)
	}

	// File should NOT exist since keychain succeeded
	if _, err := os.Stat(filepath.Join(dir, "api-key")); !os.IsNotExist(err) {
		t.Error("file should not exist when keychain save succeeds")
	}

	key, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if key != "oc_keychain123" {
		t.Errorf("got %q, want %q", key, "oc_keychain123")
	}
}

func TestStoreKeychainFallsBackToFile(t *testing.T) {
	dir := t.TempDir()
	// Keychain that always fails
	kc := &failingKeychain{}
	store := NewStore(kc, dir)

	if err := store.Save("oc_filefallback"); err != nil {
		t.Fatal(err)
	}

	// Should have fallen back to file
	key, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if key != "oc_filefallback" {
		t.Errorf("got %q, want %q", key, "oc_filefallback")
	}
}

type failingKeychain struct{}

func (f *failingKeychain) Get(string) (string, error) { return "", errors.New("keychain unavailable") }
func (f *failingKeychain) Set(string, string) error   { return errors.New("keychain unavailable") }
func (f *failingKeychain) Delete(string) error        { return errors.New("keychain unavailable") }
