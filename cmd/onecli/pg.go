package main

// `onecli pg` — agent-first database routing commands, designed to be run
// BY the agent inside an `onecli run` session (the gateway origin and
// agent token are recovered from the session's own HTTPS_PROXY value).
//
// `onecli pg url <label|host[:port]|connection-id>` prints the governed
// proxy URL for a registered database as JSON. It is the escape hatch for
// databases registered in the dashboard AFTER the agent started (or past
// the placeholder cap): the agent gets a fresh governed route without
// restarting the run. The skill documents it as the fallback when a host
// is missing from ONECLI_PG_CONNECTIONS.

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
)

type PgCmd struct {
	URL PgURLCmd `cmd:"" help:"Print the governed proxy URL for a registered database (run inside an 'onecli run' session)."`
}

type PgURLCmd struct {
	Target string `arg:"" name:"database" help:"Connection label, host[:port], or connection id."`
}

// PgURLResponse is the JSON contract of `onecli pg url`. Deliberately
// protocol-generic (a future `onecli mysql url` mirrors it): the agent
// consumes url/expires_in_seconds and treats everything else as metadata.
type PgURLResponse struct {
	URL              string `json:"url"`
	Label            string `json:"label"`
	Host             string `json:"host"`
	ExpiresInSeconds uint64 `json:"expires_in_seconds"`
}

func (c *PgURLCmd) Run(out *output.Writer) error {
	client, gatewayHost, caPath, err := pgClientFromSession()
	if err != nil {
		return out.Error("no_gateway_session", err.Error())
	}

	resp, err := client.ListPgConnections(context.Background())
	if err != nil {
		return out.Error("gateway_unreachable", fmt.Sprintf("could not list registered databases: %v", err))
	}

	conn, err := resolvePgTarget(c.Target, resp.Connections)
	if err != nil {
		return out.ErrorWithAction("not_registered", err.Error(),
			"Ask the user to connect this database in the OneCLI dashboard, then retry.")
	}

	session, err := client.MintPgSession(context.Background(), conn.ID)
	if err != nil {
		return out.Error("session_mint_failed", fmt.Sprintf("could not open a proxy session: %v", err))
	}

	label := conn.ID
	if conn.Label != nil && *conn.Label != "" {
		label = *conn.Label
	}
	return out.Write(PgURLResponse{
		URL:              placeholderPgURL(session, gatewayHost, caPath),
		Label:            label,
		Host:             normalizeHostPort(conn.Host, fmt.Sprintf("%d", conn.Port)),
		ExpiresInSeconds: session.TTLSeconds,
	})
}

// pgClientFromSession recovers the gateway client from the calling
// session's environment: HTTPS_PROXY carries the origin + agent token
// (exactly what `onecli run` exported for the agent), and the CA bundle
// path is where `onecli run` writes it. Errors are agent-actionable.
func pgClientFromSession() (*api.PgGatewayClient, string, string, error) {
	proxyURL := ""
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if v := os.Getenv(key); v != "" && strings.Contains(v, "aoc_") {
			proxyURL = v
			break
		}
	}
	if proxyURL == "" {
		return nil, "", "", fmt.Errorf("no OneCLI gateway session found in the environment; this command runs inside an 'onecli run' session")
	}
	gwURL, agentToken, ok := gatewayOriginAndToken(proxyURL)
	if !ok {
		return nil, "", "", fmt.Errorf("the proxy URL in the environment carries no usable agent token")
	}
	// The pg listener lives on the same host as the gateway HTTP origin.
	gatewayHost := gwURL
	if u, err := url.Parse(gwURL); err == nil && u.Hostname() != "" {
		gatewayHost = u.Hostname()
	}
	caPath := ""
	if home, err := os.UserHomeDir(); err == nil {
		p := home + "/.onecli/ca-bundle.pem"
		if _, err := os.Stat(p); err == nil {
			caPath = p
		}
	}
	return &api.PgGatewayClient{BaseURL: gwURL, AgentToken: agentToken}, gatewayHost, caPath, nil
}

// resolvePgTarget matches the user-supplied target against the granted
// connections: exact connection id, exact label (case-insensitive), or
// host[:port] (normalized; a bare host matches any port when unambiguous).
func resolvePgTarget(target string, conns []api.PgConnection) (*api.PgConnection, error) {
	t := strings.TrimSpace(target)
	if t == "" {
		return nil, fmt.Errorf("empty database target")
	}

	var matches []*api.PgConnection
	add := func(c *api.PgConnection) {
		for _, m := range matches {
			if m.ID == c.ID {
				return
			}
		}
		matches = append(matches, c)
	}

	tLower := strings.ToLower(t)
	for i := range conns {
		c := &conns[i]
		if c.ID == t {
			return c, nil // an id is exact by construction
		}
		if c.Label != nil && strings.ToLower(*c.Label) == tLower {
			add(c)
		}
		hostPort := normalizeHostPort(c.Host, fmt.Sprintf("%d", c.Port))
		bareHost := strings.ToLower(strings.TrimSuffix(c.Host, "."))
		if hostPort == normalizeTargetHostPort(t) || bareHost == tLower {
			add(c)
		}
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, fmt.Errorf("no registered database matches %q", t)
	default:
		var names []string
		for _, m := range matches {
			label := m.ID
			if m.Label != nil && *m.Label != "" {
				label = *m.Label
			}
			names = append(names, fmt.Sprintf("%s (%s)", label, normalizeHostPort(m.Host, fmt.Sprintf("%d", m.Port))))
		}
		return nil, fmt.Errorf("%q is ambiguous — matches: %s; use the connection id or host:port", t, strings.Join(names, ", "))
	}
}

// normalizeTargetHostPort normalizes a host[:port] target for comparison,
// defaulting the port to 5432 like the scan side does.
func normalizeTargetHostPort(t string) string {
	host, port, err := net.SplitHostPort(t)
	if err != nil {
		host, port = t, "5432"
	}
	return normalizeHostPort(host, port)
}
