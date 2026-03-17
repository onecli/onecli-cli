package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvDefault(t *testing.T) {
	t.Setenv("ONECLI_ENV", "")
	if got := Env(); got != "production" {
		t.Errorf("Env() = %q, want production", got)
	}
}

func TestEnvDev(t *testing.T) {
	t.Setenv("ONECLI_ENV", "dev")
	if got := Env(); got != "dev" {
		t.Errorf("Env() = %q, want dev", got)
	}
}

func TestEnvUnknownValueDefaultsToProduction(t *testing.T) {
	t.Setenv("ONECLI_ENV", "staging")
	if got := Env(); got != "production" {
		t.Errorf("Env() = %q, want production (unknown value should default)", got)
	}
}

func TestAPIHostDefault(t *testing.T) {
	t.Setenv("ONECLI_API_HOST", "")
	// Env var not set and no config file → default
	got := APIHost()
	if got != "https://app.onecli.sh" {
		t.Errorf("APIHost() = %q, want default", got)
	}
}

func TestAPIHostEnvOverride(t *testing.T) {
	t.Setenv("ONECLI_API_HOST", "http://localhost:3000")
	got := APIHost()
	if got != "http://localhost:3000" {
		t.Errorf("APIHost() = %q, want env var value", got)
	}
}

func TestAPIKeyFromEnv(t *testing.T) {
	t.Setenv("ONECLI_API_KEY", "oc_envkey")
	if got := APIKeyFromEnv(); got != "oc_envkey" {
		t.Errorf("APIKeyFromEnv() = %q, want oc_envkey", got)
	}
}

func TestAPIKeyFromEnvEmpty(t *testing.T) {
	t.Setenv("ONECLI_API_KEY", "")
	if got := APIKeyFromEnv(); got != "" {
		t.Errorf("APIKeyFromEnv() = %q, want empty", got)
	}
}

func TestKeychainServiceProd(t *testing.T) {
	t.Setenv("ONECLI_ENV", "")
	if got := KeychainService(); got != "onecli-api-key" {
		t.Errorf("KeychainService() = %q, want prod name", got)
	}
}

func TestKeychainServiceDev(t *testing.T) {
	t.Setenv("ONECLI_ENV", "dev")
	if got := KeychainService(); got != "onecli-api-key-dev" {
		t.Errorf("KeychainService() = %q, want dev name", got)
	}
}

func TestGetConfigValueUnknownKey(t *testing.T) {
	_, err := GetConfigValue("nonexistent")
	if !errors.Is(err, ErrUnknownConfigKey) {
		t.Errorf("expected ErrUnknownConfigKey, got %v", err)
	}
}

func TestSetConfigValueUnknownKey(t *testing.T) {
	err := SetConfigValue("nonexistent", "value")
	if !errors.Is(err, ErrUnknownConfigKey) {
		t.Errorf("expected ErrUnknownConfigKey, got %v", err)
	}
}

func TestSetConfigValueEmptyURLRejected(t *testing.T) {
	err := SetConfigValue("api-host", "")
	if !errors.Is(err, ErrInvalidConfigValue) {
		t.Errorf("expected ErrInvalidConfigValue, got %v", err)
	}
}

func TestConfigFileRoundTrip(t *testing.T) {
	// Use temp dir as home to isolate config file
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_ENV", "")
	t.Setenv("ONECLI_API_HOST", "")

	// Set a value
	if err := SetConfigValue("api-host", "http://custom:8080"); err != nil {
		t.Fatal(err)
	}

	// Read it back
	val, err := GetConfigValue("api-host")
	if err != nil {
		t.Fatal(err)
	}
	if val != "http://custom:8080" {
		t.Errorf("got %q, want %q", val, "http://custom:8080")
	}

	// Verify file exists
	path := filepath.Join(dir, ".onecli", "config.json")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file should exist at %s", path)
	}
}

func TestGetConfigValueAPIHostEnvTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_ENV", "")

	// Set file value
	_ = SetConfigValue("api-host", "http://from-file:8080")

	// Env var should win
	t.Setenv("ONECLI_API_HOST", "http://from-env:9090")
	val, err := GetConfigValue("api-host")
	if err != nil {
		t.Fatal(err)
	}
	if val != "http://from-env:9090" {
		t.Errorf("got %q, env var should take precedence", val)
	}
}

func TestGetConfigValueDefaultWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_ENV", "")
	t.Setenv("ONECLI_API_HOST", "")

	val, err := GetConfigValue("api-host")
	if err != nil {
		t.Fatal(err)
	}
	if val != "https://app.onecli.sh" {
		t.Errorf("got %q, want default", val)
	}
}
