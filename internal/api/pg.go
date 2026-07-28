package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PgGatewayClient talks to the GATEWAY's pg-session surface (not the API
// server): the same host:port that serves the HTTP proxy also mounts
// /v1/pg/*. Auth is the agent's aoc_ token.
type PgGatewayClient struct {
	// BaseURL is the gateway origin, e.g. "http://127.0.0.1:10254".
	BaseURL string
	// AgentToken is the aoc_ bearer extracted from the proxy URL.
	AgentToken string
	HTTP       *http.Client
}

// PgConnection is one dashboard-registered postgres connection available
// to the agent (host/port/label only — never credentials).
type PgConnection struct {
	ID       string  `json:"id"`
	Label    *string `json:"label"`
	Host     string  `json:"host"`
	Port     uint16  `json:"port"`
	Database *string `json:"database"`
}

// PgConnectionsResponse is GET /v1/pg/connections.
type PgConnectionsResponse struct {
	Connections []PgConnection `json:"connections"`
	ListenPort  uint16         `json:"listen_port"`
}

// PgSession is the mint response from POST /v1/pg/sessions.
type PgSession struct {
	SessionID  string `json:"session_id"`
	Token      string `json:"token"`
	User       string `json:"user"`
	ListenPort uint16 `json:"listen_port"`
	TTLSeconds uint64 `json:"ttl_seconds"`
	// Database is the dashboard-pinned database, when the connection sets
	// one — used for placeholder URLs where no client URL supplies a path.
	Database *string `json:"database"`
}

func (c *PgGatewayClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (c *PgGatewayClient) do(ctx context.Context, method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.AgentToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("calling gateway: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("gateway %s %s: HTTP %d: %s", method, path, resp.StatusCode, string(data))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

// ListPgConnections returns the registered postgres connections for the
// authenticated agent, plus the gateway's pg listener port.
func (c *PgGatewayClient) ListPgConnections(ctx context.Context) (*PgConnectionsResponse, error) {
	var out PgConnectionsResponse
	if err := c.do(ctx, http.MethodGet, "/v1/pg/connections", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MintPgSession opens a proxy session for one registered connection.
func (c *PgGatewayClient) MintPgSession(ctx context.Context, connectionID string) (*PgSession, error) {
	var out PgSession
	body := map[string]string{"connection_id": connectionID}
	if err := c.do(ctx, http.MethodPost, "/v1/pg/sessions", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// HeartbeatPgSession refreshes a session's TTL.
func (c *PgGatewayClient) HeartbeatPgSession(ctx context.Context, sessionID string) error {
	return c.do(ctx, http.MethodPost, "/v1/pg/sessions/"+sessionID+"/heartbeat", nil, nil)
}

// DeletePgSession ends a session.
func (c *PgGatewayClient) DeletePgSession(ctx context.Context, sessionID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/pg/sessions/"+sessionID, nil, nil)
}

// ReportPgWatchEvent reports an observed direct/unregistered database
// connection to the gateway (watcher sidecar detection, best-effort).
func (c *PgGatewayClient) ReportPgWatchEvent(ctx context.Context, kind, peerHost string, peerPort uint16, connectionID string) error {
	body := map[string]any{
		"kind":      kind,
		"peer_host": peerHost,
		"peer_port": peerPort,
	}
	if connectionID != "" {
		body["connection_id"] = connectionID
	}
	return c.do(ctx, http.MethodPost, "/v1/pg/events", body, nil)
}
