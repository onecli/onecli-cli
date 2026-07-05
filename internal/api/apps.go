package api

import (
	"context"
	"fmt"
	"net/http"
)

// App represents an app from the /v1/apps endpoints.
type App struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Available      bool       `json:"available"`
	ConnectionType string     `json:"connectionType"`
	Configurable   bool       `json:"configurable"`
	Config         *AppConfig `json:"config"`
	// Deprecated: first connection only — misleading for multi-account
	// providers. Prefer Connections.
	Connection      *AppConnection  `json:"connection"`
	Connections     []AppConnection `json:"connections,omitempty"`
	CredentialStubs []any           `json:"credentialStubs,omitempty"`
	Hint            string          `json:"hint,omitempty"`
}

// AppConfig is the BYOC credential configuration status.
type AppConfig struct {
	HasCredentials bool `json:"hasCredentials"`
	Enabled        bool `json:"enabled"`
}

// AppConnection is the OAuth connection status.
type AppConnection struct {
	ID          string   `json:"id"`
	Provider    string   `json:"provider"`
	Label       string   `json:"label,omitempty"`
	Status      string   `json:"status"`
	Scopes      []string `json:"scopes"`
	Scope       string   `json:"scope,omitempty"`
	ConnectedAt string   `json:"connectedAt"`
}

// ConfigFields holds BYOC credential fields for a provider. Keys are
// validated server-side against the app's own configurable field definitions
// (e.g. clientId/clientSecret for OAuth apps; appId/appSlug/privateKey for
// github-app) — unknown keys are stripped by the server.
type ConfigFields map[string]string

// ListApps returns all apps with their config and connection status.
func (c *Client) ListApps(ctx context.Context) ([]App, error) {
	var apps []App
	if err := c.do(ctx, http.MethodGet, "/v1/apps", nil, &apps); err != nil {
		return nil, fmt.Errorf("listing apps: %w", err)
	}
	return apps, nil
}

// GetApp returns a single app by provider name.
func (c *Client) GetApp(ctx context.Context, provider string) (*App, error) {
	var app App
	if err := c.do(ctx, http.MethodGet, "/v1/apps/"+provider, nil, &app); err != nil {
		return nil, fmt.Errorf("getting app: %w", err)
	}
	return &app, nil
}

// Project connection operations live in connections.go (top-level
// /v1/connections resource with a legacy-path fallback).

// AppTool is one operation in an app's permission catalog. The API serves
// only the tool's identity (id/name/description); the endpoint mapping
// behind a tool is resolved server-side from its toolId.
type AppTool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AppToolGroup groups an app's tools by read/write category, optionally with
// a wildcard tool covering the whole group.
type AppToolGroup struct {
	Category string    `json:"category"`
	Tools    []AppTool `json:"tools"`
	Wildcard *AppTool  `json:"wildcard,omitempty"`
}

// PermissionDefinition is an app's static tool catalog — the toolIds that
// rules permissions get/set operate on.
type PermissionDefinition struct {
	Provider string         `json:"provider"`
	Groups   []AppToolGroup `json:"groups"`
}

// GetPermissionDefinition returns an app's permission catalog. Works without
// a project context (the catalog is global static data).
func (c *Client) GetPermissionDefinition(ctx context.Context, provider string) (*PermissionDefinition, error) {
	var def PermissionDefinition
	if err := c.do(ctx, http.MethodGet, "/v1/apps/"+provider+"/permission-definition", nil, &def); err != nil {
		return nil, fmt.Errorf("getting permission definition: %w", err)
	}
	return &def, nil
}

// ConfigureApp saves BYOC credentials for a provider.
func (c *Client) ConfigureApp(ctx context.Context, provider string, fields ConfigFields) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPost, "/v1/apps/"+provider+"/config", fields, &resp); err != nil {
		return fmt.Errorf("configuring app: %w", err)
	}
	return nil
}

// UnconfigureApp removes BYOC credentials for a provider.
func (c *Client) UnconfigureApp(ctx context.Context, provider string) error {
	if err := c.do(ctx, http.MethodDelete, "/v1/apps/"+provider+"/config", nil, nil); err != nil {
		return fmt.Errorf("unconfiguring app: %w", err)
	}
	return nil
}
