package main

// The pg session sidecar: a small detached child process that outlives
// `onecli run`'s exec. run.go execs the agent binary in-place (preserving
// PID and TTY semantics), so nothing in the main path can heartbeat pg
// sessions or reap them when the agent exits. The sidecar is forked
// FIRST, watches its parent PID (which becomes the agent after exec), and:
//   - heartbeats each session on an interval while the parent lives,
//   - deletes the sessions when the parent exits,
//   - exits on its own if the gateway reports every session gone.
//
// Failure is safe by construction: if the sidecar dies, heartbeats stop
// and the gateway expires the sessions at TTL.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/onecli/onecli-cli/internal/api"
)

const (
	pgSidecarFlag = "__pg-sidecar"
	// Heartbeat fallback when the session TTL is unknown; normally the
	// interval is derived from the minted TTL (see heartbeatInterval).
	pgHeartbeatDefault = 5 * time.Minute
	pgHeartbeatFloor   = 15 * time.Second
	pgParentPollEvery  = 2 * time.Second
	// Watcher pass interval: frequent enough to catch short-lived direct
	// connections without measurable overhead (one lsof + ps exec).
	pgWatchEvery = 15 * time.Second
)

// heartbeatInterval keeps a session alive with margin: a third of its TTL, so
// two heartbeats can be missed before expiry. Falls back to a fixed interval
// when the TTL is unknown, and never drops below a floor.
func heartbeatInterval(ttlSeconds uint64) time.Duration {
	if ttlSeconds == 0 {
		return pgHeartbeatDefault
	}
	d := time.Duration(ttlSeconds/3) * time.Second
	if d < pgHeartbeatFloor {
		return pgHeartbeatFloor
	}
	return d
}

// spawnPgSidecar forks a detached copy of this binary in sidecar mode.
// Must be called BEFORE syscall.Exec. Best-effort: a spawn failure only
// means sessions expire by TTL instead of promptly (and no watcher runs).
// watchHosts is the encoded registered host→connection map for the
// direct-connection watcher; gatewayPgAddr the pg listener host:port the
// watcher must never report (the governed route itself).
func spawnPgSidecar(gatewayURL, agentToken string, sessionIDs []string, ttlSeconds uint64, watchHosts, gatewayPgAddr string) {
	if len(sessionIDs) == 0 && watchHosts == "" {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, pgSidecarFlag,
		"--parent", fmt.Sprintf("%d", os.Getpid()),
		"--gateway", gatewayURL,
		"--sessions", strings.Join(sessionIDs, ","),
		"--ttl", fmt.Sprintf("%d", ttlSeconds),
	)
	// The token rides the environment, not argv — argv is visible to
	// every process on the machine via ps. The watch config rides env
	// for symmetry (it is not secret, but argv length is bounded).
	cmd.Env = append(os.Environ(),
		"ONECLI_PG_SIDECAR_TOKEN="+agentToken,
		"ONECLI_PG_WATCH_HOSTS="+watchHosts,
		"ONECLI_PG_GATEWAY_ADDR="+gatewayPgAddr,
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	cmd.SysProcAttr = detachedSysProcAttr()
	_ = cmd.Start()
	// Deliberately not Wait()ed: Setsid detaches it from our session, and
	// exec replaces this process image momentarily anyway.
}

// parsePgSidecarArgs parses the hidden-mode argv:
// __pg-sidecar --parent <pid> --gateway <url> --sessions <id,id,...>
// The agent token rides ONECLI_PG_SIDECAR_TOKEN (not argv — ps-visible);
// the watcher's host map rides ONECLI_PG_WATCH_HOSTS + _GATEWAY_ADDR.
func parsePgSidecarArgs(argv []string) (pgSidecarArgs, bool) {
	args := pgSidecarArgs{
		agentToken:    os.Getenv("ONECLI_PG_SIDECAR_TOKEN"),
		watchHosts:    os.Getenv("ONECLI_PG_WATCH_HOSTS"),
		gatewayPgAddr: os.Getenv("ONECLI_PG_GATEWAY_ADDR"),
	}
	for i := 0; i+1 < len(argv); i += 2 {
		switch argv[i] {
		case "--parent":
			if _, err := fmt.Sscanf(argv[i+1], "%d", &args.parentPID); err != nil {
				return args, false
			}
		case "--gateway":
			args.gatewayURL = argv[i+1]
		case "--sessions":
			for _, id := range strings.Split(argv[i+1], ",") {
				if id = strings.TrimSpace(id); id != "" {
					args.sessionIDs = append(args.sessionIDs, id)
				}
			}
		case "--ttl":
			// Optional: absent for older callers → 0 → default interval.
			_, _ = fmt.Sscanf(argv[i+1], "%d", &args.ttlSeconds)
		}
	}
	ok := args.parentPID > 0 && args.gatewayURL != "" && (len(args.sessionIDs) > 0 || args.watchHosts != "") && args.agentToken != ""
	return args, ok
}

// runPgSidecar is the sidecar entry point (invoked via the hidden flag).
type pgSidecarArgs struct {
	parentPID  int
	gatewayURL string
	sessionIDs []string
	agentToken string
	ttlSeconds uint64
	// watchHosts is the encoded registered host→connection-id map for
	// the direct-connection watcher (empty disables watching).
	watchHosts string
	// gatewayPgAddr is the pg listener host:port never reported.
	gatewayPgAddr string
}

func runPgSidecar(args pgSidecarArgs) {
	client := &api.PgGatewayClient{BaseURL: args.gatewayURL, AgentToken: args.agentToken}

	heartbeat := time.NewTicker(heartbeatInterval(args.ttlSeconds))
	defer heartbeat.Stop()
	parentPoll := time.NewTicker(pgParentPollEvery)
	defer parentPoll.Stop()

	// The direct-connection watcher (layer 3): enabled only when the
	// parent passed a registered-host map.
	var watch *pgWatchState
	watchTick := time.NewTicker(pgWatchEvery)
	defer watchTick.Stop()
	if hosts := parsePgWatchHosts(args.watchHosts); len(hosts) > 0 {
		watch = &pgWatchState{
			registeredHosts: hosts,
			gatewayAddrs:    map[string]bool{args.gatewayPgAddr: true},
			lastReport:      map[string]time.Time{},
		}
	}

	for {
		select {
		case <-parentPoll.C:
			if !processAlive(args.parentPID) {
				reapSessions(client, args.sessionIDs)
				return
			}
		case <-watchTick.C:
			if watch != nil {
				watchOnce(client, watch, agentTreePids(args.parentPID))
			}
		case <-heartbeat.C:
			if len(args.sessionIDs) == 0 {
				continue
			}
			alive := false
			for _, id := range args.sessionIDs {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := client.HeartbeatPgSession(ctx, id); err == nil {
					alive = true
				}
				cancel()
			}
			// Every session gone (expired or deleted elsewhere): keep
			// running only for the watcher; exit when it is off too.
			if !alive && watch == nil {
				return
			}
			if !alive {
				args.sessionIDs = nil
			}
		}
	}
}

func reapSessions(client *api.PgGatewayClient, sessionIDs []string) {
	for _, id := range sessionIDs {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = client.DeletePgSession(ctx, id)
		cancel()
	}
}

// processAlive reports whether pid is still running. After exec the PID
// belongs to the agent, which is exactly the process whose lifetime
// gates the sessions.
func processAlive(pid int) bool {
	return processAliveByPID(pid)
}
