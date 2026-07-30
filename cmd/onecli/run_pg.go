package main

// Postgres URL interception for `onecli run` — phase 1 of
// docs/design/pg-interception.md (cloud repo): scan the environment and
// project .env* files for postgres:// URLs, match them BY HOST against the
// dashboard-registered connections available to this agent, mint a proxy
// session per matched connection, and swap the matched env values for
// proxy URLs (aoc_pg_<token> as the username, dummy password). Unmatched
// database URLs are left untouched and produce a warning naming the fix.
//
// The files are never modified: swapped values are injected into the
// CHILD env under the same variable names, which shadows .env for
// default-precedence loaders (dotenv, python-dotenv, Prisma).

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
)

// pgURLPrefixes match postgres connection strings in env values.
var pgURLPrefixes = []string{"postgres://", "postgresql://"}

// dotenvFiles are the project files scanned (read-only) for database URLs.
var dotenvFiles = []string{".env", ".env.local", ".env.development", ".env.development.local"}

// pgScanResult is one discovered postgres URL and where it came from.
type pgScanResult struct {
	// VarName is the environment variable carrying the URL. For a libpq
	// group scan this is "PGHOST" (the group's anchor).
	VarName string
	// URL is the parsed original connection URL. For a libpq group scan
	// it is a synthetic URL assembled from the PG* vars (host/port/db).
	URL *url.URL
	// FromEnvFile is true when the value came from a .env file rather
	// than the process environment (env wins on conflicts).
	FromEnvFile bool
	// Libpq is true when this scan represents the libpq PG* variable group
	// (PGHOST/PGPORT/...) rather than a single URL-valued variable; the
	// rewrite then emits the whole group, not one URL.
	Libpq bool
}

// libpqVars are the connection vars of the libpq environment group. PGHOST
// anchors the group; the rest refine it.
var libpqVars = []string{"PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE"}

// pgSwapOutcome reports what setupPgProxy did, for messaging + tests.
type pgSwapOutcome struct {
	// Swapped maps env var name → proxy URL.
	Swapped map[string]string
	// Placeholders maps ONECLI_PG_<LABEL>_URL → proxy URL for granted
	// connections the env scan did not cover (pre-minted).
	Placeholders map[string]string
	// IndexJSON is the ONECLI_PG_CONNECTIONS value: every governed
	// database with the env var that reaches it. Empty when none.
	IndexJSON string
	// SessionIDs to reap on exit (one per matched connection).
	SessionIDs []string
	// TTLSeconds is the shortest session TTL minted, so the sidecar can
	// heartbeat fast enough to keep every session alive (0 = none minted).
	TTLSeconds uint64
	// UnmatchedHosts are postgres hosts found in the env/.env that have
	// no registered connection — surfaced, never silently proxied.
	UnmatchedHosts []string
	// WatchHosts is the encoded registered host→connection-id map for the
	// sidecar's direct-connection watcher.
	WatchHosts string
	// GatewayPgAddr is the pg listener host:port the watcher must never
	// report (it IS the governed route).
	GatewayPgAddr string
}

// setupPgProxy discovers postgres URLs (and the libpq PG* group), matches them
// against registered connections, mints sessions, and returns the env entries
// to append (shadowing the originals). Granted connections the scan did NOT
// cover are pre-minted and exported as ONECLI_PG_<LABEL>_URL placeholders,
// with the full map advertised in ONECLI_PG_CONNECTIONS — so a governed
// route exists for every granted database, not just those already in the
// env. Returns (nil, nil) when there is nothing to do.
//
// Fail-open by default: a gateway error degrades to a warning and the URLs are
// left untouched, matching the CA-write degradation in the main run path. When
// `required` is set (`--pg-proxy=required`), a gateway failure that would leave
// a MATCHED database ungoverned instead returns an error so the caller aborts;
// degrade-open silently forfeits the governance the user asked for. Unregistered
// databases stay warnings in both modes (they are out of scope by design), and
// pre-mint failures also stay warnings: a database the agent's env never
// referenced losing its placeholder does not forfeit asked-for governance.
func setupPgProxy(out *output.Writer, client *api.PgGatewayClient, gatewayHost, caPath string, environ []string, cwd string, required bool) (*pgSwapOutcome, error) {
	scans := scanPgURLs(environ, cwd)

	resp, err := client.ListPgConnections(context.Background())
	if err != nil {
		if len(scans) == 0 {
			// Nothing scanned and nothing listable: quietly do nothing.
			return nil, nil
		}
		if required {
			return nil, fmt.Errorf("postgres governance is required (--pg-proxy=required) but the gateway could not list registered databases: %w", err)
		}
		out.Stderr(fmt.Sprintf("onecli: warning: could not list registered databases (%v); postgres URLs left untouched", err))
		return nil, nil
	}
	if len(resp.Connections) == 0 {
		hosts := uniqueHosts(scans)
		if len(hosts) > 0 {
			out.Stderr(fmt.Sprintf("onecli: unregistered database detected (%s) — connect it in the dashboard to govern agent access", strings.Join(hosts, ", ")))
		}
		return nil, nil
	}

	// Index registered connections by host:port and pair each scan.
	outcome := &pgSwapOutcome{Swapped: map[string]string{}}
	// One session per matched connection id, shared across vars that
	// point at the same upstream (DATABASE_URL + DIRECT_URL collapse).
	sessions := map[string]*api.PgSession{}
	seenUnmatched := map[string]bool{}
	// Which env var reaches each scan-covered connection, for the index.
	swappedVarByConnID := map[string]string{}

	for _, m := range matchPgScans(scans, resp.Connections) {
		if m.Conn == nil {
			if !seenUnmatched[m.HostPort] {
				seenUnmatched[m.HostPort] = true
				outcome.UnmatchedHosts = append(outcome.UnmatchedHosts, m.HostPort)
			}
			continue
		}

		session, ok := sessions[m.Conn.ID]
		if !ok {
			minted, err := client.MintPgSession(context.Background(), m.Conn.ID)
			if err != nil {
				if required {
					return nil, fmt.Errorf("postgres governance is required (--pg-proxy=required) but a proxy session for %s could not be opened: %w", m.HostPort, err)
				}
				out.Stderr(fmt.Sprintf("onecli: warning: could not open a proxy session for %s (%v); leaving %s untouched", m.HostPort, err, m.Scan.VarName))
				continue
			}
			session = minted
			sessions[m.Conn.ID] = session
			outcome.SessionIDs = append(outcome.SessionIDs, session.SessionID)
			if outcome.TTLSeconds == 0 || session.TTLSeconds < outcome.TTLSeconds {
				outcome.TTLSeconds = session.TTLSeconds
			}
		}

		if m.Scan.Libpq {
			// The libpq group rewrites to several PG* vars, not one URL.
			for k, v := range buildLibpqEnv(session.User, gatewayHost, caPath, session.ListenPort) {
				outcome.Swapped[k] = v
			}
			swappedVarByConnID[m.Conn.ID] = "PGHOST"
		} else {
			outcome.Swapped[m.Scan.VarName] = buildPgProxyURL(m.Scan.URL, session.User, gatewayHost, caPath, session.ListenPort)
			if _, seen := swappedVarByConnID[m.Conn.ID]; !seen {
				swappedVarByConnID[m.Conn.ID] = m.Scan.VarName
			}
		}
	}

	// Pre-mint placeholders for granted connections the scan left
	// uncovered, so the agent has a governed route to EVERY database it
	// may reach — not just those already named in the env.
	pre := preMintPlaceholders(
		resp.Connections, sessions, swappedVarByConnID,
		func(connectionID string) (*api.PgSession, error) {
			return client.MintPgSession(context.Background(), connectionID)
		},
		gatewayHost, caPath,
	)
	for _, w := range pre.Warnings {
		out.Stderr("onecli: warning: " + w)
	}
	outcome.Placeholders = pre.Placeholders
	outcome.IndexJSON = encodePgIndex(pre.Index)
	outcome.WatchHosts = encodePgWatchHosts(resp.Connections)
	outcome.GatewayPgAddr = normalizeHostPort(gatewayHost, fmt.Sprintf("%d", resp.ListenPort))
	// Pre-minted sessions ride the same sidecar heartbeat/reaping as the
	// scan-covered ones.
	for _, s := range pre.NewSessions {
		outcome.SessionIDs = append(outcome.SessionIDs, s.SessionID)
		if outcome.TTLSeconds == 0 || s.TTLSeconds < outcome.TTLSeconds {
			outcome.TTLSeconds = s.TTLSeconds
		}
	}

	reportPgOutcome(out, outcome)
	if len(outcome.Swapped) == 0 && len(outcome.Placeholders) == 0 && len(outcome.SessionIDs) == 0 {
		return nil, nil
	}
	return outcome, nil
}

// reportPgOutcome prints the user-facing summary of what governance was
// established: swapped vars, exported placeholders, unregistered hosts.
func reportPgOutcome(out *output.Writer, outcome *pgSwapOutcome) {
	for _, hostPort := range outcome.UnmatchedHosts {
		out.Stderr(fmt.Sprintf("onecli: unregistered database detected (%s) — connect it in the dashboard to govern agent access", hostPort))
	}
	if len(outcome.Swapped) > 0 {
		names := make([]string, 0, len(outcome.Swapped))
		for name := range outcome.Swapped {
			names = append(names, name)
		}
		out.Stderr(fmt.Sprintf("onecli: postgres governed via gateway proxy: %s", strings.Join(names, ", ")))
	}
	if len(outcome.Placeholders) > 0 {
		names := make([]string, 0, len(outcome.Placeholders))
		for name := range outcome.Placeholders {
			names = append(names, name)
		}
		out.Stderr(fmt.Sprintf("onecli: postgres available via gateway proxy: %s", strings.Join(names, ", ")))
	}
}

// pgMatch pairs one scanned URL with its registered connection (nil when
// unmatched).
type pgMatch struct {
	Scan     pgScanResult
	HostPort string
	Conn     *api.PgConnection
}

// matchPgScans indexes registered connections by host:port and pairs each
// scan with its match. Ports default to 5432 on the scan side.
func matchPgScans(scans []pgScanResult, conns []api.PgConnection) []pgMatch {
	byHostPort := map[string]api.PgConnection{}
	for _, conn := range conns {
		byHostPort[normalizeHostPort(conn.Host, fmt.Sprintf("%d", conn.Port))] = conn
	}
	out := make([]pgMatch, 0, len(scans))
	for _, scan := range scans {
		port := scan.URL.Port()
		if port == "" {
			port = "5432"
		}
		key := normalizeHostPort(scan.URL.Hostname(), port)
		m := pgMatch{Scan: scan, HostPort: key}
		if conn, ok := byHostPort[key]; ok {
			connCopy := conn
			m.Conn = &connCopy
		}
		out = append(out, m)
	}
	return out
}

// normalizeHostPort builds a comparable host:port key: the host is lowercased
// and a single trailing dot (the FQDN root) stripped, so DNS-equivalent forms
// (DB.Example.COM, db.example.com.) match the registered host. net.JoinHostPort
// brackets IPv6 consistently on both the scan and registered sides.
func normalizeHostPort(host, port string) string {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	return net.JoinHostPort(h, port)
}

// previewPgSwap is the read-only dry-run variant of setupPgProxy: it
// scans and matches but mints nothing. Returns var → registered
// connection host that would be proxied (including the placeholder vars
// that pre-mint would export), and the unmatched hosts.
func previewPgSwap(client *api.PgGatewayClient, environ []string, cwd string) (map[string]string, []string) {
	scans := scanPgURLs(environ, cwd)
	resp, err := client.ListPgConnections(context.Background())
	if err != nil {
		return nil, uniqueHosts(scans)
	}
	swap := map[string]string{}
	seenUnmatched := map[string]bool{}
	var unmatched []string
	covered := map[string]bool{}
	for _, m := range matchPgScans(scans, resp.Connections) {
		if m.Conn != nil {
			swap[m.Scan.VarName] = m.HostPort
			covered[m.Conn.ID] = true
		} else if !seenUnmatched[m.HostPort] {
			seenUnmatched[m.HostPort] = true
			unmatched = append(unmatched, m.HostPort)
		}
	}
	// Placeholder vars pre-mint WOULD export (no sessions in dry-run).
	taken := map[string]bool{}
	preview := 0
	for _, conn := range resp.Connections {
		if covered[conn.ID] || preview >= pgPreMintMax {
			continue
		}
		preview++
		swap[placeholderVarName(conn, taken)] = normalizeHostPort(conn.Host, fmt.Sprintf("%d", conn.Port))
	}
	return swap, unmatched
}

// scanPgURLs finds postgres URLs in the process env and project .env
// files. Process env wins for a variable defined in both (matching
// loader precedence — the swap then shadows the file either way).
func scanPgURLs(environ []string, cwd string) []pgScanResult {
	found := map[string]pgScanResult{}

	// .env files first so real env overwrites on collision.
	for _, name := range dotenvFiles {
		entries := parseDotenvFile(filepath.Join(cwd, name))
		for key, val := range entries {
			if u := parsePgURL(val); u != nil {
				found[key] = pgScanResult{VarName: key, URL: u, FromEnvFile: true}
			}
		}
	}
	for _, kv := range environ {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		key, val := kv[:i], kv[i+1:]
		if u := parsePgURL(val); u != nil {
			found[key] = pgScanResult{VarName: key, URL: u}
		}
	}

	out := make([]pgScanResult, 0, len(found)+1)
	for _, r := range found {
		out = append(out, r)
	}
	// The libpq PG* var group is a second, non-URL way agents name a
	// database (psql, many drivers). Collect it with the same .env-then-env
	// precedence and, when PGHOST names a TCP host, add one synthetic scan.
	if scan := scanLibpqGroup(environ, cwd); scan != nil {
		out = append(out, *scan)
	}
	return out
}

// scanLibpqGroup collects the libpq PG* vars from the project .env files and
// the process env (env wins per var) and assembles them into one synthetic
// scan. Returns nil unless PGHOST names a TCP host.
func scanLibpqGroup(environ []string, cwd string) *pgScanResult {
	vars := map[string]string{}
	for _, name := range dotenvFiles {
		for key, val := range parseDotenvFile(filepath.Join(cwd, name)) {
			if isLibpqVar(key) {
				vars[key] = val
			}
		}
	}
	for _, kv := range environ {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		if key := kv[:i]; isLibpqVar(key) {
			vars[key] = kv[i+1:]
		}
	}
	return libpqScan(vars)
}

func isLibpqVar(key string) bool {
	for _, v := range libpqVars {
		if key == v {
			return true
		}
	}
	return false
}

// libpqScan assembles the libpq PG* vars into one synthetic scan when PGHOST
// names a TCP host. Path-style PGHOST (a Unix-socket directory, e.g.
// /var/run/postgresql) is deliberately skipped: out of scope, like the
// host-less socket URLs parsePgURL rejects. The synthetic URL only carries
// host/port (and db) so matchPgScans can pair it by host:port; credentials are
// never used (the gateway injects vault creds). Returns nil without a TCP host.
func libpqScan(vars map[string]string) *pgScanResult {
	host := strings.TrimSpace(vars["PGHOST"])
	if host == "" || strings.HasPrefix(host, "/") {
		return nil
	}
	port := strings.TrimSpace(vars["PGPORT"])
	if port == "" {
		port = "5432"
	}
	u := &url.URL{Scheme: "postgresql", Host: net.JoinHostPort(host, port)}
	if db := strings.TrimSpace(vars["PGDATABASE"]); db != "" {
		u.Path = "/" + db
	}
	return &pgScanResult{VarName: "PGHOST", URL: u, Libpq: true}
}

// parsePgURL returns the parsed URL when the value is a postgres
// connection URL with a TCP host; nil otherwise. Unix-socket URLs
// (host-less) are recognized and deliberately skipped.
func parsePgURL(value string) *url.URL {
	v := strings.TrimSpace(value)
	// Strip optional surrounding quotes (common in .env files).
	v = strings.Trim(v, `"'`)
	matched := false
	for _, p := range pgURLPrefixes {
		if strings.HasPrefix(v, p) {
			matched = true
			break
		}
	}
	if !matched {
		return nil
	}
	u, err := url.Parse(v)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	return u
}

// parseDotenvFile reads KEY=VALUE pairs from a dotenv file. Deliberately
// MINIMAL — enough to find database URLs, not a general dotenv parser:
// no multiline values, no escape sequences, no variable interpolation.
// Ignores comments, blank lines, and `export ` prefixes. Returns an
// empty map when the file does not exist or cannot be read.
func parseDotenvFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	out := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		// Trim trailing inline comment only for unquoted values.
		if !strings.HasPrefix(val, `"`) && !strings.HasPrefix(val, `'`) {
			if j := strings.Index(val, " #"); j >= 0 {
				val = strings.TrimSpace(val[:j])
			}
		}
		out[key] = val
	}
	return out
}

// buildPgProxyURL rewrites an original postgres URL to point at the
// gateway pg listener, preserving the path (database) and every query
// parameter EXCEPT the overridden connection fields. TLS mode is
// host-aware:
//   - loopback gateway ⇒ sslmode=disable (plaintext localhost leg);
//   - remote gateway with the installed CA ⇒ sslmode=verify-full +
//     sslrootcert=<gateway CA>, so the client-leg TLS is authenticated
//     against the gateway's own CA (defeats a MITM on that leg, not just
//     passive sniffing);
//   - remote gateway without a CA path ⇒ sslmode=require (encrypted,
//     unverified) as a safe fallback.
func buildPgProxyURL(original *url.URL, user, gatewayHost, caPath string, listenPort uint16) string {
	q := original.Query()
	q.Del("sslmode")
	q.Del("sslrootcert")
	q.Del("sslcert")
	q.Del("sslkey")
	if isLoopbackHost(gatewayHost) {
		q.Set("sslmode", "disable")
	} else if caPath != "" {
		q.Set("sslmode", "verify-full")
		q.Set("sslrootcert", caPath)
	} else {
		q.Set("sslmode", "require")
	}

	proxy := url.URL{
		Scheme:   "postgresql",
		User:     url.UserPassword(user, "x"),
		Host:     net.JoinHostPort(gatewayHost, fmt.Sprintf("%d", listenPort)),
		Path:     original.Path,
		RawQuery: q.Encode(),
	}
	return proxy.String()
}

// buildLibpqEnv rewrites the libpq PG* connection vars to point at the gateway
// pg listener, mirroring buildPgProxyURL's host-aware TLS selection. PGDATABASE
// is intentionally NOT emitted — the client's chosen database passes through to
// the real upstream. PGPASSWORD is the dummy "x"; the real credential is never
// carried (the gateway injects vault creds upstream).
func buildLibpqEnv(user, gatewayHost, caPath string, listenPort uint16) map[string]string {
	env := map[string]string{
		"PGHOST":     gatewayHost,
		"PGPORT":     fmt.Sprintf("%d", listenPort),
		"PGUSER":     user,
		"PGPASSWORD": "x",
	}
	if isLoopbackHost(gatewayHost) {
		env["PGSSLMODE"] = "disable"
	} else if caPath != "" {
		env["PGSSLMODE"] = "verify-full"
		env["PGSSLROOTCERT"] = caPath
	} else {
		env["PGSSLMODE"] = "require"
	}
	return env
}

func uniqueHosts(scans []pgScanResult) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range scans {
		port := s.URL.Port()
		if port == "" {
			port = "5432"
		}
		hp := normalizeHostPort(s.URL.Hostname(), port)
		if !seen[hp] {
			seen[hp] = true
			out = append(out, hp)
		}
	}
	return out
}
