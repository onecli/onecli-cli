package api

import (
	"context"
	"fmt"
	"net/http"
)

// Connection represents an OAuth connection returned by the API.
type Connection struct {
	ID          string `json:"id"`
	Provider    string `json:"provider"`
	Status      string `json:"status"`
	ConnectedAt string `json:"connectedAt"`
}

// ListOrgConnections returns all connections for the organization.
func (c *Client) ListOrgConnections(ctx context.Context) ([]Connection, error) {
	var connections []Connection
	if err := c.do(ctx, http.MethodGet, "/v1/org/apps/connections", nil, &connections); err != nil {
		return nil, fmt.Errorf("listing org connections: %w", err)
	}
	return connections, nil
}

// ListOrgConnectionsByProvider returns connections for a specific provider.
func (c *Client) ListOrgConnectionsByProvider(ctx context.Context, provider string) ([]Connection, error) {
	var connections []Connection
	if err := c.do(ctx, http.MethodGet, "/v1/org/apps/connections/"+provider, nil, &connections); err != nil {
		return nil, fmt.Errorf("listing org connections for provider: %w", err)
	}
	return connections, nil
}

// DeleteOrgConnection removes an org-scoped connection by ID.
func (c *Client) DeleteOrgConnection(ctx context.Context, connectionID string) error {
	if err := c.do(ctx, http.MethodDelete, "/v1/org/apps/connections/"+connectionID, nil, nil); err != nil {
		return fmt.Errorf("deleting org connection: %w", err)
	}
	return nil
}
