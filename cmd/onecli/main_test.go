package main

import (
	"testing"
)

func TestResolveProjectFlagTakesPrecedence(t *testing.T) {
	t.Setenv("ONECLI_PROJECT", "env-proj")
	got, err := resolveProject("flag-proj")
	if err != nil {
		t.Fatal(err)
	}
	if got != "flag-proj" {
		t.Errorf("resolveProject(flag-proj) = %q, want flag-proj", got)
	}
}

func TestResolveProjectFallsBackToEnv(t *testing.T) {
	t.Setenv("ONECLI_PROJECT", "env-proj")
	got, err := resolveProject("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "env-proj" {
		t.Errorf("resolveProject(\"\") = %q, want env-proj", got)
	}
}

func TestResolveProjectEmptyWhenUnset(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_PROJECT", "")
	t.Setenv("ONECLI_ENV", "")

	got, err := resolveProject("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("resolveProject(\"\") = %q, want empty", got)
	}
}

func TestResolveProjectRejectsInvalidFlag(t *testing.T) {
	tests := []struct {
		name string
		flag string
	}{
		{"path traversal", "../etc/passwd"},
		{"query injection", "proj?foo=bar"},
		{"percent encoding", "proj%2e"},
		{"control chars", "proj\x00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveProject(tt.flag)
			if err == nil {
				t.Errorf("resolveProject(%q) should return error", tt.flag)
			}
		})
	}
}

func TestResolveProjectRejectsInvalidEnvValue(t *testing.T) {
	t.Setenv("ONECLI_PROJECT", "bad?proj")
	_, err := resolveProject("")
	if err == nil {
		t.Error("resolveProject should reject invalid env value")
	}
}
