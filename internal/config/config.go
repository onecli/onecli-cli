package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	envVar    = "ONECLI_ENV"
	apiKeyVar = "ONECLI_API_KEY"
	apiHostVar = "ONECLI_API_HOST"

	envProduction = "production"
	envDev        = "dev"

	defaultAPIHost = "https://app.onecli.sh"

	keychainServiceProd = "onecli-api-key"
	keychainServiceDev  = "onecli-api-key-dev"

	credentialsDirProd = ".onecli/credentials"
	credentialsDirDev  = ".onecli/credentials-dev"

	configFileProd = ".onecli/config.json"
	configFileDev  = ".onecli/config-dev.json"
)

// Env returns the current environment name: "production" or "dev".
// Reads from ONECLI_ENV. Unset or empty defaults to "production".
func Env() string {
	v := os.Getenv(envVar)
	if v == envDev {
		return envDev
	}
	return envProduction
}

// IsDev returns true when running in the dev environment.
func IsDev() bool {
	return Env() == envDev
}

// APIHost returns the base URL for the onecli API.
// Precedence: ONECLI_API_HOST env var > config file > default.
func APIHost() string {
	if v := os.Getenv(apiHostVar); v != "" {
		return v
	}
	cfg, err := readConfig()
	if err != nil {
		return defaultAPIHost
	}
	if v, ok := cfg["api-host"]; ok && v != "" {
		return v
	}
	return defaultAPIHost
}

// APIKeyFromEnv returns the API key from the environment variable, if set.
func APIKeyFromEnv() string {
	return os.Getenv(apiKeyVar)
}

// KeychainService returns the keychain service name for API key storage.
func KeychainService() string {
	if IsDev() {
		return keychainServiceDev
	}
	return keychainServiceProd
}

// CredentialsDir returns the absolute path to the credentials directory.
// Creates the directory if it doesn't exist.
func CredentialsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	rel := credentialsDirProd
	if IsDev() {
		rel = credentialsDirDev
	}
	dir := filepath.Join(home, rel)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating credentials directory: %w", err)
	}
	return dir, nil
}

// ErrUnknownConfigKey is returned when a config key is not recognized.
var ErrUnknownConfigKey = errors.New("unknown config key")

// ErrInvalidConfigValue is returned when a config value is not valid for its key.
var ErrInvalidConfigValue = errors.New("invalid config value")

// validKeys maps each known config key to its validator.
// nil means any non-empty string is valid.
var validKeys = map[string]func(string) error{
	"api-host": validateURL,
}

// configDefaults maps each config key to its default value.
var configDefaults = map[string]string{
	"api-host": defaultAPIHost,
}

func validateURL(u string) error {
	if u == "" {
		return fmt.Errorf("must not be empty")
	}
	return nil
}

// configData is the on-disk representation of ~/.onecli/config.json.
type configData map[string]string

// ConfigFilePath returns the absolute path to the config file.
func ConfigFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	rel := configFileProd
	if IsDev() {
		rel = configFileDev
	}
	return filepath.Join(home, rel), nil
}

// readConfig loads the config file from disk. Returns an empty map if the file
// does not exist.
func readConfig() (configData, error) {
	path, err := ConfigFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return configData{}, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	var cfg configData
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}
	return cfg, nil
}

// writeConfig persists the config to disk, creating the parent directory if needed.
func writeConfig(cfg configData) error {
	path, err := ConfigFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}
	return nil
}

// GetConfigValue returns the effective value for a config key.
// Returns ErrUnknownConfigKey for unrecognized keys.
func GetConfigValue(key string) (string, error) {
	if _, ok := validKeys[key]; !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownConfigKey, key)
	}

	// For api-host, respect the full precedence chain.
	if key == "api-host" {
		return APIHost(), nil
	}

	cfg, err := readConfig()
	if err != nil {
		return configDefaults[key], nil
	}
	if v, ok := cfg[key]; ok {
		return v, nil
	}
	return configDefaults[key], nil
}

// SetConfigValue persists a config key/value pair.
// Returns ErrUnknownConfigKey for unrecognized keys and ErrInvalidConfigValue
// for invalid values.
func SetConfigValue(key, value string) error {
	validator, ok := validKeys[key]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownConfigKey, key)
	}
	if validator != nil {
		if err := validator(value); err != nil {
			return fmt.Errorf("%w: %s", ErrInvalidConfigValue, err)
		}
	}

	cfg, err := readConfig()
	if err != nil {
		cfg = configData{}
	}
	cfg[key] = value
	return writeConfig(cfg)
}
