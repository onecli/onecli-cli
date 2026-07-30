package main

import (
	"testing"

	"github.com/onecli/onecli-cli/internal/config"
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

func TestResolveAgentFlagTakesPrecedence(t *testing.T) {
	t.Setenv("ONECLI_AGENT", "env-agent")
	got, err := resolveAgent("flag-agent")
	if err != nil {
		t.Fatal(err)
	}
	if got != "flag-agent" {
		t.Errorf("resolveAgent(flag-agent) = %q, want flag-agent", got)
	}
}

func TestResolveAgentFallsBackToEnv(t *testing.T) {
	t.Setenv("ONECLI_AGENT", "env-agent")
	got, err := resolveAgent("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "env-agent" {
		t.Errorf("resolveAgent(\"\") = %q, want env-agent", got)
	}
}

func TestResolveAgentFallsBackToConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_ENV", "")
	t.Setenv("ONECLI_AGENT", "")

	if err := config.SetConfigValue("agent", "pinned-agent"); err != nil {
		t.Fatal(err)
	}
	got, err := resolveAgent("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "pinned-agent" {
		t.Errorf("resolveAgent(\"\") = %q, want pinned-agent", got)
	}
}

func TestResolveAgentEmptyWhenUnset(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ONECLI_AGENT", "")
	t.Setenv("ONECLI_ENV", "")

	got, err := resolveAgent("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("resolveAgent(\"\") = %q, want empty (server default)", got)
	}
}

func TestResolveAgentRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		flag string
	}{
		{"path traversal", "../etc/passwd"},
		{"query injection", "agent?foo=bar"},
		{"percent encoding", "agent%2e"},
		{"control chars", "agent\x00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveAgent(tt.flag)
			if err == nil {
				t.Errorf("resolveAgent(%q) should return error", tt.flag)
			}
		})
	}
}

func TestResolveAgentRejectsInvalidEnvValue(t *testing.T) {
	t.Setenv("ONECLI_AGENT", "bad agent")
	_, err := resolveAgent("")
	if err == nil {
		t.Error("resolveAgent should reject invalid env value")
	}
}
