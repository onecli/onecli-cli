package api

import (
	"context"
	"fmt"
	"net/http"
)

// ListOrgSecrets returns all secrets scoped to the organization.
func (c *Client) ListOrgSecrets(ctx context.Context) ([]Secret, error) {
	var secrets []Secret
	if err := c.do(ctx, http.MethodGet, "/v1/org/secrets", nil, &secrets); err != nil {
		return nil, fmt.Errorf("listing org secrets: %w", err)
	}
	return secrets, nil
}

// CreateOrgSecret creates a secret at the organization level.
func (c *Client) CreateOrgSecret(ctx context.Context, input CreateSecretInput) (*Secret, error) {
	var secret Secret
	if err := c.do(ctx, http.MethodPost, "/v1/org/secrets", input, &secret); err != nil {
		return nil, fmt.Errorf("creating org secret: %w", err)
	}
	return &secret, nil
}

// UpdateOrgSecret updates an org-scoped secret.
func (c *Client) UpdateOrgSecret(ctx context.Context, id string, input UpdateSecretInput) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPatch, "/v1/org/secrets/"+id, input, &resp); err != nil {
		return fmt.Errorf("updating org secret: %w", err)
	}
	return nil
}

// DeleteOrgSecret deletes an org-scoped secret by ID.
func (c *Client) DeleteOrgSecret(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/v1/org/secrets/"+id, nil, nil); err != nil {
		return fmt.Errorf("deleting org secret: %w", err)
	}
	return nil
}
