package validate

import (
	"testing"
)

func TestResourceID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "valid cuid", id: "clxyz123abc", wantErr: false},
		{name: "valid uuid", id: "550e8400-e29b-41d4-a716-446655440000", wantErr: false},
		{name: "empty", id: "", wantErr: true},
		{name: "path traversal", id: "../etc/passwd", wantErr: true},
		{name: "query param", id: "abc?admin=true", wantErr: true},
		{name: "fragment", id: "abc#section", wantErr: true},
		{name: "percent encoding", id: "abc%2F", wantErr: true},
		{name: "space", id: "abc def", wantErr: true},
		{name: "tab", id: "abc\tdef", wantErr: true},
		{name: "newline", id: "abc\ndef", wantErr: true},
		{name: "null byte", id: "abc\x00def", wantErr: true},
		{name: "bell char", id: "abc\x07def", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ResourceID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResourceID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestNoControlChars(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		wantErr bool
	}{
		{name: "normal text", s: "hello world", wantErr: false},
		{name: "tab allowed", s: "hello\tworld", wantErr: false},
		{name: "newline allowed", s: "hello\nworld", wantErr: false},
		{name: "carriage return allowed", s: "line\r\n", wantErr: false},
		{name: "empty", s: "", wantErr: false},
		{name: "null byte", s: "abc\x00def", wantErr: true},
		{name: "bell", s: "\x07", wantErr: true},
		{name: "escape", s: "\x1b[31m", wantErr: true},
		{name: "form feed", s: "\x0c", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NoControlChars(tt.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("NoControlChars(%q) error = %v, wantErr %v", tt.s, err, tt.wantErr)
			}
		})
	}
}

func TestURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "https", url: "https://example.com", wantErr: false},
		{name: "http", url: "http://localhost:3000", wantErr: false},
		{name: "https with path", url: "https://app.onecli.sh/api/v1", wantErr: false},
		{name: "empty", url: "", wantErr: true},
		{name: "ftp scheme", url: "ftp://example.com", wantErr: true},
		{name: "no scheme", url: "example.com", wantErr: true},
		{name: "no host", url: "https://", wantErr: true},
		{name: "control char", url: "https://example\x00.com", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := URL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("URL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestAPIKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{name: "valid", key: "oc_abc123def456", wantErr: false},
		{name: "valid long", key: "oc_" + "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", wantErr: false},
		{name: "empty", key: "", wantErr: true},
		{name: "no prefix", key: "abc123", wantErr: true},
		{name: "wrong prefix", key: "sk_abc123", wantErr: true},
		{name: "control char", key: "oc_abc\x00def", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := APIKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("APIKey(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
		})
	}
}
