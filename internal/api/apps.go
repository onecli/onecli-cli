package api

import (
	"context"
	"fmt"
	"net/http"
)

// App represents an app from the /api/apps endpoints.
type App struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Available      bool           `json:"available"`
	ConnectionType string         `json:"connectionType"`
	Configurable   bool           `json:"configurable"`
	Config         *AppConfig     `json:"config"`
	Connection     *AppConnection `json:"connection"`
	Hint           string         `json:"hint,omitempty"`
}

// AppConfig is the BYOC credential configuration status.
type AppConfig struct {
	HasCredentials bool `json:"hasCredentials"`
	Enabled        bool `json:"enabled"`
}

// AppConnection is the OAuth connection status.
type AppConnection struct {
	Status      string   `json:"status"`
	Scopes      []string `json:"scopes"`
	ConnectedAt string   `json:"connectedAt"`
}

// ConfigAppInput is the request body for saving BYOC credentials.
type ConfigAppInput struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

// ListApps returns all apps with their config and connection status.
func (c *Client) ListApps(ctx context.Context) ([]App, error) {
	var apps []App
	if err := c.do(ctx, http.MethodGet, "/api/apps", nil, &apps); err != nil {
		return nil, fmt.Errorf("listing apps: %w", err)
	}
	return apps, nil
}

// GetApp returns a single app by provider name.
func (c *Client) GetApp(ctx context.Context, provider string) (*App, error) {
	var app App
	if err := c.do(ctx, http.MethodGet, "/api/apps/"+provider, nil, &app); err != nil {
		return nil, fmt.Errorf("getting app: %w", err)
	}
	return &app, nil
}

// ConfigureApp saves BYOC credentials for a provider.
func (c *Client) ConfigureApp(ctx context.Context, provider string, input ConfigAppInput) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPost, "/api/apps/"+provider+"/config", input, &resp); err != nil {
		return fmt.Errorf("configuring app: %w", err)
	}
	return nil
}

// UnconfigureApp removes BYOC credentials for a provider.
func (c *Client) UnconfigureApp(ctx context.Context, provider string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/apps/"+provider+"/config", nil, nil); err != nil {
		return fmt.Errorf("unconfiguring app: %w", err)
	}
	return nil
}

// DisconnectApp removes the OAuth connection for a provider.
func (c *Client) DisconnectApp(ctx context.Context, provider string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/apps/"+provider+"/connection", nil, nil); err != nil {
		return fmt.Errorf("disconnecting app: %w", err)
	}
	return nil
}
