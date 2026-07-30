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
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ONECLI_ENV", "")
	t.Setenv("ONECLI_API_HOST", "")
	// Env var not set and no config file → default
	got := APIHost()
	if got != "https://api.onecli.sh" {
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

func TestProjectDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_PROJECT", "")

	if got := Project(); got != "" {
		t.Errorf("Project() = %q, want empty", got)
	}
}

func TestProjectEnvOverride(t *testing.T) {
	t.Setenv("ONECLI_PROJECT", "my-proj")
	if got := Project(); got != "my-proj" {
		t.Errorf("Project() = %q, want my-proj", got)
	}
}

func TestProjectFromConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_ENV", "")
	t.Setenv("ONECLI_PROJECT", "")

	if err := SetConfigValue("project", "file-proj"); err != nil {
		t.Fatal(err)
	}
	if got := Project(); got != "file-proj" {
		t.Errorf("Project() = %q, want file-proj", got)
	}
}

func TestProjectEnvTakesPrecedenceOverFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_ENV", "")

	_ = SetConfigValue("project", "file-proj")
	t.Setenv("ONECLI_PROJECT", "env-proj")

	if got := Project(); got != "env-proj" {
		t.Errorf("Project() = %q, env var should take precedence", got)
	}
}

func TestGetConfigValueProjectRespectsEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_ENV", "")

	_ = SetConfigValue("project", "file-proj")
	t.Setenv("ONECLI_PROJECT", "env-proj")

	val, err := GetConfigValue("project")
	if err != nil {
		t.Fatal(err)
	}
	if val != "env-proj" {
		t.Errorf("GetConfigValue(project) = %q, env var should take precedence", val)
	}
}

func TestAgentDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_AGENT", "")

	if got := Agent(); got != "" {
		t.Errorf("Agent() = %q, want empty", got)
	}
}

func TestAgentEnvOverride(t *testing.T) {
	t.Setenv("ONECLI_AGENT", "env-agent")
	if got := Agent(); got != "env-agent" {
		t.Errorf("Agent() = %q, want env-agent", got)
	}
}

func TestAgentFromConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_ENV", "")
	t.Setenv("ONECLI_AGENT", "")

	if err := SetConfigValue("agent", "file-agent"); err != nil {
		t.Fatal(err)
	}
	if got := Agent(); got != "file-agent" {
		t.Errorf("Agent() = %q, want file-agent", got)
	}
}

func TestAgentEnvTakesPrecedenceOverFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_ENV", "")

	_ = SetConfigValue("agent", "file-agent")
	t.Setenv("ONECLI_AGENT", "env-agent")

	if got := Agent(); got != "env-agent" {
		t.Errorf("Agent() = %q, env var should take precedence", got)
	}
}

func TestGetConfigValueAgentRespectsEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_ENV", "")

	_ = SetConfigValue("agent", "file-agent")
	t.Setenv("ONECLI_AGENT", "env-agent")

	val, err := GetConfigValue("agent")
	if err != nil {
		t.Fatal(err)
	}
	if val != "env-agent" {
		t.Errorf("GetConfigValue(agent) = %q, env var should take precedence", val)
	}
}

func TestSetConfigValueAgentRejectsInvalidIdentifier(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_ENV", "")

	// The agent value reaches URLs and shell contexts downstream — reject
	// hardened-input violations at the boundary, like the --agent flag does.
	for _, bad := range []string{"", "two words", "a?b", "a%20b", "../etc"} {
		if err := SetConfigValue("agent", bad); !errors.Is(err, ErrInvalidConfigValue) {
			t.Errorf("SetConfigValue(agent, %q) = %v, want ErrInvalidConfigValue", bad, err)
		}
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
	if val != "https://api.onecli.sh" {
		t.Errorf("got %q, want default", val)
	}
}
