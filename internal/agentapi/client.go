// Package agentapi is the HTTP client for the agent-facing /v2 routes: the
// connect trade, sign-up, and the /v2/agents/me reads. Calls go straight to
// the API and never through a proxy from the environment: a shell that read
// agent.env points HTTPS_PROXY at the gateway, and these calls are not for it.
package agentapi

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
	"time"

	"github.com/onecli/onecli-cli/internal/api"
)

// CredentialHeader carries the credential a place already holds.
const CredentialHeader = "OneCLI-Agent-Credential"

// Client talks to the /v2 agent routes.
type Client struct {
	baseURL string
	http    *http.Client
}

// New makes a client for the API at baseURL.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    newHTTPClient(),
	}
}

// BaseURL is the API origin this client calls.
func (c *Client) BaseURL() string { return c.baseURL }

func newHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	// A shell that read agent.env holds SSL_CERT_FILE at the gateway's
	// certificate alone. Add it to the system roots rather than replace them.
	if f := os.Getenv("SSL_CERT_FILE"); f != "" {
		if data, err := os.ReadFile(f); err == nil {
			pool, perr := x509.SystemCertPool()
			if perr != nil || pool == nil {
				pool = x509.NewCertPool()
			}
			pool.AppendCertsFromPEM(data)
			tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
		}
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: tr}
}

// Error is a /v2 error answer: {"error":{"code","message","details"}}.
type Error struct {
	StatusCode int
	Code       string
	Message    string
	Details    []ErrorDetail
	RetryAfter int
}

// ErrorDetail is one entry of an error's details.
type ErrorDetail struct {
	Field   string `json:"field"`
	Issue   string `json:"issue"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

// HasIssue reports whether any detail names issue.
func (e *Error) HasIssue(issue string) bool {
	for _, d := range e.Details {
		if d.Issue == issue {
			return true
		}
	}
	return false
}

// AsAPIError maps the error onto the CLI's shared error type, so the usual
// exit codes apply.
func (e *Error) AsAPIError() *api.APIError {
	return &api.APIError{StatusCode: e.StatusCode, Message: e.Message, Type: e.Code}
}

func parseError(status int, body []byte, header http.Header) *Error {
	var env struct {
		Error struct {
			Code    string        `json:"code"`
			Message string        `json:"message"`
			Details []ErrorDetail `json:"details"`
		} `json:"error"`
	}
	e := &Error{StatusCode: status}
	if json.Unmarshal(body, &env) == nil && env.Error.Message != "" {
		e.Code = env.Error.Code
		e.Message = env.Error.Message
		e.Details = env.Error.Details
	} else {
		var flat struct {
			Error  string `json:"error"`
			Reason string `json:"reason"`
		}
		switch {
		case json.Unmarshal(body, &flat) != nil:
			e.Message = fmt.Sprintf("the server answered %d", status)
		case flat.Error != "":
			e.Message = flat.Error
		case flat.Reason != "":
			e.Message = flat.Reason
		default:
			e.Message = fmt.Sprintf("the server answered %d", status)
		}
	}
	if ra := header.Get("Retry-After"); ra != "" {
		var n int
		if _, err := fmt.Sscanf(ra, "%d", &n); err == nil && n > 0 {
			e.RetryAfter = n
		}
	}
	return e
}

type request struct {
	method  string
	path    string
	bearer  string
	headers map[string]string
	body    any
}

// do sends one call. It returns the status and the raw body of a 2xx answer.
func (c *Client) do(ctx context.Context, r request) (int, []byte, error) {
	var rd io.Reader
	if r.body != nil {
		data, err := json.Marshal(r.body)
		if err != nil {
			return 0, nil, fmt.Errorf("encoding request: %w", err)
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, c.baseURL+r.path, rd)
	if err != nil {
		return 0, nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if r.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+r.bearer)
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, c.networkError(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return resp.StatusCode, nil, parseError(resp.StatusCode, body, resp.Header)
	}
	return resp.StatusCode, body, nil
}

func (c *Client) networkError(err error) error {
	host := c.baseURL
	if u, e := url.Parse(c.baseURL); e == nil && u.Host != "" {
		host = u.Host
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Timeout() {
		return fmt.Errorf("the request to %s timed out", host)
	}
	return fmt.Errorf("could not reach %s: %w", host, err)
}

func decode[T any](status int, body []byte) (T, error) {
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		return v, fmt.Errorf("unexpected answer from the server (status %d): not the expected JSON", status)
	}
	return v, nil
}

// Gateway is where an agent reaches the gateway.
type Gateway struct {
	Address string `json:"address"`
	Scheme  string `json:"scheme"`
}

// Agent is an agent as the connect and sign-up answers give it. Credential
// is set only on the answer that made the agent.
type Agent struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	VaultID    string  `json:"vaultId"`
	Runtime    *string `json:"runtime"`
	CreatedAt  string  `json:"createdAt"`
	ExpiresAt  *string `json:"expiresAt"`
	Gateway    Gateway `json:"gateway"`
	Credential string  `json:"credential,omitempty"`
}

// ConnectInput is the body of the connect trade.
type ConnectInput struct {
	Runtime string `json:"runtime"`
	Replace bool   `json:"replace"`
	Name    string `json:"name,omitempty"`
}

// Connect trades a connect token for an agent: POST /v2/agents. held is the
// credential this place already holds, or "". created is false when the
// place was connected to this vault already (200, no credential).
func (c *Client) Connect(ctx context.Context, token, held string, in ConnectInput) (Agent, bool, error) {
	headers := map[string]string{}
	if held != "" {
		headers[CredentialHeader] = held
	}
	status, body, err := c.do(ctx, request{
		method: http.MethodPost, path: "/v2/agents", bearer: token, headers: headers, body: in,
	})
	if err != nil {
		return Agent{}, false, err
	}
	a, err := decode[Agent](status, body)
	return a, status == http.StatusCreated, err
}

// Signup is a started sign-up.
type Signup struct {
	ID              string `json:"id"`
	UserCode        string `json:"userCode"`
	VerificationURL string `json:"verificationUrl"`
	PollToken       string `json:"pollToken"`
	ExpiresAt       string `json:"expiresAt"`
	Interval        int    `json:"interval"`
}

// SignupInput is the body that starts a sign-up.
type SignupInput struct {
	Name    string `json:"name,omitempty"`
	Runtime string `json:"runtime,omitempty"`
}

// StartSignup starts a sign-up: POST /v2/agent-signups, no credential.
func (c *Client) StartSignup(ctx context.Context, in SignupInput) (Signup, error) {
	status, body, err := c.do(ctx, request{method: http.MethodPost, path: "/v2/agent-signups", body: in})
	if err != nil {
		return Signup{}, err
	}
	return decode[Signup](status, body)
}

// PollSignup polls once: POST /v2/agent-signups/{id}/token. A pending
// sign-up answers an *Error with Code authorization_pending or slow_down.
func (c *Client) PollSignup(ctx context.Context, id, pollToken string) (Agent, error) {
	status, body, err := c.do(ctx, request{
		method: http.MethodPost,
		path:   "/v2/agent-signups/" + url.PathEscape(id) + "/token",
		body:   map[string]string{"pollToken": pollToken},
	})
	if err != nil {
		return Agent{}, err
	}
	return decode[Agent](status, body)
}

// Self is the calling agent: GET /v2/agents/me.
type Self struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Vault     IDName  `json:"vault"`
	Runtime   *string `json:"runtime"`
	ExpiresAt *string `json:"expiresAt"`
	Gateway   Gateway `json:"gateway"`
}

// IDName is an id and a name.
type IDName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Me reads the calling agent.
func (c *Client) Me(ctx context.Context, credential string) (Self, error) {
	status, body, err := c.do(ctx, request{method: http.MethodGet, path: "/v2/agents/me", bearer: credential})
	if err != nil {
		return Self{}, err
	}
	return decode[Self](status, body)
}

// Item is one item of the agent's vault.
type Item struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	Service       *string  `json:"service"`
	Hosts         []string `json:"hosts"`
	Allows        []string `json:"allows"`
	NeedsApproval bool     `json:"needsApproval"`
	State         string   `json:"state"`
	Example       *string  `json:"example"`
}

type itemsPage struct {
	Data       []Item  `json:"data"`
	NextCursor *string `json:"nextCursor"`
}

// maxItemPages bounds the walk over pages, so a server that keeps answering
// a cursor cannot hold the CLI forever.
const maxItemPages = 100

// Items lists every item of the agent's vault, all pages.
func (c *Client) Items(ctx context.Context, credential string) ([]Item, error) {
	items := []Item{}
	cursor := ""
	for range maxItemPages {
		q := url.Values{"limit": {"100"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		status, body, err := c.do(ctx, request{
			method: http.MethodGet, path: "/v2/agents/me/items?" + q.Encode(), bearer: credential,
		})
		if err != nil {
			return nil, err
		}
		page, err := decode[itemsPage](status, body)
		if err != nil {
			return nil, err
		}
		items = append(items, page.Data...)
		if page.NextCursor == nil || *page.NextCursor == "" {
			return items, nil
		}
		cursor = *page.NextCursor
	}
	return nil, fmt.Errorf("the item list did not end after %d pages", maxItemPages)
}

// Check is what would happen to one call.
type Check struct {
	Decision string  `json:"decision"`
	Item     *IDRef  `json:"item,omitempty"`
	Rule     *RuleOf `json:"rule,omitempty"`
	Reason   string  `json:"reason"`
}

// IDRef names a thing by id.
type IDRef struct {
	ID string `json:"id"`
}

// RuleOf is the rule that decided.
type RuleOf struct {
	ID   *string `json:"id"`
	Name string  `json:"name"`
}

// ErrUnknown is returned with a Check when the gateway could not decide (503).
var ErrUnknown = errors.New("the gateway could not decide; never read this as allowed")

// CheckCall asks what would happen to one call: POST /v2/agents/me/check.
func (c *Client) CheckCall(ctx context.Context, credential, method, rawURL string) (Check, error) {
	status, body, err := c.do(ctx, request{
		method: http.MethodPost, path: "/v2/agents/me/check", bearer: credential,
		body: map[string]string{"method": method, "url": rawURL},
	})
	var e *Error
	if errors.As(err, &e) && e.StatusCode == http.StatusServiceUnavailable {
		// The 503 answer is {decision:"unknown", reason}, not the error shape.
		return Check{Decision: "unknown", Reason: e.Message}, ErrUnknown
	}
	if err != nil {
		return Check{}, err
	}
	return decode[Check](status, body)
}

// CA fetches the gateway's certificate: GET /v1/gateway/ca.
func (c *Client) CA(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/gateway/ca", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, c.networkError(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading the certificate: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp.StatusCode, body, resp.Header)
	}
	if !bytes.HasPrefix(bytes.TrimLeft(body, "\r\n\t "), []byte("-----BEGIN CERTIFICATE-----")) {
		return nil, errors.New("the gateway's certificate did not look like one")
	}
	return body, nil
}

// DeleteAgent disconnects an agent with a person's key: DELETE /v2/agents/{id}.
func (c *Client) DeleteAgent(ctx context.Context, key, id string) error {
	_, _, err := c.do(ctx, request{
		method: http.MethodDelete, path: "/v2/agents/" + url.PathEscape(id), bearer: key,
	})
	return err
}

// Skill fetches the agent page: GET /v2/skill, markdown.
func (c *Client) Skill(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v2/skill", nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", c.networkError(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading the skill: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", parseError(resp.StatusCode, body, resp.Header)
	}
	return string(body), nil
}

// AppOrigin is the dashboard's address, as GET /v1/instance names it
// (origins.external). It must be a plain http or https origin.
func (c *Client) AppOrigin(ctx context.Context) (string, error) {
	status, body, err := c.do(ctx, request{method: http.MethodGet, path: "/v1/instance"})
	if err != nil {
		return "", err
	}
	inst, err := decode[struct {
		Origins struct {
			External string `json:"external"`
		} `json:"origins"`
	}](status, body)
	if err != nil {
		return "", err
	}
	origin := strings.TrimRight(inst.Origins.External, "/")
	u, perr := url.Parse(origin)
	if origin == "" || perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("the server did not name a plain dashboard address")
	}
	return origin, nil
}
