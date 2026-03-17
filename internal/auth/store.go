package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/onecli/onecli-cli/internal/config"
)

// ErrAPIKeyNotFound is returned when no API key is stored.
var ErrAPIKeyNotFound = errors.New("API key not found")

// Keychain abstracts OS keychain access for API key storage.
type Keychain interface {
	Get(service string) (string, error)
	Set(service string, value string) error
	Delete(service string) error
}

// Store provides API key persistence.
// Tries env var first, then keychain, then file-based storage.
type Store struct {
	keychain       Keychain
	credentialsDir string
}

// NewStore creates an API key store. keychain may be nil, in which case
// only file-based storage is used.
func NewStore(keychain Keychain, credentialsDir string) *Store {
	return &Store{
		keychain:       keychain,
		credentialsDir: credentialsDir,
	}
}

// Load returns the stored API key.
// Precedence: ONECLI_API_KEY env var > keychain > file.
func (s *Store) Load() (string, error) {
	// 1. Environment variable (highest precedence).
	if v := config.APIKeyFromEnv(); v != "" {
		return v, nil
	}

	// 2. OS keychain.
	if s.keychain != nil {
		service := config.KeychainService()
		key, err := s.keychain.Get(service)
		if err == nil && key != "" {
			return key, nil
		}
	}

	// 3. File fallback.
	return s.loadFile()
}

// Save persists an API key. Tries keychain first, then file.
func (s *Store) Save(apiKey string) error {
	if s.keychain != nil {
		service := config.KeychainService()
		if err := s.keychain.Set(service, apiKey); err == nil {
			_ = os.Remove(s.filePath())
			return nil
		}
	}
	return s.saveFile(apiKey)
}

// Delete removes the stored API key from both keychain and file.
func (s *Store) Delete() error {
	keychainDeleted := false
	if s.keychain != nil {
		service := config.KeychainService()
		if err := s.keychain.Delete(service); err == nil {
			keychainDeleted = true
		}
	}

	fileDeleted := false
	if err := os.Remove(s.filePath()); err == nil {
		fileDeleted = true
	}

	if !keychainDeleted && !fileDeleted {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (s *Store) filePath() string {
	return filepath.Join(s.credentialsDir, "api-key")
}

func (s *Store) loadFile() (string, error) {
	data, err := os.ReadFile(s.filePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrAPIKeyNotFound
		}
		return "", fmt.Errorf("reading API key file: %w", err)
	}
	key := string(data)
	if key == "" {
		return "", ErrAPIKeyNotFound
	}
	return key, nil
}

func (s *Store) saveFile(apiKey string) error {
	if err := os.MkdirAll(s.credentialsDir, 0o700); err != nil {
		return fmt.Errorf("creating credentials directory: %w", err)
	}
	if err := os.WriteFile(s.filePath(), []byte(apiKey), 0o600); err != nil {
		return fmt.Errorf("writing API key file: %w", err)
	}
	return nil
}
