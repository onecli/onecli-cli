package main

// The direct-connection watcher: layer 3 of the pg governance model
// (design doc "Guidance loop for direct-connection attempts"). While the
// agent runs, the sidecar periodically enumerates the agent process
// tree's ESTABLISHED TCP connections to common postgres ports. A peer
// that is NOT the gateway pg listener is reported to the gateway:
//   - peer matches a REGISTERED database host → pg_direct_connection
//     (the dashboard shows "agent connected directly, bypassing
//     governance"),
//   - otherwise → pg_unregistered_connection (the "connect it in the
//     dashboard" nudge).
//
// Detection is after-the-fact by nature; the PRIMARY corrective stays
// the skill/hook guidance and the swap. Failure is inert: watcher errors
// never affect the agent, and reporting is debounced per peer.

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/onecli/onecli-cli/internal/api"
)

// pgWatchPorts are the TCP ports treated as postgres-ish. 6543 because
// Supabase's transaction pooler is a common agent-encountered endpoint.
var pgWatchPorts = map[uint16]bool{5432: true, 5433: true, 5439: true, 6432: true, 6543: true}

// pgWatchDebounce suppresses repeat reports for the same peer.
const pgWatchDebounce = 10 * time.Minute

// pgWatchState carries the watcher's config + debounce memory.
type pgWatchState struct {
	// registeredHosts maps host:port → connection id (normalized like
	// matchPgScans keys). Passed from the parent via env at spawn.
	registeredHosts map[string]string
	// gatewayAddrs are listener addresses never reported (the proxy
	// itself): the pg listener host:port forms.
	gatewayAddrs map[string]bool
	lastReport   map[string]time.Time
}

// pgObservedConn is one ESTABLISHED TCP connection of the agent tree.
type pgObservedConn struct {
	PeerHost string
	PeerPort uint16
}

// classifyObserved decides what (if anything) to report for one observed
// connection. Pure: takes the clock for debounce testing.
func (w *pgWatchState) classifyObserved(conn pgObservedConn, now time.Time) (kind string, connectionID string, report bool) {
	if !pgWatchPorts[conn.PeerPort] {
		return "", "", false
	}
	key := normalizeHostPort(conn.PeerHost, fmt.Sprintf("%d", conn.PeerPort))
	if w.gatewayAddrs[key] {
		return "", "", false
	}
	if last, seen := w.lastReport[key]; seen && now.Sub(last) < pgWatchDebounce {
		return "", "", false
	}
	w.lastReport[key] = now
	if id, ok := w.registeredHosts[key]; ok {
		return "pg_direct_connection", id, true
	}
	return "pg_unregistered_connection", "", true
}

// watchOnce runs one detection pass: enumerate + classify + report. Both
// the registered-host map AND the gateway's own listener address are
// DNS-expanded per pass (lsof reports IP peers, config uses hostnames) —
// without expanding gatewayAddrs, the agent's own governed connections to
// a REMOTE gateway (hostname config, IP peer, port 6432 in the watch
// list) would be false-positive reported. The state's own maps stay
// hostname-keyed.
func watchOnce(client *api.PgGatewayClient, w *pgWatchState, pids []int) {
	lookup := func(host string) ([]string, error) { return net.LookupHost(host) }
	gatewayExpanded := map[string]bool{}
	gatewayAsMap := map[string]string{}
	for addr := range w.gatewayAddrs {
		gatewayAsMap[addr] = ""
	}
	for addr := range expandWatchHostsDNS(gatewayAsMap, lookup) {
		gatewayExpanded[addr] = true
	}
	pass := &pgWatchState{
		registeredHosts: expandWatchHostsDNS(w.registeredHosts, lookup),
		gatewayAddrs:    gatewayExpanded,
		lastReport:      w.lastReport, // shared: debounce survives passes
	}
	for _, conn := range observedPgConns(pids) {
		kind, connID, report := pass.classifyObserved(conn, time.Now())
		if !report {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = client.ReportPgWatchEvent(ctx, kind, conn.PeerHost, conn.PeerPort, connID)
		cancel()
	}
}

// observedPgConns enumerates ESTABLISHED TCP peers of the given pids.
// macOS: lsof (in the default PATH on every macOS). Linux: procfs would
// be cheaper, but lsof is present on the distros agents actually run and
// keeps one code path; a procfs fast path can come later.
func observedPgConns(pids []int) []pgObservedConn {
	if len(pids) == 0 {
		return nil
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return nil
	}
	pidList := make([]string, 0, len(pids))
	for _, p := range pids {
		pidList = append(pidList, strconv.Itoa(p))
	}
	// -a: AND the filters; -p: pids; -iTCP -sTCP:ESTABLISHED: established
	// TCP only; -n/-P: numeric hosts/ports (no DNS stalls); -F n: parse-
	// friendly output (peer address lines start with 'n').
	out, err := exec.Command("lsof", "-a", "-p", strings.Join(pidList, ","),
		"-iTCP", "-sTCP:ESTABLISHED", "-n", "-P", "-F", "n").Output()
	if err != nil {
		// lsof exits 1 when nothing matches — that is the common case.
		return nil
	}
	return parseLsofConns(string(out))
}

// parseLsofConns extracts peer host:port pairs from `lsof -F n` output.
// Connection lines look like "nlocal->peer:port"; only the peer side
// matters here.
func parseLsofConns(out string) []pgObservedConn {
	var conns []pgObservedConn
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "n") || !strings.Contains(line, "->") {
			continue
		}
		peer := line[strings.Index(line, "->")+2:]
		host, portStr, err := net.SplitHostPort(strings.TrimSpace(peer))
		if err != nil {
			continue
		}
		port, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			continue
		}
		key := host + ":" + portStr
		if seen[key] {
			continue
		}
		seen[key] = true
		conns = append(conns, pgObservedConn{PeerHost: host, PeerPort: uint16(port)})
	}
	return conns
}

// agentTreePids returns the parent pid plus its descendants (best-effort:
// one `ps` pass, direct + transitive children).
func agentTreePids(parent int) []int {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=").Output()
	if err != nil {
		return []int{parent}
	}
	children := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		children[ppid] = append(children[ppid], pid)
	}
	pids := []int{parent}
	for i := 0; i < len(pids); i++ {
		pids = append(pids, children[pids[i]]...)
	}
	return pids
}

// parsePgWatchHosts decodes the ONECLI_PG_WATCH_HOSTS env value:
// comma-separated host:port=connection-id entries.
func parsePgWatchHosts(raw string) map[string]string {
	out := map[string]string{}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		i := strings.LastIndexByte(entry, '=')
		if i <= 0 || i == len(entry)-1 {
			continue
		}
		out[entry[:i]] = entry[i+1:]
	}
	return out
}

// expandWatchHostsDNS adds IP-keyed entries for every hostname-keyed
// entry: lsof reports peers as IP literals (-n, no DNS stalls), while
// databases are registered by hostname — without the expansion a direct
// connection would never match. Resolution happens in the sidecar, off
// the agent's critical path, and is refreshed per call so DNS changes
// are picked up across watch passes. Lookup failures leave the original
// entry: an unresolvable host cannot be dialed directly either.
func expandWatchHostsDNS(hosts map[string]string, lookup func(host string) ([]string, error)) map[string]string {
	out := make(map[string]string, len(hosts)*2)
	for key, id := range hosts {
		out[key] = id
		host, port, err := net.SplitHostPort(key)
		if err != nil || net.ParseIP(host) != nil {
			continue // already an IP literal (or malformed)
		}
		addrs, err := lookup(host)
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			out[normalizeHostPort(addr, port)] = id
		}
	}
	return out
}

// encodePgWatchHosts is the inverse of parsePgWatchHosts, built from the
// registered connection list at spawn.
func encodePgWatchHosts(conns []api.PgConnection) string {
	var entries []string
	for _, c := range conns {
		entries = append(entries, normalizeHostPort(c.Host, fmt.Sprintf("%d", c.Port))+"="+c.ID)
	}
	return strings.Join(entries, ",")
}
