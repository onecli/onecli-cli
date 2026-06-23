package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

func TestRewriteContainerHomeEnv(t *testing.T) {
	t.Run("rewrites container CODEX_HOME to local", func(t *testing.T) {
		env := map[string]string{
			"CODEX_HOME":  "/home/node/.codex",
			"HTTPS_PROXY": "https://proxy:8080",
		}
		rewriteContainerHomeEnv(env, "/Users/me")
		if got, want := env["CODEX_HOME"], filepath.Join("/Users/me", ".codex"); got != want {
			t.Errorf("CODEX_HOME = %q, want %q", got, want)
		}
		if env["HTTPS_PROXY"] != "https://proxy:8080" {
			t.Errorf("HTTPS_PROXY mutated: %q", env["HTTPS_PROXY"])
		}
	})

	t.Run("no-op when var absent", func(t *testing.T) {
		env := map[string]string{"PATH": "/usr/bin"}
		rewriteContainerHomeEnv(env, "/Users/me")
		if _, ok := env["CODEX_HOME"]; ok {
			t.Error("CODEX_HOME should not be added when absent")
		}
	})

	t.Run("no-op when home empty", func(t *testing.T) {
		env := map[string]string{"CODEX_HOME": "/home/node/.codex"}
		rewriteContainerHomeEnv(env, "")
		if env["CODEX_HOME"] != "/home/node/.codex" {
			t.Errorf("CODEX_HOME = %q, want unchanged", env["CODEX_HOME"])
		}
	})

	t.Run("nil map is safe", func(t *testing.T) {
		rewriteContainerHomeEnv(nil, "/Users/me")
	})
}

func TestAgentSkillDir(t *testing.T) {
	tests := []struct {
		cmd  string
		want agentSpec
		ok   bool
	}{
		{"claude", agentSpec{agentName: "Claude Code", baseDir: ".claude"}, true},
		{"cursor", agentSpec{agentName: "Cursor", baseDir: ".cursor", configDir: "Cursor"}, true},
		{"agent", agentSpec{agentName: "Cursor", baseDir: ".cursor", configDir: "Cursor"}, true},
		{"codex", agentSpec{agentName: "Codex", baseDir: ".agents", nativeProxyConfig: ".codex"}, true},
		{"hermes", agentSpec{agentName: "Hermes", baseDir: ".hermes", skipHook: true, pluginGateway: true, dockerSandbox: true}, true},
		{"opencode", agentSpec{agentName: "OpenCode", baseDir: ".opencode"}, true},
		{"/usr/local/bin/cursor", agentSpec{agentName: "Cursor", baseDir: ".cursor", configDir: "Cursor"}, true},
		{"unknown", agentSpec{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got, ok := agentSkillDir(tt.cmd)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("spec = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestProxyURLWithHost(t *testing.T) {
	tests := []struct{ name, raw, host, want string }{
		{"rewrites host keeping port+creds", "http://aoc_tok:x@127.0.0.1:10255", "host.docker.internal", "http://aoc_tok:x@host.docker.internal:10255"},
		{"no port", "http://aoc_tok@localhost", "host.docker.internal", "http://aoc_tok@host.docker.internal"},
		{"empty input", "", "host.docker.internal", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := proxyURLWithHost(tt.raw, tt.host); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrependPythonPath(t *testing.T) {
	sep := string(os.PathListSeparator)
	t.Run("absent appends", func(t *testing.T) {
		got := prependPythonPath([]string{"HOME=/h"}, "/shim")
		if v, _ := envValue(got, "PYTHONPATH"); v != "/shim" {
			t.Errorf("PYTHONPATH = %q, want /shim", v)
		}
	})
	t.Run("existing prepends", func(t *testing.T) {
		got := prependPythonPath([]string{"PYTHONPATH=/a" + sep + "/b"}, "/shim")
		want := "/shim" + sep + "/a" + sep + "/b"
		if v, _ := envValue(got, "PYTHONPATH"); v != want {
			t.Errorf("PYTHONPATH = %q, want %q", v, want)
		}
	})
	t.Run("idempotent when already present", func(t *testing.T) {
		got := prependPythonPath([]string{"PYTHONPATH=/shim" + sep + "/a"}, "/shim")
		if v, _ := envValue(got, "PYTHONPATH"); v != "/shim"+sep+"/a" {
			t.Errorf("PYTHONPATH = %q", v)
		}
	})
}

func TestHermesSandboxEnv(t *testing.T) {
	var cfg hermesConfig
	cfg.Terminal.DockerEnv = map[string]any{"FOO": "bar"}
	cfg.Terminal.DockerVolumes = []string{"/data:/data"}

	env := hermesSandboxEnv(cfg, "/home/u/.onecli/ca-bundle.pem", "/home/u/.onecli/pyca",
		"http://aoc_t:x@host.docker.internal:10255")

	if v, _ := envValue(env, "TERMINAL_DOCKER_PERSIST_ACROSS_PROCESSES"); v != "false" {
		t.Errorf("persist = %q, want false", v)
	}

	rawEnv, ok := envValue(env, "TERMINAL_DOCKER_ENV")
	if !ok {
		t.Fatal("TERMINAL_DOCKER_ENV missing")
	}
	var de map[string]string
	if err := json.Unmarshal([]byte(rawEnv), &de); err != nil {
		t.Fatalf("docker_env not valid JSON: %v", err)
	}
	if de["FOO"] != "bar" {
		t.Errorf("user docker_env clobbered: FOO=%q", de["FOO"])
	}
	if de["HTTPS_PROXY"] != "http://aoc_t:x@host.docker.internal:10255" {
		t.Errorf("HTTPS_PROXY = %q", de["HTTPS_PROXY"])
	}
	if de["SSL_CERT_FILE"] != "/etc/ssl/onecli-ca.pem" {
		t.Errorf("SSL_CERT_FILE = %q", de["SSL_CERT_FILE"])
	}
	if de["PYTHONPATH"] != "/opt/onecli-pyca" {
		t.Errorf("PYTHONPATH = %q", de["PYTHONPATH"])
	}
	if de["ONECLI_GATEWAY"] != "true" {
		t.Errorf("ONECLI_GATEWAY = %q", de["ONECLI_GATEWAY"])
	}

	rawVol, _ := envValue(env, "TERMINAL_DOCKER_VOLUMES")
	var vols []string
	if err := json.Unmarshal([]byte(rawVol), &vols); err != nil {
		t.Fatalf("volumes not valid JSON: %v", err)
	}
	if !slices.Contains(vols, "/data:/data") {
		t.Errorf("user volume dropped: %v", vols)
	}
	if !slices.Contains(vols, "/home/u/.onecli/ca-bundle.pem:/etc/ssl/onecli-ca.pem:ro") {
		t.Errorf("CA mount missing: %v", vols)
	}
}

func TestHermesSandboxEnv_NoCA(t *testing.T) {
	env := hermesSandboxEnv(hermesConfig{}, "", "", "")
	rawEnv, _ := envValue(env, "TERMINAL_DOCKER_ENV")
	var de map[string]string
	_ = json.Unmarshal([]byte(rawEnv), &de)
	if _, ok := de["SSL_CERT_FILE"]; ok {
		t.Errorf("should not set CA env when caPath empty: %v", de)
	}
	if de["ONECLI_GATEWAY"] != "true" {
		t.Error("ONECLI_GATEWAY should still be set")
	}
}

func TestContainerProxyURLFor(t *testing.T) {
	const server = "http://aoc_tok:x@host.docker.internal:10255"
	tests := []struct{ name, gatewayHost, want string }{
		{"loopback 127.0.0.1 → host.docker.internal", "127.0.0.1", "http://aoc_tok:x@host.docker.internal:10255"},
		{"loopback localhost → host.docker.internal", "localhost", "http://aoc_tok:x@host.docker.internal:10255"},
		{"routable cloud host kept as-is", "api.onecli.sh", "http://aoc_tok:x@api.onecli.sh:10255"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containerProxyURLFor(server, tt.gatewayHost); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHermesSandboxEnv_AddHost(t *testing.T) {
	addHostPresent := func(env []string) bool {
		raw, _ := envValue(env, "TERMINAL_DOCKER_EXTRA_ARGS")
		var args []string
		_ = json.Unmarshal([]byte(raw), &args)
		return slices.Contains(args, "host.docker.internal:host-gateway")
	}

	// host.docker.internal proxy → --add-host only on Linux.
	hdi := hermesSandboxEnv(hermesConfig{}, "/ca.pem", "/shim", "http://t@host.docker.internal:10255")
	if got, want := addHostPresent(hdi), runtime.GOOS == "linux"; got != want {
		t.Errorf("host.docker.internal proxy: --add-host=%v, want %v (GOOS=%s)", got, want, runtime.GOOS)
	}

	// Routable gateway host → never --add-host (container connects directly).
	if addHostPresent(hermesSandboxEnv(hermesConfig{}, "/ca.pem", "/shim", "http://t@api.onecli.sh:10255")) {
		t.Error("routable gateway host should not get --add-host")
	}
}

func TestHermesSandboxEnv_PythonPath(t *testing.T) {
	// User's docker_env PYTHONPATH is preserved (shim prepended), not clobbered.
	var cfg hermesConfig
	cfg.Terminal.DockerEnv = map[string]any{"PYTHONPATH": "/app/libs"}
	env := hermesSandboxEnv(cfg, "/ca.pem", "/shim", "")
	raw, _ := envValue(env, "TERMINAL_DOCKER_ENV")
	var de map[string]string
	if err := json.Unmarshal([]byte(raw), &de); err != nil {
		t.Fatalf("docker_env not valid JSON: %v", err)
	}
	if de["PYTHONPATH"] != "/opt/onecli-pyca:/app/libs" {
		t.Errorf("PYTHONPATH = %q, want shim prepended to user value", de["PYTHONPATH"])
	}

	// No shim installed (shimDir=="") → don't point PYTHONPATH at an unmounted dir.
	env = hermesSandboxEnv(hermesConfig{}, "/ca.pem", "", "")
	raw, _ = envValue(env, "TERMINAL_DOCKER_ENV")
	de = nil
	_ = json.Unmarshal([]byte(raw), &de)
	if _, ok := de["PYTHONPATH"]; ok {
		t.Errorf("PYTHONPATH should be unset when shim absent: %q", de["PYTHONPATH"])
	}
}

func TestEnableHermesPlugin(t *testing.T) {
	t.Run("creates minimal config when absent", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		changed, err := enableHermesPlugin(path, "onecli-gateway")
		if err != nil || !changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		assertPluginEnabled(t, path)
	})

	t.Run("adds to existing config preserving other keys", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		writeJSON(t, path, "terminal:\n  backend: docker\nsecurity:\n  redact_secrets: true\n")
		changed, err := enableHermesPlugin(path, "onecli-gateway")
		if err != nil || !changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		data, _ := os.ReadFile(path)
		if s := string(data); !strings.Contains(s, "backend: docker") || !strings.Contains(s, "redact_secrets") {
			t.Errorf("existing keys lost:\n%s", s)
		}
		assertPluginEnabled(t, path)
	})

	t.Run("idempotent when already enabled", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		writeJSON(t, path, "plugins:\n  enabled:\n    - onecli-gateway\n")
		changed, err := enableHermesPlugin(path, "onecli-gateway")
		if err != nil {
			t.Fatal(err)
		}
		if changed {
			t.Error("should be a no-op when already enabled")
		}
	})

	t.Run("adds enabled list under existing plugins", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		writeJSON(t, path, "plugins:\n  disabled:\n    - noisy\n")
		changed, err := enableHermesPlugin(path, "onecli-gateway")
		if err != nil || !changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		assertPluginEnabled(t, path)
		if data, _ := os.ReadFile(path); !strings.Contains(string(data), "noisy") {
			t.Errorf("existing plugins.disabled lost:\n%s", data)
		}
	})

	t.Run("promotes scalar enabled, keeping the user's value", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		writeJSON(t, path, "plugins:\n  enabled: my-plugin\n")
		changed, err := enableHermesPlugin(path, "onecli-gateway")
		if err != nil || !changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		assertPluginEnabled(t, path)
		if data, _ := os.ReadFile(path); !strings.Contains(string(data), "my-plugin") {
			t.Errorf("user's scalar plugin value was dropped:\n%s", data)
		}
	})

	t.Run("explicit null enabled becomes a clean sequence, no literal null entry", func(t *testing.T) {
		for _, nullForm := range []string{"null", "~", ""} {
			path := filepath.Join(t.TempDir(), "config.yaml")
			writeJSON(t, path, "plugins:\n  enabled: "+nullForm+"\n")
			changed, err := enableHermesPlugin(path, "onecli-gateway")
			if err != nil || !changed {
				t.Fatalf("nullForm=%q: changed=%v err=%v", nullForm, changed, err)
			}
			assertPluginEnabled(t, path)
			data, _ := os.ReadFile(path)
			var cfg struct {
				Plugins struct {
					Enabled []string `yaml:"enabled"`
				} `yaml:"plugins"`
			}
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("nullForm=%q: result not valid YAML: %v", nullForm, err)
			}
			if want := []string{"onecli-gateway"}; !slices.Equal(cfg.Plugins.Enabled, want) {
				t.Errorf("nullForm=%q: enabled = %v, want %v (a null scalar must not become a list entry)", nullForm, cfg.Plugins.Enabled, want)
			}
		}
	})

	t.Run("errors on duplicate plugins keys instead of false success", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		writeJSON(t, path, "plugins:\n  enabled:\n    - a\nplugins:\n  enabled:\n    - b\n")
		changed, err := enableHermesPlugin(path, "onecli-gateway")
		if err == nil {
			t.Error("expected an error for duplicate top-level plugins keys")
		}
		if changed {
			t.Error("must not report changed=true on a duplicate-key config")
		}
	})
}

func envValue(env []string, key string) (string, bool) {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return kv[len(key)+1:], true
		}
	}
	return "", false
}

func assertPluginEnabled(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Plugins struct {
			Enabled []string `yaml:"enabled"`
		} `yaml:"plugins"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("result not valid YAML: %v\n%s", err, data)
	}
	if slices.Contains(cfg.Plugins.Enabled, "onecli-gateway") {
		return
	}
	t.Errorf("onecli-gateway not in plugins.enabled:\n%s", data)
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
