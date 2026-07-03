package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Client is the HTTP client for the OneCLI API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	prefix     string    // resolved API prefix: "/v1" or "/api"
	prefixOnce sync.Once // ensures prefix detection runs once

	// Project slug/id → resolved project id, cached per process. /v1 servers
	// scope requests via the X-Project-Id header (ids, not slugs); the legacy
	// ?projectId= query is kept alongside for old /api servers.
	projectIDs   map[string]string
	projectIDsMu sync.Mutex

	connBase     string    // resolved project-connections base path
	connEnvelope bool      // legacy base wraps list responses in {connections}
	connOnce     sync.Once // ensures the connections probe runs once
}

// New creates an API client.
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: buildHTTPClient(),
	}
}

// newWithPrefix creates a client with a pre-set prefix (for testing).
func newWithPrefix(baseURL, apiKey, prefix string) *Client {
	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: buildHTTPClient(),
		prefix:     prefix,
	}
}

func buildHTTPClient() *http.Client {
	client := &http.Client{Timeout: 30 * time.Second}
	f := os.Getenv("SSL_CERT_FILE")
	if f == "" {
		return client
	}
	data, err := os.ReadFile(f)
	if err != nil {
		return client
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(data)
	client.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}
	return client
}

// withProjectQuery appends a projectId query param to path when projectID is non-empty.
func withProjectQuery(basePath, projectID string) string {
	if projectID == "" {
		return basePath
	}
	u, err := url.Parse(basePath)
	if err != nil {
		return basePath
	}
	q := u.Query()
	q.Set("projectId", projectID)
	u.RawQuery = q.Encode()
	return u.String()
}

// APIError represents an error response from the API.
type APIError struct {
	StatusCode int
	Message    string
	Type       string // server error type (e.g. "authentication_error"), when present
}

func (e *APIError) Error() string {
	return e.Message
}

// parseAPIError extracts the server's message from either error shape: the
// envelope {"error":{"message","type"}} (auth failures, ServiceErrors, 5xx)
// or the flat {"error":"..."} used by route-level validation, falling back
// to the HTTP status text.
func parseAPIError(status int, body []byte) *APIError {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error.Message != "" {
		return &APIError{
			StatusCode: status,
			Message:    envelope.Error.Message,
			Type:       envelope.Error.Type,
		}
	}
	var flat struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &flat) == nil && flat.Error != "" {
		return &APIError{StatusCode: status, Message: flat.Error}
	}
	return &APIError{StatusCode: status, Message: http.StatusText(status)}
}

// networkError translates raw Go network errors into user-friendly messages.
func (c *Client) networkError(err error) error {
	host := c.baseURL
	if u, e := url.Parse(c.baseURL); e == nil && u.Host != "" {
		host = u.Host
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("could not connect to gateway at %s — is the OneCLI gateway running?", host)
	}
	if os.IsTimeout(err) {
		return fmt.Errorf("request to gateway at %s timed out", host)
	}
	return fmt.Errorf("could not reach gateway at %s: %w", host, err)
}

// resolvePrefix probes the server once to determine whether it supports
// /v1 (new) or /api (legacy). All subsequent requests use the resolved prefix.
func (c *Client) resolvePrefix(ctx context.Context) {
	if c.prefix != "" {
		return
	}
	c.prefixOnce.Do(func() {
		c.prefix = "/v1"
		req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/v1/health", nil)
		if err != nil {
			return
		}
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		resp, err := c.httpClient.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		if err != nil || resp.StatusCode == http.StatusNotFound {
			c.prefix = "/api"
		}
	})
}

// applyPrefix replaces the /v1 prefix in path with the resolved prefix.
func (c *Client) applyPrefix(path string) string {
	if c.prefix == "/api" && strings.HasPrefix(path, "/v1/") {
		return "/api" + path[3:]
	}
	return path
}

// do executes an HTTP request and decodes the JSON response.
// For 204 responses, result should be nil.
func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	return c.doProject(ctx, method, path, "", body, result)
}

// resolveProjectID maps a project slug or id to the project id via
// GET /v1/projects, cached per value for the process. Unresolvable values
// (legacy servers without /v1/projects, unknown slugs, transient failures)
// pass through as-is so the server rejects a bad selection loudly instead of
// it being dropped. The lookup runs outside the cache lock — a duplicate
// concurrent fetch is harmless.
func (c *Client) resolveProjectID(ctx context.Context, project string) string {
	c.projectIDsMu.Lock()
	if c.projectIDs == nil {
		c.projectIDs = map[string]string{}
	}
	if id, ok := c.projectIDs[project]; ok {
		c.projectIDsMu.Unlock()
		return id
	}
	c.projectIDsMu.Unlock()

	id := project
	var projects []Project
	if err := c.do(ctx, http.MethodGet, "/v1/projects", nil, &projects); err == nil {
		for _, p := range projects {
			if p.ID == project || p.Slug == project {
				id = p.ID
				break
			}
		}
	}

	c.projectIDsMu.Lock()
	c.projectIDs[project] = id
	c.projectIDsMu.Unlock()
	return id
}

// resolveConnectionsBase probes once for the top-level /v1/connections
// resource. Older servers (pre connections promotion) 404 it and keep the
// legacy /v1/apps/connections paths, whose list responses are wrapped in a
// {connections} envelope. Mirrors the /v1-vs-/api prefix probe.
func (c *Client) resolveConnectionsBase(ctx context.Context) (base string, envelope bool) {
	c.connOnce.Do(func() {
		c.connBase, c.connEnvelope = "/v1/connections", false
		c.resolvePrefix(ctx)
		req, err := http.NewRequestWithContext(
			ctx, http.MethodGet, c.baseURL+c.applyPrefix("/v1/connections"), nil,
		)
		if err != nil {
			return
		}
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		resp, err := c.httpClient.Do(req)
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		if err == nil && resp.StatusCode == http.StatusNotFound {
			c.connBase, c.connEnvelope = "/v1/apps/connections", true
		}
	})
	return c.connBase, c.connEnvelope
}

// doProject executes a request scoped to an explicit project (slug or id).
// The resolved project id is sent as X-Project-Id — how /v1 servers scope
// requests — and the legacy ?projectId= query is kept alongside for old
// /api servers. An empty project means "the API key's own project".
func (c *Client) doProject(ctx context.Context, method, path, project string, body any, result any) error {
	c.resolvePrefix(ctx)
	var projectHeader string
	if project != "" {
		path = withProjectQuery(path, project)
		projectHeader = c.resolveProjectID(ctx, project)
	}
	path = c.applyPrefix(path)
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if projectHeader != "" {
		req.Header.Set("X-Project-Id", projectHeader)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.networkError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return parseAPIError(resp.StatusCode, respBody)
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unexpected response from server (status %d) — expected JSON", resp.StatusCode)
		}
	}
	return nil
}
