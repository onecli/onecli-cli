package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
)

func TestParsePgURL(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"postgres scheme", "postgres://u:p@db.example.com:5432/app", true},
		{"postgresql scheme", "postgresql://u:p@db.example.com/app", true},
		{"quoted value", `"postgresql://u:p@db.example.com/app"`, true},
		{"not a db url", "https://example.com", false},
		{"empty", "", false},
		{"unix socket (no host)", "postgresql:///dbname?host=/var/run/postgresql", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePgURL(tc.value)
			if (got != nil) != tc.want {
				t.Errorf("parsePgURL(%q) = %v, want match=%v", tc.value, got, tc.want)
			}
		})
	}
}

func TestScanPgURLsEnvWinsOverDotenv(t *testing.T) {
	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	if err := os.WriteFile(dotenv, []byte(
		"# comment\n"+
			"DATABASE_URL=postgresql://file:pw@file-host:5432/app\n"+
			"ONLY_IN_FILE=postgres://only:pw@file-only-host/db\n"+
			"NOT_A_DB=hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	environ := []string{
		"DATABASE_URL=postgresql://env:pw@env-host:5433/app",
		"PATH=/usr/bin",
	}
	scans := scanPgURLs(environ, dir)

	byName := map[string]pgScanResult{}
	for _, s := range scans {
		byName[s.VarName] = s
	}
	if len(byName) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(byName), byName)
	}
	if got := byName["DATABASE_URL"].URL.Hostname(); got != "env-host" {
		t.Errorf("env should win over .env, got host %q", got)
	}
	if byName["DATABASE_URL"].FromEnvFile {
		t.Error("DATABASE_URL should be marked as from the process env")
	}
	if got := byName["ONLY_IN_FILE"].URL.Hostname(); got != "file-only-host" {
		t.Errorf("file-only var missing, got %q", got)
	}
}

func TestParseDotenvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "export EXPORTED=postgres://a:b@h/db\n" +
		"QUOTED='postgresql://q:w@qh/db'\n" +
		"WITH_COMMENT=postgres://c:d@ch/db # prod!\n" +
		"BROKEN\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got := parseDotenvFile(path)
	if got["EXPORTED"] != "postgres://a:b@h/db" {
		t.Errorf("export prefix not stripped: %q", got["EXPORTED"])
	}
	if got["QUOTED"] != "'postgresql://q:w@qh/db'" {
		t.Errorf("quoted value mangled: %q", got["QUOTED"])
	}
	if got["WITH_COMMENT"] != "postgres://c:d@ch/db" {
		t.Errorf("inline comment not trimmed: %q", got["WITH_COMMENT"])
	}
	if _, ok := got["BROKEN"]; ok {
		t.Error("malformed line should be skipped")
	}
	if parseDotenvFile(filepath.Join(dir, "missing")) != nil {
		t.Error("missing file should return nil")
	}
}

func TestBuildPgProxyURL(t *testing.T) {
	orig, _ := url.Parse("postgresql://real:secret@db.example.com:5432/app?schema=public&sslmode=require&connect_timeout=5")

	got := buildPgProxyURL(orig, "aoc_pg_tok", "127.0.0.1", "", 6432)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("proxy URL does not parse: %v", err)
	}
	if u.Hostname() != "127.0.0.1" || u.Port() != "6432" {
		t.Errorf("wrong proxy host: %s", u.Host)
	}
	if u.User.Username() != "aoc_pg_tok" {
		t.Errorf("token not in username: %s", u.User.Username())
	}
	if pw, _ := u.User.Password(); pw != "x" {
		t.Errorf("expected dummy password, got %q", pw)
	}
	if u.Path != "/app" {
		t.Errorf("database path not preserved: %q", u.Path)
	}
	q := u.Query()
	if q.Get("schema") != "public" || q.Get("connect_timeout") != "5" {
		t.Errorf("client-semantic params dropped: %v", q)
	}
	if q.Get("sslmode") != "disable" {
		t.Errorf("loopback leg should pin sslmode=disable, got %q", q.Get("sslmode"))
	}
	// The real password must never appear anywhere in the proxy URL.
	if containsSubstring(got, "secret") {
		t.Errorf("real credential leaked into proxy URL: %s", got)
	}

	// Remote gateway WITH the installed CA ⇒ verify-full + sslrootcert.
	remote := buildPgProxyURL(orig, "aoc_pg_tok", "gw.example.com", "/home/u/.onecli/ca-bundle.pem", 6432)
	ru, _ := url.Parse(remote)
	if ru.Query().Get("sslmode") != "verify-full" {
		t.Errorf("remote+CA leg should verify-full, got %q", ru.Query().Get("sslmode"))
	}
	if ru.Query().Get("sslrootcert") != "/home/u/.onecli/ca-bundle.pem" {
		t.Errorf("remote+CA leg should set sslrootcert, got %q", ru.Query().Get("sslrootcert"))
	}

	// Remote gateway WITHOUT a CA path ⇒ require (encrypted, unverified).
	remoteNoCA := buildPgProxyURL(orig, "aoc_pg_tok", "gw.example.com", "", 6432)
	rnu, _ := url.Parse(remoteNoCA)
	if rnu.Query().Get("sslmode") != "require" {
		t.Errorf("remote-without-CA leg should require TLS, got %q", rnu.Query().Get("sslmode"))
	}
}

func TestGatewayOriginAndToken(t *testing.T) {
	origin, token, ok := gatewayOriginAndToken("http://x:aoc_abc123@127.0.0.1:10254")
	if !ok || origin != "http://127.0.0.1:10254" || token != "aoc_abc123" {
		t.Errorf("got (%q,%q,%v)", origin, token, ok)
	}
	// Username-position token.
	origin, token, ok = gatewayOriginAndToken("http://aoc_zzz@gw:1")
	if !ok || origin != "http://gw:1" || token != "aoc_zzz" {
		t.Errorf("got (%q,%q,%v)", origin, token, ok)
	}
	if _, _, ok := gatewayOriginAndToken("http://user:pw@host:1"); ok {
		t.Error("non-aoc credentials must not be treated as a token")
	}
	if _, _, ok := gatewayOriginAndToken("not a url"); ok {
		t.Error("garbage must not parse")
	}
}

func TestRemoveEnvKey(t *testing.T) {
	env := []string{"A=1", "DATABASE_URL=x", "B=2", "DATABASE_URL=y"}
	got := removeEnvKey(env, "DATABASE_URL")
	if len(got) != 2 || got[0] != "A=1" || got[1] != "B=2" {
		t.Errorf("got %v", got)
	}
}

func TestParsePgSidecarArgs(t *testing.T) {
	t.Setenv("ONECLI_PG_SIDECAR_TOKEN", "aoc_tok")
	args, ok := parsePgSidecarArgs([]string{
		"--parent", "1234",
		"--gateway", "http://127.0.0.1:10254",
		"--sessions", "s1,s2",
		"--ttl", "900",
	})
	if !ok {
		t.Fatal("expected ok")
	}
	if args.parentPID != 1234 || args.gatewayURL != "http://127.0.0.1:10254" || len(args.sessionIDs) != 2 {
		t.Errorf("got %+v", args)
	}
	if args.ttlSeconds != 900 {
		t.Errorf("ttl not parsed: got %d", args.ttlSeconds)
	}

	t.Setenv("ONECLI_PG_SIDECAR_TOKEN", "")
	if _, ok := parsePgSidecarArgs([]string{"--parent", "1", "--gateway", "g", "--sessions", "s"}); ok {
		t.Error("missing token must fail")
	}
}

func TestMatchPgScans(t *testing.T) {
	mk := func(raw string) pgScanResult {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return pgScanResult{VarName: "V", URL: u}
	}
	conns := []api.PgConnection{
		{ID: "c1", Host: "db.example.com", Port: 5432},
		{ID: "c2", Host: "other.example.com", Port: 6543},
	}

	matches := matchPgScans([]pgScanResult{
		mk("postgres://u:p@db.example.com/app"),         // default port matches c1
		mk("postgres://u:p@db.example.com:6543/app"),    // wrong port — no match
		mk("postgres://u:p@other.example.com:6543/x"),   // matches c2
		mk("postgres://u:p@unknown.example.com:5432/y"), // unregistered
	}, conns)

	if matches[0].Conn == nil || matches[0].Conn.ID != "c1" {
		t.Errorf("default-port match failed: %+v", matches[0])
	}
	if matches[1].Conn != nil {
		t.Error("port mismatch must not match")
	}
	if matches[2].Conn == nil || matches[2].Conn.ID != "c2" {
		t.Errorf("nonstandard-port match failed: %+v", matches[2])
	}
	if matches[3].Conn != nil {
		t.Error("unregistered host must not match")
	}
}

func TestLibpqScan(t *testing.T) {
	s := libpqScan(map[string]string{
		"PGHOST": "db.example.com", "PGPORT": "5433",
		"PGUSER": "app", "PGPASSWORD": "secret", "PGDATABASE": "mydb",
	})
	if s == nil || !s.Libpq {
		t.Fatalf("expected a libpq scan, got %+v", s)
	}
	if s.URL.Hostname() != "db.example.com" || s.URL.Port() != "5433" {
		t.Errorf("wrong host:port: %s", s.URL.Host)
	}
	if s.URL.Path != "/mydb" {
		t.Errorf("database not carried into synthetic URL: %q", s.URL.Path)
	}
	// Path-style PGHOST (unix socket) is deliberately skipped.
	if libpqScan(map[string]string{"PGHOST": "/var/run/postgresql"}) != nil {
		t.Error("unix-socket PGHOST must be skipped")
	}
	// No PGHOST → no scan.
	if libpqScan(map[string]string{"PGUSER": "x"}) != nil {
		t.Error("missing PGHOST must produce no scan")
	}
	// PGPORT unset defaults to 5432.
	d := libpqScan(map[string]string{"PGHOST": "h"})
	if d == nil || d.URL.Port() != "5432" {
		t.Errorf("expected default port 5432, got %+v", d)
	}
}

func TestScanPgURLsLibpqGroup(t *testing.T) {
	environ := []string{"PGHOST=env-host", "PGPORT=5432", "PGUSER=app", "PATH=/usr/bin"}
	scans := scanPgURLs(environ, t.TempDir())
	var lib *pgScanResult
	for i := range scans {
		if scans[i].Libpq {
			lib = &scans[i]
		}
	}
	if lib == nil {
		t.Fatal("libpq group not scanned")
	}
	if lib.URL.Hostname() != "env-host" {
		t.Errorf("wrong libpq host: %s", lib.URL.Hostname())
	}
}

func TestBuildLibpqEnv(t *testing.T) {
	loop := buildLibpqEnv("aoc_pg_tok", "127.0.0.1", "", 6432)
	if loop["PGHOST"] != "127.0.0.1" || loop["PGPORT"] != "6432" {
		t.Errorf("wrong host/port: %v", loop)
	}
	if loop["PGUSER"] != "aoc_pg_tok" || loop["PGPASSWORD"] != "x" {
		t.Errorf("wrong user/password: %v", loop)
	}
	if loop["PGSSLMODE"] != "disable" {
		t.Errorf("loopback should disable TLS, got %q", loop["PGSSLMODE"])
	}
	if _, ok := loop["PGDATABASE"]; ok {
		t.Error("PGDATABASE must pass through, never be emitted")
	}
	rem := buildLibpqEnv("aoc_pg_tok", "gw.example.com", "/ca.pem", 6432)
	if rem["PGSSLMODE"] != "verify-full" || rem["PGSSLROOTCERT"] != "/ca.pem" {
		t.Errorf("remote+CA wrong TLS: %v", rem)
	}
	rn := buildLibpqEnv("aoc_pg_tok", "gw.example.com", "", 6432)
	if rn["PGSSLMODE"] != "require" {
		t.Errorf("remote no-CA should require TLS, got %q", rn["PGSSLMODE"])
	}
}

func TestMatchPgScansHostNormalization(t *testing.T) {
	u, _ := url.Parse("postgres://u:p@db.example.com./app") // lowercase + trailing dot
	conns := []api.PgConnection{{ID: "c1", Host: "DB.Example.COM", Port: 5432}}
	matches := matchPgScans([]pgScanResult{{VarName: "V", URL: u}}, conns)
	if matches[0].Conn == nil || matches[0].Conn.ID != "c1" {
		t.Errorf("case/trailing-dot host should match: %+v", matches[0])
	}
}

func TestHeartbeatInterval(t *testing.T) {
	if got := heartbeatInterval(0); got != pgHeartbeatDefault {
		t.Errorf("ttl=0 should use default, got %v", got)
	}
	if got := heartbeatInterval(900); got != 300*time.Second {
		t.Errorf("ttl=900 should be 300s, got %v", got)
	}
	if got := heartbeatInterval(30); got != pgHeartbeatFloor {
		t.Errorf("ttl=30 should clamp to floor, got %v", got)
	}
}

func TestSetupPgProxyRequiredListFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := &api.PgGatewayClient{BaseURL: srv.URL, AgentToken: "aoc_x"}
	environ := []string{"DATABASE_URL=postgres://u:p@db.example.com:5432/app"}
	out := output.NewWithWriters(io.Discard, io.Discard)

	if _, err := setupPgProxy(out, client, "127.0.0.1", "", environ, t.TempDir(), true); err == nil {
		t.Error("required mode must error when the gateway list fails")
	}
	outcome, err := setupPgProxy(out, client, "127.0.0.1", "", environ, t.TempDir(), false)
	if err != nil || outcome != nil {
		t.Errorf("default mode must fail open, got (%v, %v)", outcome, err)
	}
}

func TestSetupPgProxyRequiredMintFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(api.PgConnectionsResponse{
				Connections: []api.PgConnection{{ID: "c1", Host: "db.example.com", Port: 5432}},
				ListenPort:  6432,
			})
			return
		}
		w.WriteHeader(http.StatusInternalServerError) // mint fails
	}))
	defer srv.Close()
	client := &api.PgGatewayClient{BaseURL: srv.URL, AgentToken: "aoc_x"}
	environ := []string{"DATABASE_URL=postgres://u:p@db.example.com:5432/app"}
	out := output.NewWithWriters(io.Discard, io.Discard)

	if _, err := setupPgProxy(out, client, "127.0.0.1", "", environ, t.TempDir(), true); err == nil {
		t.Error("required mode must error when session mint fails for a matched db")
	}
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
