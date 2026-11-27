package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// CLIAuthSession is the answer to POST /v1/auth/cli/session: a one-time
// code and the dashboard page where a signed-in person confirms it.
type CLIAuthSession struct {
	Code    string `json:"code"`
	AuthURL string `json:"auth_url"`
}

// CLIAuthPoll is the answer to GET /v1/auth/cli/poll. Status is "pending",
// "expired" or "ok". APIKey is set on "ok" only, and only on the first poll
// after the person confirmed: the server hands it out once.
type CLIAuthPoll struct {
	Status      string  `json:"status"`
	APIKey      string  `json:"api_key,omitempty"`
	WorkspaceID *string `json:"workspace_id,omitempty"`
}

// StartCLIAuth opens a browser login session. It needs no credentials.
func (c *Client) StartCLIAuth(ctx context.Context) (*CLIAuthSession, error) {
	var s CLIAuthSession
	if err := c.do(ctx, http.MethodPost, "/v1/auth/cli/session", nil, &s); err != nil {
		return nil, fmt.Errorf("starting browser login: %w", err)
	}
	return &s, nil
}

// PollCLIAuth asks once whether the browser login was confirmed.
func (c *Client) PollCLIAuth(ctx context.Context, code string) (*CLIAuthPoll, error) {
	var p CLIAuthPoll
	path := "/v1/auth/cli/poll?code=" + url.QueryEscape(code)
	if err := c.do(ctx, http.MethodGet, path, nil, &p); err != nil {
		return nil, fmt.Errorf("checking browser login: %w", err)
	}
	return &p, nil
}
