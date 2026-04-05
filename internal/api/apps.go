package api

import (
	"context"
	"fmt"
	"net/http"
)

// App represents an app connection returned by the API.
type App struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	Status    string `json:"status"`
	Docs      string `json:"docs,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// ConnectAppInput is the request body for connecting an app.
type ConnectAppInput struct {
	Provider     string `json:"provider"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

// ListApps returns all app connections for the authenticated user.
func (c *Client) ListApps(ctx context.Context) ([]App, error) {
	var apps []App
	if err := c.do(ctx, http.MethodGet, "/api/apps", nil, &apps); err != nil {
		return nil, fmt.Errorf("listing apps: %w", err)
	}
	return apps, nil
}

// ConnectApp creates a new app connection.
func (c *Client) ConnectApp(ctx context.Context, input ConnectAppInput) (*App, error) {
	var app App
	if err := c.do(ctx, http.MethodPost, "/api/apps", input, &app); err != nil {
		return nil, fmt.Errorf("connecting app: %w", err)
	}
	return &app, nil
}

// DisconnectApp removes an app connection by ID.
func (c *Client) DisconnectApp(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/apps/"+id, nil, nil); err != nil {
		return fmt.Errorf("disconnecting app: %w", err)
	}
	return nil
}
