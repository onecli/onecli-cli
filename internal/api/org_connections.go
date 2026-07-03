package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Connection represents an OAuth connection returned by the API.
type Connection struct {
	ID          string   `json:"id"`
	Provider    string   `json:"provider"`
	Label       string   `json:"label,omitempty"`
	Status      string   `json:"status"`
	Scopes      []string `json:"scopes,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	ConnectedAt string   `json:"connectedAt"`
}

// ListOrgConnections returns the organization's connections, optionally
// filtered by provider. /v1/org/connections is served by every deployed
// server (it predates and now supersedes the /v1/org/apps/connections
// alias); provider filtering uses the path form because older servers drop
// query params on this route.
func (c *Client) ListOrgConnections(ctx context.Context, provider string) ([]Connection, error) {
	path := "/v1/org/connections"
	if provider != "" {
		path += "/" + url.PathEscape(provider)
	}
	var connections []Connection
	if err := c.do(ctx, http.MethodGet, path, nil, &connections); err != nil {
		return nil, fmt.Errorf("listing org connections: %w", err)
	}
	return connections, nil
}

// DeleteOrgConnection removes an org-scoped connection by ID.
func (c *Client) DeleteOrgConnection(ctx context.Context, connectionID string) error {
	if err := c.do(ctx, http.MethodDelete, "/v1/org/connections/"+connectionID, nil, nil); err != nil {
		return fmt.Errorf("deleting org connection: %w", err)
	}
	return nil
}

// RenameOrgConnection sets the display label of an org-scoped connection.
func (c *Client) RenameOrgConnection(ctx context.Context, connectionID, label string) (*Connection, error) {
	var conn Connection
	body := map[string]string{"label": label}
	if err := c.do(ctx, http.MethodPatch, "/v1/org/connections/"+connectionID, body, &conn); err != nil {
		return nil, fmt.Errorf("renaming org connection: %w", err)
	}
	return &conn, nil
}
