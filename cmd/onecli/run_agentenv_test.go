package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/onecli/onecli-cli/internal/agentenv"
	"github.com/onecli/onecli-cli/internal/api"
)

// oldServer answers GET /v1/container-config the way an older setup does.
func oldServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			w.WriteHeader(200)
		case "/v1/container-config":
			calls++
			writeAgentJSON(w, 200, api.ContainerConfig{
				Env: map[string]string{
					"HTTPS_PROXY": "http://x:aoc_old@gw.example.com:10255",
					"HTTP_PROXY":  "http://x:aoc_old@gw.example.com:10255",
				},
				CACertificate: testPEM,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func runDryRun(t *testing.T, cmd RunCmd) map[string]any {
	t.Helper()
	out, stdout, _ := newOut()
	cmd.DryRun = true
	if err := cmd.Run(out); err != nil {
		t.Fatal(err)
	}
	res := decodeOut(t, stdout)
	payload, _ := res["payload"].(map[string]any)
	return payload
}

func TestRunPicksItsConfig(t *testing.T) {
	cases := []struct {
		name       string
		setup      func(t *testing.T, dir string)
		agentFlag  string
		wantSource string
		wantOld    int
	}{
		{name: "agent.env", setup: func(t *testing.T, dir string) { writeEnvFile(t, dir) }, wantSource: "agent-credential"},
		{name: "credential variable", setup: func(t *testing.T, _ string) { t.Setenv(agentenv.CredentialVar, testCred) }, wantSource: "agent-credential"},
		{name: "old setup", setup: func(*testing.T, string) {}, wantSource: "container-config", wantOld: 1},
		{name: "agent flag keeps the old call", setup: func(t *testing.T, dir string) { writeEnvFile(t, dir) },
			agentFlag: "old-agent", wantSource: "container-config", wantOld: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, dir := agentEnv(t)
			tc.setup(t, dir)
			f := newFakeAPI(t)
			srv, oldCalls := oldServer(t)
			t.Setenv("ONECLI_API_HOST", srv.URL)
			t.Setenv("ONECLI_API_KEY", "")
			payload := runDryRun(t, RunCmd{Agent: tc.agentFlag, Args: []string{"sh"}})
			if payload["config"] != tc.wantSource {
				t.Fatalf("config = %v, want %s", payload["config"], tc.wantSource)
			}
			if *oldCalls != tc.wantOld {
				t.Fatalf("container-config calls = %d, want %d", *oldCalls, tc.wantOld)
			}
			names := []string{}
			for _, n := range payload["env_injected"].([]any) {
				names = append(names, n.(string))
			}
			for _, want := range []string{"HTTPS_PROXY", "SSL_CERT_FILE"} {
				if !slices.Contains(names, want) {
					t.Fatalf("env_injected = %v lacks %s", names, want)
				}
			}
			if tc.wantSource == "agent-credential" && len(f.callsTo("GET /v1/gateway/ca")) == 0 {
				if _, err := os.Stat(filepath.Join(dir, agentenv.CAFileName)); err != nil {
					t.Fatal("the new path took the certificate from nowhere")
				}
			}
		})
	}
}

func TestAgentRunConfigMatchesAgentEnv(t *testing.T) {
	_, dir := agentEnv(t)
	writeEnvFile(t, dir)
	newFakeAPI(t)
	cfg, err := agentRunConfig(newAgentAPI(), true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Env["HTTPS_PROXY"] != "http://x:"+testCred+"@gw.example.com:10255" || cfg.CACertificate != testPEM {
		t.Fatalf("cfg = %+v", cfg)
	}
	for _, k := range caTrustKeys {
		if _, ok := cfg.Env[k]; ok {
			t.Fatalf("%s must be set from the combined bundle, not agent.env", k)
		}
	}
	env := childEnv(cfg, "/tmp/bundle.pem", true)
	for _, want := range []string{"SSL_CERT_FILE=/tmp/bundle.pem", "PGSSLROOTCERT=/tmp/bundle.pem", "NODE_USE_ENV_PROXY=1"} {
		if !slices.Contains(env, want) {
			t.Fatalf("child env lacks %s", want)
		}
	}
	if !slices.ContainsFunc(env, func(kv string) bool {
		return strings.HasPrefix(kv, "NODE_OPTIONS=") && strings.Contains(kv, undiciWarningFlag)
	}) {
		t.Fatal("the Node proxy warning is not muted")
	}
}

func TestChildEnvOldPathUnchanged(t *testing.T) {
	t.Setenv("PGSSLROOTCERT", "/mine.pem")
	cfg := &api.ContainerConfig{Env: map[string]string{"HTTPS_PROXY": "http://x:aoc_old@gw:1"}}
	got := childEnv(cfg, "/b.pem", false)
	want := suppressUndiciProxyWarning(buildChildEnv(os.Environ(), cfg.Env, "/b.pem"))
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	if string(a) != string(b) {
		t.Fatal("the old path's child env changed")
	}
}

func TestUpsertProxyURLRefreshesAnAgentCredential(t *testing.T) {
	content := "[network]\nproxy_url = \"http://x:oc_agent_old@gw:1\"\n"
	updated, status := upsertGatewayProxyURL(content, "http://x:oc_agent_new@gw:1")
	if status != proxyURLRefreshed || !strings.Contains(updated, "oc_agent_new") {
		t.Fatalf("status = %v, content = %q", status, updated)
	}
}
