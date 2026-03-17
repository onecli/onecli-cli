package validate

import (
	"fmt"
	"net/url"
	"strings"
)

// ResourceID validates a resource identifier (agent ID, secret ID, etc.).
// Rejects path traversal, query param injection, percent-encoding, and control characters.
func ResourceID(id string) error {
	if id == "" {
		return fmt.Errorf("must not be empty")
	}
	if err := NoControlChars(id); err != nil {
		return err
	}
	if strings.ContainsAny(id, " \t\n\r") {
		return fmt.Errorf("must not contain whitespace")
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("must not contain path traversal (..)")
	}
	if strings.ContainsAny(id, "?#") {
		return fmt.Errorf("must not contain query parameters (? or #)")
	}
	if strings.Contains(id, "%") {
		return fmt.Errorf("must not contain percent-encoded characters")
	}
	return nil
}

// NoControlChars rejects bytes below ASCII 0x20 except tab, newline, and carriage return.
func NoControlChars(s string) error {
	for i, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return fmt.Errorf("contains control character at position %d (0x%02x)", i, r)
		}
	}
	return nil
}

// URL validates that a string is a valid HTTP or HTTPS URL.
func URL(u string) error {
	if u == "" {
		return fmt.Errorf("must not be empty")
	}
	if err := NoControlChars(u); err != nil {
		return err
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("must use http or https scheme, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("must have a host")
	}
	return nil
}

// APIKey validates that a string looks like a valid OneCLI API key.
func APIKey(key string) error {
	if key == "" {
		return fmt.Errorf("must not be empty")
	}
	if err := NoControlChars(key); err != nil {
		return err
	}
	if !strings.HasPrefix(key, "oc_") {
		return fmt.Errorf("must start with 'oc_' prefix")
	}
	return nil
}
