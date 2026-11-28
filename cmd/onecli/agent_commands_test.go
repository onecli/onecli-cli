package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onecli/onecli-cli/internal/agentapi"
	"github.com/onecli/onecli-cli/internal/agentenv"
	"github.com/onecli/onecli-cli/pkg/exitcode"
	"github.com/onecli/onecli-cli/pkg/output"
)

const (
	testCred  = "oc_agent_test_CRED123"
	testToken = "oc_connect_test_TOKEN123"
	testPEM   = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
)

// fakeAPI is an httptest server for the agent routes. Each handler may be
// replaced per test; every call is recorded.
type fakeAPI struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	calls    []recorded
	handlers map[string]http.HandlerFunc
}

type recorded struct {
	Method, Path, Auth, Held string
	Body                     map[string]any
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{t: t, handlers: map[string]http.HandlerFunc{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.calls = append(f.calls, recorded{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get(agentapi.CredentialHeader), body})
		h := f.handlers[r.Method+" "+r.URL.Path]
		f.mu.Unlock()
		if h == nil {
			h = f.defaults(r.Method + " " + r.URL.Path)
		}
		if h == nil {
			writeV2Error(w, 404, "not_found", "no route")
			return
		}
		h(w, r)
	}))
	t.Cleanup(f.srv.Close)
	orig := newAgentAPI
	newAgentAPI = func() *agentapi.Client { return agentapi.New(f.srv.URL) }
	t.Cleanup(func() { newAgentAPI = orig })
	return f
}

func (f *fakeAPI) on(route string, h http.HandlerFunc) { f.handlers[route] = h }

func (f *fakeAPI) callsTo(route string) []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recorded
	for _, c := range f.calls {
		if c.Method+" "+c.Path == route {
			out = append(out, c)
		}
	}
	return out
}

func writeAgentJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeV2Error(w http.ResponseWriter, status int, code, msg string, details ...map[string]string) {
	if details == nil {
		details = []map[string]string{}
	}
	writeAgentJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": msg, "details": details, "requestId": nil}})
}

func agentBearerOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testCred {
			writeV2Error(w, 401, "unauthenticated", "Not signed in.")
			return
		}
		next(w, r)
	}
}

func (f *fakeAPI) defaults(route string) http.HandlerFunc {
	switch route {
	case "GET /v1/gateway/ca":
		return func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, testPEM) }
	case "GET /v1/instance":
		return func(w http.ResponseWriter, _ *http.Request) {
			writeAgentJSON(w, 200, map[string]any{"origins": map[string]any{"external": "https://app.example.com"}})
		}
	case "GET /v2/skill":
		return func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "# OneCLI for agents\n") }
	case "POST /v2/agents":
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+testToken {
				writeV2Error(w, 401, "unauthenticated", "Not signed in.")
				return
			}
			writeAgentJSON(w, 201, map[string]any{
				"id": "agt_1", "name": "linux-ab12", "vaultId": "vlt_1", "runtime": "linux",
				"createdAt": "2026-11-27T00:00:00Z", "expiresAt": nil,
				"gateway":    map[string]any{"address": "gw.example.com:10255", "scheme": "http"},
				"credential": testCred,
			})
		}
	case "GET /v2/agents/me":
		return agentBearerOnly(func(w http.ResponseWriter, _ *http.Request) {
			writeAgentJSON(w, 200, map[string]any{
				"id": "agt_1", "name": "linux-ab12", "vault": map[string]any{"id": "vlt_1", "name": "Personal"},
				"runtime": "linux", "expiresAt": nil,
				"gateway": map[string]any{"address": "gw.example.com:10255", "scheme": "http"},
			})
		})
	case "GET /v2/agents/me/items":
		return agentBearerOnly(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("cursor") == "" {
				next := "c2"
				writeAgentJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "itm_1", "kind": "app", "name": "GitHub", "service": "github", "hosts": []string{"api.github.com"}, "allows": []string{"read"}, "needsApproval": false, "state": "working", "example": nil}}, "nextCursor": next})
				return
			}
			writeAgentJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "itm_2", "kind": "secret", "name": "Key", "service": nil, "hosts": []string{"api.example.com"}, "allows": []string{}, "needsApproval": true, "state": "working", "example": nil}}, "nextCursor": nil})
		})
	case "POST /v2/agents/me/check":
		return agentBearerOnly(func(w http.ResponseWriter, _ *http.Request) {
			writeAgentJSON(w, 200, map[string]any{"decision": "allowed", "item": map[string]any{"id": "itm_1"}, "rule": nil, "reason": "Your item GitHub serves it."})
		})
	}
	return nil
}

// agentEnv points every agent path at temp folders and clears the
// credential variable.
func agentEnv(t *testing.T) (home, dir string) {
	t.Helper()
	home = t.TempDir()
	dir = filepath.Join(t.TempDir(), "onecli")
	t.Setenv("HOME", home)
	t.Setenv(agentenv.HomeVar, dir)
	t.Setenv(agentenv.ProfileVar, filepath.Join(home, ".profile"))
	t.Setenv(agentenv.CredentialVar, "")
	t.Setenv("SSL_CERT_FILE", "")
	fakeSystemRoots(t)
	return home, dir
}

// testRootPEM stands in for the system's root certificates.
const testRootPEM = "-----BEGIN CERTIFICATE-----\nUk9PVA==\n-----END CERTIFICATE-----\n"

// fakeSystemRoots points the system root lookup at one temp file, so tests
// never depend on the machine's roots.
func fakeSystemRoots(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "roots.pem")
	if err := os.WriteFile(p, []byte(testRootPEM), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := agentenv.SystemCAPaths
	agentenv.SystemCAPaths = []string{p}
	t.Cleanup(func() { agentenv.SystemCAPaths = orig })
	return p
}

func newOut() (*output.Writer, *bytes.Buffer, *bytes.Buffer) {
	var o, e bytes.Buffer
	return output.NewWithWriters(&o, &e), &o, &e
}

func decodeOut(t *testing.T, b *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b.Bytes(), &m); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, b.String())
	}
	return m
}

func wantAgentErr(t *testing.T, err error, code string, exit int) *agentError {
	t.Helper()
	var ae *agentError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v (%T), want agentError %s", err, err, code)
	}
	if ae.code != code || ae.exit != exit {
		t.Fatalf("agentError = %s/%d (%s), want %s/%d", ae.code, ae.exit, ae.msg, code, exit)
	}
	return ae
}

func writeEnvFile(t *testing.T, dir string) {
	t.Helper()
	_, err := agentenv.Write(dir, agentenv.Settings{
		AgentID: "agt_1", VaultID: "vlt_1", Credential: testCred,
		Scheme: "http", Address: "gw.example.com:10255", CAFile: filepath.Join(dir, agentenv.CAFileName),
		BundleFile: filepath.Join(dir, agentenv.CABundleFileName),
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ---- connect ----

func TestConnectWritesAgentEnvAndNeverTheToken(t *testing.T) {
	home, dir := agentEnv(t)
	f := newFakeAPI(t)
	out, stdout, _ := newOut()
	if err := (&ConnectCmd{Token: testToken, Name: "bot"}).Run(out); err != nil {
		t.Fatal(err)
	}
	res := decodeOut(t, stdout)
	if res["agent_id"] != "agt_1" || res["created"] != true {
		t.Fatalf("result = %v", res)
	}
	envPath := filepath.Join(dir, agentenv.FileName)
	st, err := os.Stat(envPath)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("agent.env: %v mode %v", err, st)
	}
	vals, _ := agentenv.Read(envPath)
	if vals[agentenv.CredentialVar] != testCred || vals["HTTPS_PROXY"] != "http://x:"+testCred+"@gw.example.com:10255" {
		t.Fatalf("agent.env = %v", vals)
	}
	// The token is in no file and not in stdout.
	for _, p := range []string{envPath, filepath.Join(dir, agentenv.CAFileName), filepath.Join(home, ".profile")} {
		data, _ := os.ReadFile(p)
		if strings.Contains(string(data), testToken) {
			t.Fatalf("%s holds the connect token", p)
		}
	}
	if strings.Contains(stdout.String(), testToken) || strings.Contains(stdout.String(), testCred) {
		t.Fatalf("stdout holds a secret: %s", stdout)
	}
	calls := f.callsTo("POST /v2/agents")
	if len(calls) != 1 || calls[0].Auth != "Bearer "+testToken || calls[0].Body["name"] != "bot" || calls[0].Held != "" {
		t.Fatalf("trade calls = %+v", calls)
	}
	profile, _ := os.ReadFile(filepath.Join(home, ".profile"))
	if !strings.Contains(string(profile), agentenv.ProfileLine(envPath)) {
		t.Fatalf("profile = %q", profile)
	}
}

func TestConnectSendsTheHeldCredential(t *testing.T) {
	_, dir := agentEnv(t)
	writeEnvFile(t, dir)
	f := newFakeAPI(t)
	f.on("POST /v2/agents", func(w http.ResponseWriter, _ *http.Request) {
		writeAgentJSON(w, 200, map[string]any{"id": "agt_1", "name": "linux-ab12", "vaultId": "vlt_1", "runtime": "linux",
			"createdAt": "x", "expiresAt": nil, "gateway": map[string]any{"address": "gw.example.com:10255", "scheme": "http"}})
	})
	out, stdout, _ := newOut()
	if err := (&ConnectCmd{Token: testToken, NoProfile: true}).Run(out); err != nil {
		t.Fatal(err)
	}
	if res := decodeOut(t, stdout); res["created"] != false {
		t.Fatalf("result = %v", res)
	}
	if calls := f.callsTo("POST /v2/agents"); calls[0].Held != testCred {
		t.Fatalf("held header = %q", calls[0].Held)
	}
	vals, _ := agentenv.Read(filepath.Join(dir, agentenv.FileName))
	if vals[agentenv.CredentialVar] != testCred {
		t.Fatal("a kept agent must leave agent.env as it is")
	}
}

func TestConnectRefusals(t *testing.T) {
	cases := []struct {
		name    string
		cmd     ConnectCmd
		handler http.HandlerFunc
		code    string
		exit    int
		noCall  bool
		action  string
	}{
		{name: "no token", cmd: ConnectCmd{}, code: exitcode.CodeError, exit: exitcode.Error, noCall: true},
		{name: "not a token", cmd: ConnectCmd{Token: "oc_key_abc"}, code: exitcode.CodeValidation, exit: exitcode.Error, noCall: true},
		{name: "token with a quote", cmd: ConnectCmd{Token: "oc_connect_a'b"}, code: exitcode.CodeValidation, exit: exitcode.Error, noCall: true},
		{name: "both token and credential", cmd: ConnectCmd{Token: testToken, Credential: testCred}, code: exitcode.CodeValidation, exit: exitcode.Error, noCall: true},
		{name: "name with a quote", cmd: ConnectCmd{Token: testToken, Name: `a"b`}, code: exitcode.CodeValidation, exit: exitcode.Error, noCall: true},
		{name: "token refused", cmd: ConnectCmd{Token: testToken},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeV2Error(w, 401, "unauthenticated", "Not signed in.")
			},
			code: exitcode.CodeAuthRequired, exit: exitcode.AuthRequired},
		{name: "on another vault", cmd: ConnectCmd{Token: testToken},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeV2Error(w, 409, "conflict", "This agent is connected to Work. Send replace.", map[string]string{"field": "replace", "issue": "agent_on_other_vault", "message": "x"})
			},
			code: exitcode.CodeConflict, exit: exitcode.Conflict, action: "--replace"},
		{name: "answer with an unsafe credential", cmd: ConnectCmd{Token: testToken},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeAgentJSON(w, 201, map[string]any{"id": "agt_1", "name": "n", "vaultId": "v", "credential": "oc_agent_x';id;'",
					"gateway": map[string]any{"address": "gw:1", "scheme": "http"}})
			},
			code: "", exit: 0},
		{name: "answer with an unsafe address", cmd: ConnectCmd{Token: testToken},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeAgentJSON(w, 201, map[string]any{"id": "agt_1", "name": "n", "vaultId": "v", "credential": testCred,
					"gateway": map[string]any{"address": "evil.com/$(id)", "scheme": "http"}})
			},
			code: "", exit: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, dir := agentEnv(t)
			f := newFakeAPI(t)
			if tc.handler != nil {
				f.on("POST /v2/agents", tc.handler)
			}
			out, _, _ := newOut()
			cmd := tc.cmd
			err := cmd.Run(out)
			if err == nil {
				t.Fatal("connect succeeded")
			}
			if tc.code != "" {
				ae := wantAgentErr(t, err, tc.code, tc.exit)
				if tc.action != "" && !strings.Contains(ae.action, tc.action) {
					t.Fatalf("action = %q, want it to name %q", ae.action, tc.action)
				}
				if strings.Contains(ae.msg, "oc_connect_") || strings.Contains(ae.msg, "oc_key_") {
					t.Fatalf("the error echoes the argument: %q", ae.msg)
				}
			}
			if _, serr := os.Stat(filepath.Join(dir, agentenv.FileName)); !os.IsNotExist(serr) {
				t.Fatal("agent.env was written on a refusal")
			}
			if tc.noCall && len(f.calls) != 0 {
				t.Fatalf("calls made before validation: %+v", f.calls)
			}
		})
	}
}

func TestConnectNoCertificateMakesNothing(t *testing.T) {
	_, dir := agentEnv(t)
	f := newFakeAPI(t)
	f.on("GET /v1/gateway/ca", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html>") })
	out, _, _ := newOut()
	if err := (&ConnectCmd{Token: testToken}).Run(out); err == nil {
		t.Fatal("connect succeeded with no certificate")
	}
	if len(f.callsTo("POST /v2/agents")) != 0 {
		t.Fatal("the token was traded before the certificate was had")
	}
	if _, err := os.Stat(filepath.Join(dir, agentenv.FileName)); !os.IsNotExist(err) {
		t.Fatal("agent.env written")
	}
}

func TestConnectDryRunSendsNothing(t *testing.T) {
	_, dir := agentEnv(t)
	f := newFakeAPI(t)
	out, stdout, _ := newOut()
	if err := (&ConnectCmd{Token: testToken, DryRun: true}).Run(out); err != nil {
		t.Fatal(err)
	}
	if res := decodeOut(t, stdout); res["dry_run"] != true {
		t.Fatalf("result = %v", res)
	}
	if strings.Contains(stdout.String(), testToken) {
		t.Fatal("dry run prints the token")
	}
	if len(f.calls) != 0 {
		t.Fatalf("dry run made calls: %+v", f.calls)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("dry run made the folder")
	}
}

func TestConnectReadsTheTokenFromStdin(t *testing.T) {
	agentEnv(t)
	f := newFakeAPI(t)
	orig := stdin
	stdin = strings.NewReader(testToken + "\n")
	t.Cleanup(func() { stdin = orig })
	out, _, _ := newOut()
	if err := (&ConnectCmd{Token: "-", NoProfile: true}).Run(out); err != nil {
		t.Fatal(err)
	}
	if calls := f.callsTo("POST /v2/agents"); len(calls) != 1 || calls[0].Auth != "Bearer "+testToken {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestConnectWithCredential(t *testing.T) {
	_, dir := agentEnv(t)
	f := newFakeAPI(t)
	out, _, _ := newOut()
	if err := (&ConnectCmd{Credential: testCred, NoProfile: true}).Run(out); err != nil {
		t.Fatal(err)
	}
	if len(f.callsTo("POST /v2/agents")) != 0 || len(f.callsTo("GET /v2/agents/me")) != 1 {
		t.Fatalf("calls = %+v", f.calls)
	}
	vals, _ := agentenv.Read(filepath.Join(dir, agentenv.FileName))
	if vals["ONECLI_AGENT_ID"] != "agt_1" || vals[agentenv.CredentialVar] != testCred {
		t.Fatalf("agent.env = %v", vals)
	}
}

// ---- status, items, check ----

func TestStatus(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(t *testing.T, dir string)
		code   string
		exit   int
		source string
	}{
		{name: "from agent.env", setup: func(t *testing.T, dir string) { writeEnvFile(t, dir) }, source: "file"},
		{name: "from the variable", setup: func(t *testing.T, _ string) { t.Setenv(agentenv.CredentialVar, testCred) }, source: "env"},
		{name: "not connected", setup: func(*testing.T, string) {}, code: exitcode.CodeAuthRequired, exit: exitcode.AuthRequired},
		{name: "credential stopped", setup: func(t *testing.T, _ string) { t.Setenv(agentenv.CredentialVar, "oc_agent_revoked") },
			code: exitcode.CodeAuthRequired, exit: exitcode.AuthRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, dir := agentEnv(t)
			tc.setup(t, dir)
			newFakeAPI(t)
			out, stdout, _ := newOut()
			err := (&StatusCmd{}).Run(out)
			if tc.code != "" {
				wantAgentErr(t, err, tc.code, tc.exit)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			res := decodeOut(t, stdout)
			if res["vault_name"] != "Personal" || res["credential_source"] != tc.source || res["gateway"] != "http://gw.example.com:10255" {
				t.Fatalf("status = %v", res)
			}
			if strings.Contains(stdout.String(), testCred) {
				t.Fatal("status prints the credential")
			}
		})
	}
}

func TestStatusQuiet(t *testing.T) {
	agentEnv(t)
	t.Setenv(agentenv.CredentialVar, testCred)
	newFakeAPI(t)
	out, stdout, _ := newOut()
	if err := (&StatusCmd{Quiet: "vault_id"}).Run(out); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "vlt_1\n" {
		t.Fatalf("quiet = %q", stdout)
	}
}

func TestItemsWalksEveryPage(t *testing.T) {
	agentEnv(t)
	t.Setenv(agentenv.CredentialVar, testCred)
	f := newFakeAPI(t)
	out, stdout, _ := newOut()
	if err := (&ItemsCmd{Quiet: "id"}).Run(out); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "itm_1\nitm_2\n" {
		t.Fatalf("items = %q", stdout)
	}
	if n := len(f.callsTo("GET /v2/agents/me/items")); n != 2 {
		t.Fatalf("pages fetched = %d", n)
	}
}

func TestItemsStopsAnEndlessCursor(t *testing.T) {
	agentEnv(t)
	t.Setenv(agentenv.CredentialVar, testCred)
	f := newFakeAPI(t)
	f.on("GET /v2/agents/me/items", func(w http.ResponseWriter, _ *http.Request) {
		writeAgentJSON(w, 200, map[string]any{"data": []any{}, "nextCursor": "again"})
	})
	out, _, _ := newOut()
	if err := (&ItemsCmd{}).Run(out); err == nil {
		t.Fatal("an endless list did not stop")
	}
}

func TestCheck(t *testing.T) {
	cases := []struct {
		name, method, url string
		handler           http.HandlerFunc
		decision          string
		code              string
		noCall            bool
	}{
		{name: "allowed", method: "get", url: "https://api.github.com/user", decision: "allowed"},
		{name: "bad method", method: "G E T", url: "https://x.com", code: exitcode.CodeValidation, noCall: true},
		{name: "long method", method: strings.Repeat("A", 17), url: "https://x.com", code: exitcode.CodeValidation, noCall: true},
		{name: "bad url", method: "GET", url: "ftp://x.com", code: exitcode.CodeValidation, noCall: true},
		{name: "unknown is never allowed", method: "GET", url: "https://x.com",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeAgentJSON(w, 503, map[string]any{"decision": "unknown", "reason": "The gateway did not answer."})
			}, decision: "unknown", code: codeUnavailable},
		{name: "throttled", method: "GET", url: "https://x.com",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "7")
				writeV2Error(w, 429, "rate_limited", "Too many checks.")
			}, code: codeRateLimited},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentEnv(t)
			t.Setenv(agentenv.CredentialVar, testCred)
			f := newFakeAPI(t)
			if tc.handler != nil {
				f.on("POST /v2/agents/me/check", tc.handler)
			}
			out, stdout, _ := newOut()
			err := (&CheckCmd{Method: tc.method, URL: tc.url}).Run(out)
			if tc.code != "" {
				ae := wantAgentErr(t, err, tc.code, exitcode.Error)
				if tc.code == codeRateLimited && !strings.Contains(ae.action, "7 seconds") {
					t.Fatalf("action = %q", ae.action)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if tc.decision != "" {
				if res := decodeOut(t, stdout); res["decision"] != tc.decision {
					t.Fatalf("result = %v", res)
				}
			}
			calls := f.callsTo("POST /v2/agents/me/check")
			if tc.noCall {
				if len(calls) != 0 {
					t.Fatal("checked a call that failed validation")
				}
				return
			}
			if len(calls) != 1 || calls[0].Body["method"] != strings.ToUpper(tc.method) {
				t.Fatalf("calls = %+v", calls)
			}
		})
	}
}

// ---- signup ----

func TestSignupPollsUntilApproved(t *testing.T) {
	_, dir := agentEnv(t)
	f := newFakeAPI(t)
	var slept []time.Duration
	origSleep := sleep
	sleep = func(d time.Duration) { slept = append(slept, d) }
	t.Cleanup(func() { sleep = origSleep })

	f.on("POST /v2/agent-signups", func(w http.ResponseWriter, _ *http.Request) {
		writeAgentJSON(w, 201, map[string]any{"id": "sgn_1", "userCode": "KTRM-DQWX", "verificationUrl": "https://app.example.com/connect-agent",
			"pollToken": "poll_SECRET", "expiresAt": time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339), "interval": 5})
	})
	polls := 0
	f.on("POST /v2/agent-signups/sgn_1/token", func(w http.ResponseWriter, r *http.Request) {
		polls++
		switch polls {
		case 1:
			writeV2Error(w, 400, "authorization_pending", "Not yet.")
		case 2:
			writeV2Error(w, 400, "slow_down", "Too soon.")
		default:
			writeAgentJSON(w, 200, map[string]any{"id": "agt_9", "name": "bot", "vaultId": "vlt_1", "runtime": "linux", "createdAt": "x",
				"expiresAt": nil, "credential": testCred, "gateway": map[string]any{"address": "gw.example.com:10255", "scheme": "http"}})
		}
	})
	out, stdout, stderr := newOut()
	if err := (&SignupCmd{Name: "bot", Timeout: time.Hour, NoProfile: true}).Run(out); err != nil {
		t.Fatal(err)
	}
	if res := decodeOut(t, stdout); res["agent_id"] != "agt_9" {
		t.Fatalf("result = %v", res)
	}
	if !strings.Contains(stderr.String(), "KTRM-DQWX") || !strings.Contains(stderr.String(), "connect-agent") {
		t.Fatalf("the person is not shown the link and code: %s", stderr)
	}
	for _, s := range []string{stdout.String(), stderr.String()} {
		if strings.Contains(s, "poll_SECRET") || strings.Contains(s, testCred) {
			t.Fatal("a secret was printed")
		}
	}
	want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second}
	if len(slept) != 3 || slept[0] != want[0] || slept[2] != want[2] {
		t.Fatalf("sleeps = %v, want %v", slept, want)
	}
	for _, c := range f.callsTo("POST /v2/agent-signups/sgn_1/token") {
		if c.Body["pollToken"] != "poll_SECRET" {
			t.Fatalf("poll body = %v", c.Body)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, agentenv.FileName))
	if strings.Contains(string(data), "poll_SECRET") || !strings.Contains(string(data), testCred) {
		t.Fatalf("agent.env = %s", data)
	}
}

func TestSignupEnds(t *testing.T) {
	cases := []struct {
		name   string
		answer func(w http.ResponseWriter)
		code   string
		exit   int
	}{
		{"denied", func(w http.ResponseWriter) { writeV2Error(w, 400, "access_denied", "No.") }, "ACCESS_DENIED", exitcode.Forbidden},
		{"expired", func(w http.ResponseWriter) { writeV2Error(w, 400, "expired_token", "Over.") }, "EXPIRED", exitcode.Error},
		{"bad poll token", func(w http.ResponseWriter) { writeV2Error(w, 401, "unauthenticated", "Invalid poll token.") }, exitcode.CodeAuthRequired, exitcode.AuthRequired},
		{"timeout", func(w http.ResponseWriter) { writeV2Error(w, 400, "authorization_pending", "Not yet.") }, "EXPIRED", exitcode.Error},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, dir := agentEnv(t)
			f := newFakeAPI(t)
			origSleep := sleep
			sleep = func(time.Duration) {}
			t.Cleanup(func() { sleep = origSleep })
			f.on("POST /v2/agent-signups", func(w http.ResponseWriter, _ *http.Request) {
				writeAgentJSON(w, 201, map[string]any{"id": "sgn_1", "userCode": "A", "verificationUrl": "u", "pollToken": "p", "expiresAt": "", "interval": 5})
			})
			f.on("POST /v2/agent-signups/sgn_1/token", func(w http.ResponseWriter, _ *http.Request) { tc.answer(w) })
			out, _, _ := newOut()
			err := (&SignupCmd{Timeout: time.Minute}).Run(out)
			wantAgentErr(t, err, tc.code, tc.exit)
			if _, serr := os.Stat(filepath.Join(dir, agentenv.FileName)); !os.IsNotExist(serr) {
				t.Fatal("agent.env written")
			}
		})
	}
}

func TestSignupRefusesWhenConnected(t *testing.T) {
	_, dir := agentEnv(t)
	writeEnvFile(t, dir)
	f := newFakeAPI(t)
	out, _, _ := newOut()
	wantAgentErr(t, (&SignupCmd{Timeout: time.Minute}).Run(out), exitcode.CodeConflict, exitcode.Conflict)
	if len(f.callsTo("POST /v2/agent-signups")) != 0 {
		t.Fatal("started a sign-up while connected")
	}
}

func TestSignupRefusesAnUnsafeID(t *testing.T) {
	agentEnv(t)
	f := newFakeAPI(t)
	f.on("POST /v2/agent-signups", func(w http.ResponseWriter, _ *http.Request) {
		writeAgentJSON(w, 201, map[string]any{"id": "../agents", "userCode": "A", "verificationUrl": "u", "pollToken": "p", "expiresAt": "", "interval": 5})
	})
	out, _, _ := newOut()
	if err := (&SignupCmd{Timeout: time.Minute}).Run(out); err == nil {
		t.Fatal("polled an unsafe id")
	}
	for _, c := range f.calls {
		if strings.Contains(c.Path, "/token") {
			t.Fatalf("polled with an unsafe id: %s", c.Path)
		}
	}
}

// ---- request ----

func TestRequest(t *testing.T) {
	cases := []struct {
		service, want string
		ok            bool
	}{
		{"github", "https://app.example.com/w/vlt_1", true},
		{"GitHub", "https://app.example.com/w/vlt_1", true},
		{"api.example.com", "https://app.example.com/w/vlt_1", true},
		{"../x", "", false},
		{"a b", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.service, func(t *testing.T) {
			agentEnv(t)
			t.Setenv(agentenv.CredentialVar, testCred)
			newFakeAPI(t)
			out, stdout, _ := newOut()
			err := (&RequestCmd{Service: tc.service}).Run(out)
			if !tc.ok {
				wantAgentErr(t, err, exitcode.CodeValidation, exitcode.Error)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			res := decodeOut(t, stdout)
			if res["url"] != tc.want || res["vault_id"] != "vlt_1" || !strings.Contains(res["message"].(string), strings.ToLower(tc.service)) {
				t.Fatalf("request = %v, want url %s", res, tc.want)
			}
		})
	}
}

func TestRequestRefusesAnOddOrigin(t *testing.T) {
	agentEnv(t)
	t.Setenv(agentenv.CredentialVar, testCred)
	f := newFakeAPI(t)
	f.on("GET /v1/instance", func(w http.ResponseWriter, _ *http.Request) {
		writeAgentJSON(w, 200, map[string]any{"origins": map[string]any{"external": "javascript:alert(1)"}})
	})
	out, stdout, _ := newOut()
	if err := (&RequestCmd{Service: "github"}).Run(out); err != nil {
		t.Fatal(err)
	}
	res := decodeOut(t, stdout)
	if _, has := res["url"]; has || res["path"] != "/w/vlt_1" || res["note"] == nil {
		t.Fatalf("an odd origin must give the path alone: %v", res)
	}
}

// ---- disconnect ----

func TestDisconnect(t *testing.T) {
	cases := []struct {
		name       string
		key        string
		localOnly  bool
		del        http.HandlerFunc
		removed    bool
		keepsEnv   bool
		wantDelete bool
		wantAuth   string
		code       string
	}{
		{name: "with a key", key: "oc_key_person", removed: true, wantDelete: true},
		{name: "gone already", key: "oc_key_person", removed: true, wantDelete: true,
			del: func(w http.ResponseWriter, _ *http.Request) { writeV2Error(w, 404, "not_found", "No such agent.") }},
		{name: "no key agent credential refused", key: "", removed: false, wantDelete: true, wantAuth: "Bearer " + testCred,
			del: func(w http.ResponseWriter, _ *http.Request) {
				writeV2Error(w, 401, "unauthenticated", "Not signed in.")
			}},
		{name: "no key agent credential accepted", key: "", removed: true, wantDelete: true, wantAuth: "Bearer " + testCred},
		{name: "local only", key: "oc_key_person", localOnly: true, removed: false},
		{name: "refused keeps agent.env", key: "oc_key_person", wantDelete: true, keepsEnv: true, code: exitcode.CodeForbidden,
			del: func(w http.ResponseWriter, _ *http.Request) {
				writeV2Error(w, 403, "forbidden", "You cannot use this agent's vault.")
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, dir := agentEnv(t)
			writeEnvFile(t, dir)
			envPath := filepath.Join(dir, agentenv.FileName)
			if _, err := agentenv.EnsureProfileLine(filepath.Join(home, ".profile"), envPath); err != nil {
				t.Fatal(err)
			}
			f := newFakeAPI(t)
			f.on("DELETE /v2/agents/agt_1", func(w http.ResponseWriter, r *http.Request) {
				if tc.del != nil {
					tc.del(w, r)
					return
				}
				w.WriteHeader(204)
			})
			orig := loadPersonKey
			loadPersonKey = func() string { return tc.key }
			t.Cleanup(func() { loadPersonKey = orig })

			out, stdout, _ := newOut()
			err := (&DisconnectCmd{KeepRemote: tc.localOnly}).Run(out)
			dels := f.callsTo("DELETE /v2/agents/agt_1")
			if tc.wantDelete != (len(dels) == 1) {
				t.Fatalf("delete calls = %+v", dels)
			}
			wantAuth := tc.wantAuth
			if wantAuth == "" {
				wantAuth = "Bearer " + tc.key
			}
			if len(dels) == 1 && dels[0].Auth != wantAuth {
				t.Fatalf("delete sent %q, want the person's key", dels[0].Auth)
			}
			_, statErr := os.Stat(envPath)
			if tc.keepsEnv {
				wantAgentErr(t, err, tc.code, exitcode.Forbidden)
				if statErr != nil {
					t.Fatal("agent.env deleted though the agent still exists")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !os.IsNotExist(statErr) {
				t.Fatal("agent.env still there")
			}
			res := decodeOut(t, stdout)
			if res["remote_removed"] != tc.removed || res["agent_env_deleted"] != true || res["profile_updated"] != true {
				t.Fatalf("result = %v", res)
			}
		})
	}
}

func TestDisconnectNothing(t *testing.T) {
	agentEnv(t)
	newFakeAPI(t)
	out, _, _ := newOut()
	wantAgentErr(t, (&DisconnectCmd{}).Run(out), exitcode.CodeAuthRequired, exitcode.AuthRequired)
}

func TestDisconnectDryRunKeepsEverything(t *testing.T) {
	_, dir := agentEnv(t)
	writeEnvFile(t, dir)
	f := newFakeAPI(t)
	orig := loadPersonKey
	loadPersonKey = func() string { return "oc_key_person" }
	t.Cleanup(func() { loadPersonKey = orig })
	out, stdout, _ := newOut()
	if err := (&DisconnectCmd{DryRun: true}).Run(out); err != nil {
		t.Fatal(err)
	}
	if decodeOut(t, stdout)["dry_run"] != true {
		t.Fatal("no dry run answer")
	}
	if len(f.callsTo("DELETE /v2/agents/agt_1")) != 0 {
		t.Fatal("dry run deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, agentenv.FileName)); err != nil {
		t.Fatal("dry run removed agent.env")
	}
}

// ---- attach ----

func TestAttachClaudeAndDisconnectUndoesIt(t *testing.T) {
	home, dir := agentEnv(t)
	writeEnvFile(t, dir)
	newFakeAPI(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"model":"x","env":{"MINE":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, stdout, _ := newOut()
	if err := (&AttachCmd{Agent: "claude"}).Run(out); err != nil {
		t.Fatal(err)
	}
	if res := decodeOut(t, stdout); len(res["attached"].([]any)) != 1 {
		t.Fatalf("result = %v", res)
	}
	skill, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "onecli", "SKILL.md"))
	if err != nil || !strings.HasPrefix(string(skill), "---\nname: onecli\n") || !strings.Contains(string(skill), "# OneCLI for agents") {
		t.Fatalf("skill = %q %v", skill, err)
	}
	var s map[string]any
	data, _ := os.ReadFile(settings)
	_ = json.Unmarshal(data, &s)
	env := s["env"].(map[string]any)
	if s["model"] != "x" || env["MINE"] != "1" || env["HTTPS_PROXY"] != "http://x:"+testCred+"@gw.example.com:10255" {
		t.Fatalf("settings = %v", s)
	}

	// The person changes one value attach wrote: disconnect must keep it.
	env["NO_PROXY"] = "localhost,mine.internal"
	data, _ = json.Marshal(s)
	if err := os.WriteFile(settings, data, 0o600); err != nil {
		t.Fatal(err)
	}
	orig := loadPersonKey
	loadPersonKey = func() string { return "" }
	t.Cleanup(func() { loadPersonKey = orig })
	out, _, _ = newOut()
	if err := (&DisconnectCmd{KeepRemote: true}).Run(out); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(settings)
	if strings.Contains(string(data), testCred) {
		t.Fatalf("the credential stays in settings.json after disconnect: %s", data)
	}
	s = nil
	_ = json.Unmarshal(data, &s)
	if s["model"] != "x" || s["env"].(map[string]any)["MINE"] != "1" || s["env"].(map[string]any)["NO_PROXY"] != "localhost,mine.internal" {
		t.Fatalf("disconnect removed what attach did not write: %v", s)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "onecli", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("skill still there")
	}
}

func TestAttachRefusals(t *testing.T) {
	t.Run("unknown agent", func(t *testing.T) {
		agentEnv(t)
		t.Setenv(agentenv.CredentialVar, testCred)
		newFakeAPI(t)
		out, _, _ := newOut()
		wantAgentErr(t, (&AttachCmd{Agent: "vim"}).Run(out), exitcode.CodeValidation, exitcode.Error)
	})
	t.Run("none found", func(t *testing.T) {
		agentEnv(t)
		t.Setenv(agentenv.CredentialVar, testCred)
		newFakeAPI(t)
		out, _, _ := newOut()
		wantAgentErr(t, (&AttachCmd{}).Run(out), exitcode.CodeNotFound, exitcode.NotFound)
	})
	t.Run("settings not plain JSON is left alone", func(t *testing.T) {
		home, _ := agentEnv(t)
		t.Setenv(agentenv.CredentialVar, testCred)
		newFakeAPI(t)
		settings := filepath.Join(home, ".claude", "settings.json")
		_ = os.MkdirAll(filepath.Dir(settings), 0o700)
		_ = os.WriteFile(settings, []byte("{ // comment\n}"), 0o600)
		out, _, _ := newOut()
		if err := (&AttachCmd{Agent: "claude"}).Run(out); err == nil {
			t.Fatal("attach wrote into a file it cannot read")
		}
		data, _ := os.ReadFile(settings)
		if string(data) != "{ // comment\n}" {
			t.Fatal("the file was changed")
		}
	})
	t.Run("not connected", func(t *testing.T) {
		agentEnv(t)
		newFakeAPI(t)
		out, _, _ := newOut()
		wantAgentErr(t, (&AttachCmd{Agent: "claude"}).Run(out), exitcode.CodeAuthRequired, exitcode.AuthRequired)
	})
}

func TestAttachDryRunWritesNothing(t *testing.T) {
	home, _ := agentEnv(t)
	t.Setenv(agentenv.CredentialVar, testCred)
	newFakeAPI(t)
	out, stdout, _ := newOut()
	if err := (&AttachCmd{Agent: "codex", DryRun: true}).Run(out); err != nil {
		t.Fatal(err)
	}
	if decodeOut(t, stdout)["dry_run"] != true {
		t.Fatal("no dry run answer")
	}
	if _, err := os.Stat(filepath.Join(home, ".agents")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote")
	}
}

// ---- exec ----

func TestExecSetsTheProxyForTheCommandOnly(t *testing.T) {
	_, dir := agentEnv(t)
	writeEnvFile(t, dir)
	if err := os.WriteFile(filepath.Join(dir, agentenv.CAFileName), []byte(testPEM), 0o644); err != nil {
		t.Fatal(err)
	}
	newFakeAPI(t)
	t.Setenv("HTTPS_PROXY", "http://corp-proxy:3128")
	var child bytes.Buffer
	origOut := execStdout
	execStdout = &child
	t.Cleanup(func() { execStdout = origOut })
	out, _, _ := newOut()
	if err := (&ExecCmd{Args: []string{"--", "env"}}).Run(out); err != nil {
		t.Fatal(err)
	}
	env := child.String()
	for _, want := range []string{
		"HTTPS_PROXY=http://x:" + testCred + "@gw.example.com:10255",
		"SSL_CERT_FILE=", "NODE_EXTRA_CA_CERTS=", "ONECLI_AGENT_CREDENTIAL=" + testCred,
	} {
		if !strings.Contains(env, want) {
			t.Errorf("child env lacks %q", want)
		}
	}
	if strings.Contains(env, "corp-proxy") {
		t.Error("the inherited proxy shadows the gateway")
	}
	if os.Getenv("HTTPS_PROXY") != "http://corp-proxy:3128" {
		t.Error("exec changed its own environment")
	}
}

func TestExecPassesTheExitCode(t *testing.T) {
	agentEnv(t)
	t.Setenv(agentenv.CredentialVar, testCred)
	newFakeAPI(t)
	out, _, _ := newOut()
	err := (&ExecCmd{NoCA: true, Args: []string{"sh", "-c", "exit 7"}}).Run(out)
	var es *exitStatus
	if !errors.As(err, &es) || es.code != 7 {
		t.Fatalf("err = %v", err)
	}
}

func TestExecRefusals(t *testing.T) {
	t.Run("no command", func(t *testing.T) {
		agentEnv(t)
		out, _, _ := newOut()
		wantAgentErr(t, (&ExecCmd{Args: []string{"--"}}).Run(out), exitcode.CodeValidation, exitcode.Error)
	})
	t.Run("unknown command", func(t *testing.T) {
		agentEnv(t)
		out, _, _ := newOut()
		wantAgentErr(t, (&ExecCmd{Args: []string{"no-such-binary-onecli"}}).Run(out), exitcode.CodeNotFound, exitcode.NotFound)
	})
	t.Run("not connected", func(t *testing.T) {
		agentEnv(t)
		newFakeAPI(t)
		out, _, _ := newOut()
		wantAgentErr(t, (&ExecCmd{Args: []string{"true"}}).Run(out), exitcode.CodeAuthRequired, exitcode.AuthRequired)
	})
	t.Run("dry run runs nothing and prints no secret", func(t *testing.T) {
		agentEnv(t)
		t.Setenv(agentenv.CredentialVar, testCred)
		newFakeAPI(t)
		marker := filepath.Join(t.TempDir(), "ran")
		out, stdout, _ := newOut()
		if err := (&ExecCmd{DryRun: true, Args: []string{"touch", marker}}).Run(out); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("dry run ran the command")
		}
		if strings.Contains(stdout.String(), testCred) {
			t.Fatal("dry run printed the credential")
		}
	})
}

// ---- the gateway certificate's folder ----

func TestConnectRefusesASharedDirBeforeWritingTheCertificate(t *testing.T) {
	_, dir := agentEnv(t)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	f := newFakeAPI(t)
	out, _, _ := newOut()
	err := (&ConnectCmd{Token: testToken, NoProfile: true}).Run(out)
	if err == nil || !strings.Contains(err.Error(), "writable by group or others") {
		t.Fatalf("err = %v, want a refusal of the shared dir", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, agentenv.CAFileName)); !os.IsNotExist(err) {
		t.Fatal("the gateway certificate was written into a shared dir")
	}
	if n := len(f.callsTo("POST /v2/agents")); n != 0 {
		t.Fatalf("the connect token was traded %d times", n)
	}
}

func TestGatewayPEMIgnoresACertificateInASharedDir(t *testing.T) {
	_, dir := agentEnv(t)
	newFakeAPI(t)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	planted := "-----BEGIN CERTIFICATE-----\nplanted\n-----END CERTIFICATE-----\n"
	if err := os.WriteFile(filepath.Join(dir, agentenv.CAFileName), []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
	// Private dir: the file next to agent.env is used.
	got, err := gatewayPEM(newAgentAPI(), dir)
	if err != nil || string(got) != planted {
		t.Fatalf("private dir: got %q, %v; want the local file", got, err)
	}
	// Shared dir: the file may be planted, so the server's is fetched.
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	got, err = gatewayPEM(newAgentAPI(), dir)
	if err != nil || string(got) != testPEM {
		t.Fatalf("shared dir: got %q, %v; want the server's certificate", got, err)
	}
}

// ---- the certificate bundle ----

func TestConnectWritesTheBundleAndPointsAgentEnvAtIt(t *testing.T) {
	_, dir := agentEnv(t)
	newFakeAPI(t)
	out, _, _ := newOut()
	if err := (&ConnectCmd{Token: testToken, NoProfile: true}).Run(out); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(dir, agentenv.CABundleFileName)
	data, err := os.ReadFile(bundle)
	if err != nil || string(data) != testRootPEM+testPEM {
		t.Fatalf("bundle = %q, %v; want roots then the gateway certificate", data, err)
	}
	vals, _ := agentenv.Read(filepath.Join(dir, agentenv.FileName))
	if vals["SSL_CERT_FILE"] != bundle || vals["REQUESTS_CA_BUNDLE"] != bundle || vals["GIT_SSL_CAINFO"] != bundle || vals["CURL_CA_BUNDLE"] != bundle {
		t.Fatalf("agent.env does not point the replacing clients at the bundle: %v", vals)
	}
	if vals["NODE_EXTRA_CA_CERTS"] != filepath.Join(dir, agentenv.CAFileName) {
		t.Fatalf("NODE_EXTRA_CA_CERTS = %q", vals["NODE_EXTRA_CA_CERTS"])
	}
}

func TestConnectWithNoSystemRootsFailsBeforeTheTrade(t *testing.T) {
	_, dir := agentEnv(t)
	agentenv.SystemCAPaths = []string{"/no/such/file"}
	f := newFakeAPI(t)
	out, _, _ := newOut()
	err := (&ConnectCmd{Token: testToken, NoProfile: true}).Run(out)
	if !errors.Is(err, agentenv.ErrNoSystemRoots) {
		t.Fatalf("err = %v, want ErrNoSystemRoots", err)
	}
	if n := len(f.callsTo("POST /v2/agents")); n != 0 {
		t.Fatalf("the connect token was traded %d times", n)
	}
	for _, name := range []string{agentenv.FileName, agentenv.CABundleFileName} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s was written", name)
		}
	}
}

// ---- the credential's input ----

func credentialWritten(t *testing.T, dir string) {
	t.Helper()
	vals, _ := agentenv.Read(filepath.Join(dir, agentenv.FileName))
	if vals[agentenv.CredentialVar] != testCred {
		t.Fatalf("agent.env = %v", vals)
	}
}

func TestConnectCredentialInputs(t *testing.T) {
	t.Run("stdin", func(t *testing.T) {
		_, dir := agentEnv(t)
		newFakeAPI(t)
		orig := stdin
		stdin = strings.NewReader(testCred + "\n")
		t.Cleanup(func() { stdin = orig })
		out, _, stderr := newOut()
		if err := (&ConnectCmd{Credential: "-", NoProfile: true}).Run(out); err != nil {
			t.Fatal(err)
		}
		credentialWritten(t, dir)
		if strings.Contains(stderr.String(), "warning") {
			t.Fatal("stdin input got the deprecation note")
		}
	})
	t.Run("from_env", func(t *testing.T) {
		_, dir := agentEnv(t)
		t.Setenv(agentenv.CredentialVar, testCred)
		newFakeAPI(t)
		out, _, stderr := newOut()
		if err := (&ConnectCmd{FromEnv: true, NoProfile: true}).Run(out); err != nil {
			t.Fatal(err)
		}
		credentialWritten(t, dir)
		if strings.Contains(stderr.String(), "warning") {
			t.Fatal("env input got the deprecation note")
		}
	})
	// Control: the old form keeps working, with the note, and the note never
	// holds the credential.
	t.Run("argv_still_works_with_a_note", func(t *testing.T) {
		_, dir := agentEnv(t)
		newFakeAPI(t)
		out, _, stderr := newOut()
		if err := (&ConnectCmd{Credential: testCred, NoProfile: true}).Run(out); err != nil {
			t.Fatal(err)
		}
		credentialWritten(t, dir)
		if !strings.Contains(stderr.String(), "--credential -") {
			t.Fatalf("no deprecation note: %q", stderr.String())
		}
		if strings.Contains(stderr.String(), testCred) {
			t.Fatal("the note printed the credential")
		}
	})
	t.Run("refusals", func(t *testing.T) {
		agentEnv(t)
		newFakeAPI(t)
		out, _, _ := newOut()
		wantAgentErr(t, (&ConnectCmd{FromEnv: true}).Run(out), exitcode.CodeValidation, exitcode.Error)
		wantAgentErr(t, (&ConnectCmd{FromEnv: true, Credential: "-"}).Run(out), exitcode.CodeValidation, exitcode.Error)
		wantAgentErr(t, (&ConnectCmd{Token: "-", Credential: "-"}).Run(out), exitcode.CodeValidation, exitcode.Error)
		t.Setenv(agentenv.CredentialVar, testCred)
		wantAgentErr(t, (&ConnectCmd{Token: testToken, FromEnv: true}).Run(out), exitcode.CodeValidation, exitcode.Error)
	})
}

func TestFetchCAWarnsOnPlainHTTPOnly(t *testing.T) {
	var warned []string
	orig := warnLine
	warnLine = func(msg string) { warned = append(warned, msg) }
	t.Cleanup(func() { warnLine = orig })

	f := newFakeAPI(t)
	if _, err := fetchCA(context.Background(), agentapi.New(f.srv.URL)); err != nil {
		t.Fatal(err)
	}
	if len(warned) != 0 {
		t.Fatalf("loopback warned: %v", warned)
	}
	// A plain http host that is not loopback warns once, before any call.
	// The fetch itself fails (the host does not exist); only the warning counts.
	_, _ = fetchCA(context.Background(), agentapi.New("http://onecli.invalid"))
	if len(warned) != 1 || !strings.Contains(warned[0], "plain http") {
		t.Fatalf("warned = %v", warned)
	}
}
