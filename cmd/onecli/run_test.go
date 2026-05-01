package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFindProxyURL(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want string
	}{
		{"HTTPS_PROXY", []string{"HOME=/home", "HTTPS_PROXY=https://proxy:8080"}, "https://proxy:8080"},
		{"HTTP_PROXY fallback", []string{"HTTP_PROXY=http://proxy:3128"}, "http://proxy:3128"},
		{"lowercase", []string{"https_proxy=https://lower:9090"}, "https://lower:9090"},
		{"HTTPS takes priority over HTTP", []string{"HTTP_PROXY=http://fallback", "HTTPS_PROXY=https://primary"}, "https://primary"},
		{"with credentials", []string{"HTTPS_PROXY=https://user:pass@proxy:8080"}, "https://user:pass@proxy:8080"},
		{"no proxy set", []string{"HOME=/home", "PATH=/usr/bin"}, ""},
		{"empty env", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findProxyURL(tt.env)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripProxyCredentials(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want []string
	}{
		{
			"strips from HTTPS_PROXY",
			[]string{"HOME=/home", "HTTPS_PROXY=https://user:pass@proxy:8080/path"},
			[]string{"HOME=/home", "HTTPS_PROXY=https://proxy:8080/path"},
		},
		{
			"strips from all proxy vars",
			[]string{
				"HTTPS_PROXY=https://u:p@h:1",
				"HTTP_PROXY=http://u:p@h:2",
				"https_proxy=https://u:p@h:3",
				"http_proxy=http://u:p@h:4",
			},
			[]string{
				"HTTPS_PROXY=https://h:1",
				"HTTP_PROXY=http://h:2",
				"https_proxy=https://h:3",
				"http_proxy=http://h:4",
			},
		},
		{
			"preserves non-proxy vars",
			[]string{"HOME=/home", "PATH=/usr/bin"},
			[]string{"HOME=/home", "PATH=/usr/bin"},
		},
		{
			"preserves proxy without credentials",
			[]string{"HTTPS_PROXY=https://proxy:8080"},
			[]string{"HTTPS_PROXY=https://proxy:8080"},
		},
		{
			"handles entry without equals",
			[]string{"NOEQUALSSIGN"},
			[]string{"NOEQUALSSIGN"},
		},
		{
			"empty",
			[]string{},
			[]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripProxyCredentials(tt.env)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d\ngot:  %v\nwant: %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestAgentSkillDir(t *testing.T) {
	tests := []struct {
		cmd      string
		wantName string
		wantDir  string
		wantCfg  string
		wantOK   bool
	}{
		{"claude", "Claude Code", ".claude", "", true},
		{"cursor", "Cursor", ".cursor", "Cursor", true},
		{"agent", "Cursor", ".cursor", "Cursor", true},
		{"codex", "Codex", ".agents", "", true},
		{"hermes", "Hermes", ".hermes", "", true},
		{"opencode", "OpenCode", ".opencode", "", true},
		{"/usr/local/bin/cursor", "Cursor", ".cursor", "Cursor", true},
		{"unknown", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			name, dir, cfg, ok := agentSkillDir(tt.cmd)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if dir != tt.wantDir {
				t.Errorf("dir = %q, want %q", dir, tt.wantDir)
			}
			if cfg != tt.wantCfg {
				t.Errorf("configDir = %q, want %q", cfg, tt.wantCfg)
			}
		})
	}
}

func TestVscodeSettingsPath(t *testing.T) {
	path := vscodeSettingsPath("TestApp")
	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(path, "Application Support/TestApp") {
			t.Errorf("darwin path %q missing Application Support/TestApp", path)
		}
	case "linux":
		if !strings.Contains(path, ".config/TestApp") {
			t.Errorf("linux path %q missing .config/TestApp", path)
		}
	default:
		if path != "" {
			t.Errorf("unsupported OS should return empty, got %q", path)
		}
		return
	}
	if !strings.HasSuffix(path, filepath.Join("User", "settings.json")) {
		t.Errorf("path %q should end with User/settings.json", path)
	}
}

func TestMergeVSCodeProxySettings_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "User", "settings.json")

	err := mergeVSCodeProxySettings(path, "https://proxy:8080", "Basic dTpw", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := readSettingsMap(t, path)
	if got["http.proxy"] != "https://proxy:8080" {
		t.Errorf("http.proxy = %v", got["http.proxy"])
	}
	if got["http.proxyAuthorization"] != "Basic dTpw" {
		t.Errorf("http.proxyAuthorization = %v", got["http.proxyAuthorization"])
	}
}

func TestMergeVSCodeProxySettings_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	writeJSON(t, path, `{
    "editor.fontSize": 14,
    "workbench.colorTheme": "One Dark Pro"
}
`)

	err := mergeVSCodeProxySettings(path, "https://proxy:8080", "Basic dTpw", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := readSettingsMap(t, path)
	if got["editor.fontSize"] != float64(14) {
		t.Errorf("editor.fontSize = %v, want 14", got["editor.fontSize"])
	}
	if got["workbench.colorTheme"] != "One Dark Pro" {
		t.Errorf("workbench.colorTheme = %v", got["workbench.colorTheme"])
	}
	if got["http.proxy"] != "https://proxy:8080" {
		t.Errorf("http.proxy = %v", got["http.proxy"])
	}
}

func TestMergeVSCodeProxySettings_UpdatesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	writeJSON(t, path, `{
    "http.proxy": "https://old:1111",
    "editor.fontSize": 14,
    "http.proxyAuthorization": "Basic b2xk"
}
`)

	err := mergeVSCodeProxySettings(path, "https://new:2222", "Basic bmV3", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := readSettingsMap(t, path)
	if got["http.proxy"] != "https://new:2222" {
		t.Errorf("http.proxy = %v, want https://new:2222", got["http.proxy"])
	}
	if got["http.proxyAuthorization"] != "Basic bmV3" {
		t.Errorf("http.proxyAuthorization = %v, want Basic bmV3", got["http.proxyAuthorization"])
	}
	if got["editor.fontSize"] != float64(14) {
		t.Errorf("editor.fontSize lost, got %v", got["editor.fontSize"])
	}
}

func TestMergeVSCodeProxySettings_TerminalEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	writeJSON(t, path, `{}`)

	termEnv := map[string]string{
		"HTTPS_PROXY": "https://proxy:8080",
		"HTTP_PROXY":  "http://proxy:8080",
	}
	err := mergeVSCodeProxySettings(path, "https://proxy:8080", "Basic dTpw", termEnv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := readSettingsMap(t, path)
	termKey := "terminal.integrated.env.osx"
	if runtime.GOOS == "linux" {
		termKey = "terminal.integrated.env.linux"
	}
	termObj, ok := got[termKey].(map[string]any)
	if !ok {
		t.Fatalf("%s missing or not an object", termKey)
	}
	if termObj["HTTPS_PROXY"] != "https://proxy:8080" {
		t.Errorf("terminal HTTPS_PROXY = %v", termObj["HTTPS_PROXY"])
	}
}

func TestMergeVSCodeProxySettings_RejectsJSONC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	writeJSON(t, path, `{
    // this is a comment
    "editor.fontSize": 14
}
`)

	err := mergeVSCodeProxySettings(path, "https://proxy:8080", "Basic dTpw", nil)
	if err == nil {
		t.Fatal("expected error for JSONC input")
	}
	if !strings.Contains(err.Error(), "comments or invalid JSON") {
		t.Errorf("error = %q, want mention of comments", err.Error())
	}
}

func readSettingsMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshaling %s: %v\ncontent: %s", path, err, data)
	}
	return m
}

func writeJSON(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
