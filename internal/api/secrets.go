package api

import (
	"context"
	"fmt"
	"net/http"
)

// Secret represents a secret returned by the API.
type Secret struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Type            string           `json:"type"`
	HostPattern     string           `json:"hostPattern"`
	PathPattern     *string          `json:"pathPattern"`
	InjectionConfig *InjectionConfig `json:"injectionConfig"`
	CreatedAt       string           `json:"createdAt"`
	TypeLabel       string           `json:"typeLabel,omitempty"`
	Preview         string           `json:"preview,omitempty"`
	Warning         string           `json:"warning,omitempty"`
}

// InjectionConfig describes how a secret is injected into requests.
// Either HeaderName or ParamName should be set, not both.
type InjectionConfig struct {
	HeaderName  string `json:"headerName,omitempty"`
	ValueFormat string `json:"valueFormat,omitempty"`
	ParamName   string `json:"paramName,omitempty"`
	ParamFormat string `json:"paramFormat,omitempty"`
}

// CreateSecretInput is the request body for creating a secret.
type CreateSecretInput struct {
	Name            string           `json:"name"`
	Type            string           `json:"type"`
	Value           string           `json:"value"`
	HostPattern     string           `json:"hostPattern"`
	PathPattern     string           `json:"pathPattern,omitempty"`
	InjectionConfig *InjectionConfig `json:"injectionConfig,omitempty"`
}

// UpdateSecretInput is the request body for updating a secret.
type UpdateSecretInput struct {
	Value           *string          `json:"value,omitempty"`
	HostPattern     *string          `json:"hostPattern,omitempty"`
	PathPattern     *string          `json:"pathPattern,omitempty"`
	InjectionConfig *InjectionConfig `json:"injectionConfig,omitempty"`
}

// ListSecrets returns all secrets for the authenticated user.
// If projectID is non-empty, results are scoped to that project.
func (c *Client) ListSecrets(ctx context.Context, projectID string) ([]Secret, error) {
	path := withProjectQuery("/v1/secrets", projectID)
	var secrets []Secret
	if err := c.do(ctx, http.MethodGet, path, nil, &secrets); err != nil {
		return nil, fmt.Errorf("listing secrets: %w", err)
	}
	return secrets, nil
}

// CreateSecret creates a new secret.
// If projectID is non-empty, the secret is created in that project.
func (c *Client) CreateSecret(ctx context.Context, projectID string, input CreateSecretInput) (*Secret, error) {
	path := withProjectQuery("/v1/secrets", projectID)
	var secret Secret
	if err := c.do(ctx, http.MethodPost, path, input, &secret); err != nil {
		return nil, fmt.Errorf("creating secret: %w", err)
	}
	return &secret, nil
}

// UpdateSecret updates an existing secret.
func (c *Client) UpdateSecret(ctx context.Context, id string, input UpdateSecretInput) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPatch, "/v1/secrets/"+id, input, &resp); err != nil {
		return fmt.Errorf("updating secret: %w", err)
	}
	return nil
}

// DeleteSecret deletes a secret by ID.
func (c *Client) DeleteSecret(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/v1/secrets/"+id, nil, nil); err != nil {
		return fmt.Errorf("deleting secret: %w", err)
	}
	return nil
}
