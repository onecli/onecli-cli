package main

// Pre-mint + advertise: alongside swapping URLs the agent's env already
// carries, `onecli run` mints a proxy session for EVERY registered
// connection the agent is granted and exports one placeholder variable
// per connection (ONECLI_PG_<LABEL>_URL) plus a machine-readable index
// (ONECLI_PG_CONNECTIONS). The gateway skill instructs the agent: any
// postgres host in the index is reached ONLY via its mapped variable —
// even when the user pastes credentials — because a governed route always
// exists for every granted database. This closes the "registered but not
// in the env at spawn" gap where the agent had no proxy URL to use and
// dialed the real host directly.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/onecli/onecli-cli/internal/api"
)

// pgPreMintMax bounds how many un-scanned granted connections are
// pre-minted per run. Guards against a project with a huge connection
// inventory turning every spawn into a mint storm; past the cap the
// remaining databases are still governed when their URL appears in the
// env (the scan path), they just get no placeholder.
const pgPreMintMax = 16

// pgIndexVar is the env var carrying the JSON index of governed databases.
const pgIndexVar = "ONECLI_PG_CONNECTIONS"

// pgIndexEntry is one row of the ONECLI_PG_CONNECTIONS JSON index. EnvVar
// names the variable holding the proxy URL — a NAME, not a $reference:
// env values do not interpolate, so the agent reads the named var itself.
type pgIndexEntry struct {
	Label  string `json:"label"`
	Host   string `json:"host"`
	EnvVar string `json:"env_var"`
}

var pgLabelSanitizer = regexp.MustCompile(`[^A-Z0-9]+`)

// placeholderVarName derives ONECLI_PG_<LABEL>_URL from a connection's
// label (falling back to the connection id), sanitized to env-var-safe
// characters. `taken` de-duplicates collisions ("main db" vs "main-db")
// with a numeric suffix; the caller passes the same map across calls.
func placeholderVarName(conn api.PgConnection, taken map[string]bool) string {
	base := ""
	if conn.Label != nil {
		base = strings.ToUpper(*conn.Label)
	}
	base = strings.Trim(pgLabelSanitizer.ReplaceAllString(base, "_"), "_")
	if base == "" {
		// No usable label: an id prefix is stable and unique enough.
		id := strings.ToUpper(conn.ID)
		id = strings.Trim(pgLabelSanitizer.ReplaceAllString(id, "_"), "_")
		if len(id) > 8 {
			id = id[:8]
		}
		base = id
	}
	name := "ONECLI_PG_" + base + "_URL"
	for i := 2; taken[name]; i++ {
		name = fmt.Sprintf("ONECLI_PG_%s_%d_URL", base, i)
	}
	taken[name] = true
	return name
}

// placeholderPgURL builds the proxy URL for a pre-minted connection with
// no client-supplied URL to preserve. The database path is the pinned
// database from the mint response, else "postgres" — NEVER empty, because
// libpq defaults a missing database to the username (aoc_pg_...), which
// does not exist upstream. TLS parameters come from buildPgProxyURL so
// the two paths cannot drift.
func placeholderPgURL(session *api.PgSession, gatewayHost, caPath string) string {
	db := "postgres"
	if session.Database != nil && strings.TrimSpace(*session.Database) != "" {
		db = strings.TrimSpace(*session.Database)
	}
	synthetic := &url.URL{Scheme: "postgresql", Path: "/" + db}
	return buildPgProxyURL(synthetic, session.User, gatewayHost, caPath, session.ListenPort)
}

// preMintResult is what preMintPlaceholders produced, separated from the
// caller's own bookkeeping: NewSessions holds only the sessions THIS call
// minted (the caller registers them with the sidecar); Placeholders and
// Index are the env surface to export.
type preMintResult struct {
	// Placeholders maps ONECLI_PG_<LABEL>_URL → proxy URL.
	Placeholders map[string]string
	// Index lists every governed database (scan-covered and pre-minted)
	// with the env var that reaches it.
	Index []pgIndexEntry
	// NewSessions are the sessions minted by this call, in mint order.
	NewSessions []*api.PgSession
	// Warnings are user-facing degradation notices (mint failures, cap).
	Warnings []string
}

// preMintPlaceholders mints sessions for granted connections that the env
// scan did NOT already cover and returns the placeholder env entries plus
// the full index (which also lists scan-covered connections, mapped to
// their swapped var). minted is read for reuse and updated with every new
// session; newly minted sessions are also reported in the result so the
// caller can register exactly those with the sidecar.
//
// Best-effort by design, even under --pg-proxy=required: a pre-mint
// failure means a database the agent's env never referenced lacks a
// placeholder — governance of the databases the agent was actually
// launched to use is unaffected, so aborting the run would be wrong.
func preMintPlaceholders(
	conns []api.PgConnection,
	minted map[string]*api.PgSession,
	swappedVarByConnID map[string]string,
	mint func(connectionID string) (*api.PgSession, error),
	gatewayHost, caPath string,
) preMintResult {
	res := preMintResult{Placeholders: map[string]string{}}
	taken := map[string]bool{}

	for _, conn := range conns {
		hostPort := normalizeHostPort(conn.Host, fmt.Sprintf("%d", conn.Port))
		label := conn.ID
		if conn.Label != nil && *conn.Label != "" {
			label = *conn.Label
		}

		// Scan-covered connection: the index points at the swapped var.
		if varName, ok := swappedVarByConnID[conn.ID]; ok {
			res.Index = append(res.Index, pgIndexEntry{Label: label, Host: hostPort, EnvVar: varName})
			continue
		}

		if len(res.NewSessions) >= pgPreMintMax {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"placeholder cap reached (%d); %s has no ONECLI_PG_* variable — its URL in the env is still governed", pgPreMintMax, hostPort))
			continue
		}

		session, ok := minted[conn.ID]
		if !ok {
			s, err := mint(conn.ID)
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf(
					"could not open a proxy session for %s (%v); no placeholder exported", hostPort, err))
				continue
			}
			session = s
			minted[conn.ID] = session
			res.NewSessions = append(res.NewSessions, session)
		}

		varName := placeholderVarName(conn, taken)
		res.Placeholders[varName] = placeholderPgURL(session, gatewayHost, caPath)
		res.Index = append(res.Index, pgIndexEntry{Label: label, Host: hostPort, EnvVar: varName})
	}
	return res
}

// encodePgIndex renders the ONECLI_PG_CONNECTIONS value. Empty index →
// empty string (the variable is then not exported at all).
func encodePgIndex(index []pgIndexEntry) string {
	if len(index) == 0 {
		return ""
	}
	b, err := json.Marshal(index)
	if err != nil {
		return ""
	}
	return string(b)
}
