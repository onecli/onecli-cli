package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// ListConnections returns the project's app connections, optionally filtered
// by provider. Prefers the top-level /v1/connections resource (bare array,
// ?provider= filter); falls back once per process to the legacy
// /v1/apps/connections paths ({connections} envelope, filter-in-path) on
// older servers.
func (c *Client) ListConnections(ctx context.Context, provider string) ([]AppConnection, error) {
	base, envelope := c.resolveConnectionsBase(ctx)
	if envelope {
		path := base
		if provider != "" {
			path += "/" + url.PathEscape(provider)
		}
		var resp struct {
			Connections []AppConnection `json:"connections"`
		}
		if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
			return nil, fmt.Errorf("listing connections: %w", err)
		}
		return resp.Connections, nil
	}
	path := base
	if provider != "" {
		path += "?provider=" + url.QueryEscape(provider)
	}
	var connections []AppConnection
	if err := c.do(ctx, http.MethodGet, path, nil, &connections); err != nil {
		return nil, fmt.Errorf("listing connections: %w", err)
	}
	return connections, nil
}

// DisconnectApp removes an app connection by ID.
func (c *Client) DisconnectApp(ctx context.Context, connectionID string) error {
	base, _ := c.resolveConnectionsBase(ctx)
	if err := c.do(ctx, http.MethodDelete, base+"/"+connectionID, nil, nil); err != nil {
		return fmt.Errorf("disconnecting app: %w", err)
	}
	return nil
}

// RenameConnection sets the display label of an app connection.
func (c *Client) RenameConnection(ctx context.Context, connectionID, label string) (*AppConnection, error) {
	base, _ := c.resolveConnectionsBase(ctx)
	var conn AppConnection
	body := map[string]string{"label": label}
	if err := c.do(ctx, http.MethodPatch, base+"/"+connectionID, body, &conn); err != nil {
		return nil, fmt.Errorf("renaming connection: %w", err)
	}
	return &conn, nil
}
