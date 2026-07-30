package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"io"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
)

func strPtr(s string) *string { return &s }

func TestPlaceholderVarName(t *testing.T) {
	cases := []struct {
		name  string
		label *string
		id    string
		want  string
	}{
		{"simple label", strPtr("main-db"), "id1", "ONECLI_PG_MAIN_DB_URL"},
		{"spaces and case", strPtr("Analytics Prod"), "id2", "ONECLI_PG_ANALYTICS_PROD_URL"},
		{"symbols collapse", strPtr("a--b__c!!d"), "id3", "ONECLI_PG_A_B_C_D_URL"},
		{"leading trailing trim", strPtr("--x--"), "id4", "ONECLI_PG_X_URL"},
		{"no label uses id prefix", nil, "abcdef1234567890", "ONECLI_PG_ABCDEF12_URL"},
		{"unicode stripped", strPtr("caf\u00e9"), "zz11xx22", "ONECLI_PG_CAF_URL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := placeholderVarName(api.PgConnection{ID: tc.id, Label: tc.label}, map[string]bool{})
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPlaceholderVarNameCollisions(t *testing.T) {
	taken := map[string]bool{}
	a := placeholderVarName(api.PgConnection{ID: "1", Label: strPtr("main db")}, taken)
	b := placeholderVarName(api.PgConnection{ID: "2", Label: strPtr("main-db")}, taken)
	c := placeholderVarName(api.PgConnection{ID: "3", Label: strPtr("MAIN_DB")}, taken)
	if a != "ONECLI_PG_MAIN_DB_URL" {
		t.Errorf("first = %q", a)
	}
	if b != "ONECLI_PG_MAIN_DB_2_URL" || c != "ONECLI_PG_MAIN_DB_3_URL" {
		t.Errorf("collisions = %q, %q; want _2/_3 suffixes", b, c)
	}
}

func TestPlaceholderPgURL(t *testing.T) {
	session := &api.PgSession{User: "aoc_pg_tok", ListenPort: 6432}

	t.Run("pinned database rides the path", func(t *testing.T) {
		s := *session
		s.Database = strPtr("appdb")
		u, err := url.Parse(placeholderPgURL(&s, "gw.example.com", "/ca.pem"))
		if err != nil {
			t.Fatal(err)
		}
		if u.Path != "/appdb" {
			t.Errorf("path = %q, want /appdb", u.Path)
		}
		if u.Query().Get("sslmode") != "verify-full" {
			t.Errorf("sslmode = %q", u.Query().Get("sslmode"))
		}
	})

	t.Run("no pin falls back to postgres, never the aoc user", func(t *testing.T) {
		u, err := url.Parse(placeholderPgURL(session, "127.0.0.1", ""))
		if err != nil {
			t.Fatal(err)
		}
		if u.Path != "/postgres" {
			t.Errorf("path = %q, want /postgres (libpq would default to the aoc_pg_ username)", u.Path)
		}
		if u.Query().Get("sslmode") != "disable" {
			t.Errorf("loopback sslmode = %q", u.Query().Get("sslmode"))
		}
	})
}

func TestPreMintPlaceholders(t *testing.T) {
	conns := []api.PgConnection{
		{ID: "covered", Label: strPtr("covered-db"), Host: "a.example.com", Port: 5432},
		{ID: "extra", Label: strPtr("extra-db"), Host: "b.example.com", Port: 5432},
		{ID: "failing", Label: strPtr("bad-db"), Host: "c.example.com", Port: 5432},
	}
	minted := map[string]*api.PgSession{
		"covered": {SessionID: "s1", User: "aoc_pg_a", ListenPort: 6432, TTLSeconds: 900},
	}
	mint := func(id string) (*api.PgSession, error) {
		if id == "failing" {
			return nil, fmt.Errorf("boom")
		}
		return &api.PgSession{SessionID: "s-" + id, User: "aoc_pg_" + id, ListenPort: 6432, TTLSeconds: 900}, nil
	}

	res := preMintPlaceholders(
		conns, minted, map[string]string{"covered": "DATABASE_URL"}, mint, "127.0.0.1", "")
	placeholders, index, warnings := res.Placeholders, res.Index, res.Warnings

	if len(placeholders) != 1 {
		t.Fatalf("placeholders = %v, want exactly the extra connection", placeholders)
	}
	if _, ok := placeholders["ONECLI_PG_EXTRA_DB_URL"]; !ok {
		t.Errorf("missing ONECLI_PG_EXTRA_DB_URL: %v", placeholders)
	}
	// Failing mint warns, never aborts.
	if len(warnings) != 1 || !strings.Contains(warnings[0], "c.example.com") {
		t.Errorf("warnings = %v", warnings)
	}
	// The index lists BOTH governed routes: swapped var + placeholder.
	byLabel := map[string]pgIndexEntry{}
	for _, e := range index {
		byLabel[e.Label] = e
	}
	if byLabel["covered-db"].EnvVar != "DATABASE_URL" {
		t.Errorf("covered entry = %+v", byLabel["covered-db"])
	}
	if byLabel["extra-db"].EnvVar != "ONECLI_PG_EXTRA_DB_URL" {
		t.Errorf("extra entry = %+v", byLabel["extra-db"])
	}
	if _, listed := byLabel["bad-db"]; listed {
		t.Error("a connection without a working route must not be advertised")
	}
	// The extra session landed in the shared mint map for reuse AND is
	// reported as newly minted (the caller's sidecar registration set).
	if minted["extra"] == nil || minted["extra"].SessionID != "s-extra" {
		t.Errorf("minted map = %v", minted)
	}
	if len(res.NewSessions) != 1 || res.NewSessions[0].SessionID != "s-extra" {
		t.Errorf("NewSessions = %v, want exactly the extra session", res.NewSessions)
	}
}

func TestPreMintPlaceholdersCap(t *testing.T) {
	var conns []api.PgConnection
	for i := 0; i < pgPreMintMax+3; i++ {
		conns = append(conns, api.PgConnection{
			ID: fmt.Sprintf("c%d", i), Label: strPtr(fmt.Sprintf("db%d", i)),
			Host: fmt.Sprintf("h%d.example.com", i), Port: 5432,
		})
	}
	mint := func(id string) (*api.PgSession, error) {
		return &api.PgSession{SessionID: "s-" + id, User: "aoc_pg_x", ListenPort: 6432, TTLSeconds: 900}, nil
	}
	res := preMintPlaceholders(conns, map[string]*api.PgSession{}, map[string]string{}, mint, "127.0.0.1", "")
	if len(res.Placeholders) != pgPreMintMax {
		t.Errorf("placeholders = %d, want cap %d", len(res.Placeholders), pgPreMintMax)
	}
	if len(res.Warnings) != 3 {
		t.Errorf("warnings = %d, want 3 over-cap notices", len(res.Warnings))
	}
}

func TestEncodePgIndex(t *testing.T) {
	if encodePgIndex(nil) != "" {
		t.Error("empty index must encode to empty string")
	}
	got := encodePgIndex([]pgIndexEntry{{Label: "main", Host: "db.example.com:5432", EnvVar: "ONECLI_PG_MAIN_URL"}})
	var back []pgIndexEntry
	if err := json.Unmarshal([]byte(got), &back); err != nil || len(back) != 1 || back[0].EnvVar != "ONECLI_PG_MAIN_URL" {
		t.Errorf("round-trip failed: %q (%v)", got, err)
	}
	if strings.Contains(got, "$") {
		t.Error("index must carry var NAMES, not $references (env values do not interpolate)")
	}
}

// End-to-end through setupPgProxy: an empty env still yields placeholders
// for every granted connection, and the sidecar gets their sessions.
func TestSetupPgProxyPreMintsWithEmptyEnv(t *testing.T) {
	var mintCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(api.PgConnectionsResponse{
				Connections: []api.PgConnection{
					{ID: "c1", Label: strPtr("main"), Host: "db.example.com", Port: 5432},
				},
				ListenPort: 6432,
			})
			return
		}
		mintCalls++
		_ = json.NewEncoder(w).Encode(api.PgSession{
			SessionID: "s1", Token: "tok", User: "aoc_pg_tok", ListenPort: 6432, TTLSeconds: 900,
		})
	}))
	defer srv.Close()

	client := &api.PgGatewayClient{BaseURL: srv.URL, AgentToken: "aoc_x"}
	out := output.NewWithWriters(io.Discard, io.Discard)
	outcome, err := setupPgProxy(out, client, "127.0.0.1", "", nil, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if outcome == nil {
		t.Fatal("outcome must not be nil when granted connections exist")
	}
	if mintCalls != 1 {
		t.Errorf("mint calls = %d, want 1", mintCalls)
	}
	if len(outcome.Placeholders) != 1 || outcome.Placeholders["ONECLI_PG_MAIN_URL"] == "" {
		t.Errorf("placeholders = %v", outcome.Placeholders)
	}
	if len(outcome.SessionIDs) != 1 || outcome.SessionIDs[0] != "s1" {
		t.Errorf("sessions for sidecar = %v", outcome.SessionIDs)
	}
	if outcome.TTLSeconds != 900 {
		t.Errorf("ttl = %d", outcome.TTLSeconds)
	}
	if outcome.IndexJSON == "" || !strings.Contains(outcome.IndexJSON, "ONECLI_PG_MAIN_URL") {
		t.Errorf("index = %q", outcome.IndexJSON)
	}
}

// Pre-mint failures must not abort even under --pg-proxy=required: only
// databases the env actually references are covered by required.
func TestSetupPgProxyRequiredIgnoresPreMintFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(api.PgConnectionsResponse{
				Connections: []api.PgConnection{
					{ID: "c1", Label: strPtr("unused"), Host: "other.example.com", Port: 5432},
				},
				ListenPort: 6432,
			})
			return
		}
		w.WriteHeader(http.StatusInternalServerError) // every mint fails
	}))
	defer srv.Close()

	client := &api.PgGatewayClient{BaseURL: srv.URL, AgentToken: "aoc_x"}
	out := output.NewWithWriters(io.Discard, io.Discard)
	// Env has NO postgres URL: nothing matched, so required has nothing
	// to enforce; the pre-mint failure stays a warning.
	outcome, err := setupPgProxy(out, client, "127.0.0.1", "", nil, t.TempDir(), true)
	if err != nil {
		t.Errorf("pre-mint failure aborted a required run: %v", err)
	}
	if outcome != nil {
		t.Errorf("no governed route established, outcome should be nil, got %+v", outcome)
	}
}
