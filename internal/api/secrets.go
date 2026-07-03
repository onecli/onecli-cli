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
	ValueSource     string           `json:"valueSource,omitempty"`
	OpRef           string           `json:"opRef,omitempty"`
	Scope           *string          `json:"scope,omitempty"`
	Metadata        any              `json:"metadata,omitempty"`
	CreatedAt       string           `json:"createdAt"`
	TypeLabel       string           `json:"typeLabel,omitempty"`
	Preview         string           `json:"preview,omitempty"`
	Warning         string           `json:"warning,omitempty"`
}

// InjectionConfig describes how a secret is injected into requests.
// Either HeaderName or ParamName should be set, not both. The path fields
// carry path-injection configs (decoded for display; the CLI flags don't
// build them — use --json).
type InjectionConfig struct {
	HeaderName      string `json:"headerName,omitempty"`
	ValueFormat     string `json:"valueFormat,omitempty"`
	ParamName       string `json:"paramName,omitempty"`
	ParamFormat     string `json:"paramFormat,omitempty"`
	PathTemplate    string `json:"pathTemplate,omitempty"`
	PathRegex       string `json:"pathRegex,omitempty"`
	PathReplacement string `json:"pathReplacement,omitempty"`
}

// CreateSecretInput is the request body for creating a secret. The
// valueSource/opRef/opDisplay fields carry 1Password-sourced secrets via
// --json payloads.
type CreateSecretInput struct {
	Name            string           `json:"name"`
	Type            string           `json:"type"`
	Value           string           `json:"value,omitempty"`
	ValueSource     string           `json:"valueSource,omitempty"`
	OpRef           string           `json:"opRef,omitempty"`
	OpDisplay       any              `json:"opDisplay,omitempty"`
	HostPattern     string           `json:"hostPattern"`
	PathPattern     string           `json:"pathPattern,omitempty"`
	InjectionConfig *InjectionConfig `json:"injectionConfig,omitempty"`
}

// UpdateSecretInput is the request body for updating a secret.
type UpdateSecretInput struct {
	Name            *string          `json:"name,omitempty"`
	Value           *string          `json:"value,omitempty"`
	ValueSource     *string          `json:"valueSource,omitempty"`
	OpRef           *string          `json:"opRef,omitempty"`
	HostPattern     *string          `json:"hostPattern,omitempty"`
	PathPattern     *string          `json:"pathPattern,omitempty"`
	InjectionConfig *InjectionConfig `json:"injectionConfig,omitempty"`
}

// ListSecrets returns all secrets for the authenticated user.
// If projectID is non-empty, results are scoped to that project.
func (c *Client) ListSecrets(ctx context.Context, projectID string) ([]Secret, error) {
	var secrets []Secret
	if err := c.doProject(ctx, http.MethodGet, "/v1/secrets", projectID, nil, &secrets); err != nil {
		return nil, fmt.Errorf("listing secrets: %w", err)
	}
	return secrets, nil
}

// CreateSecret creates a new secret.
// If projectID is non-empty, the secret is created in that project.
func (c *Client) CreateSecret(ctx context.Context, projectID string, input CreateSecretInput) (*Secret, error) {
	var secret Secret
	if err := c.doProject(ctx, http.MethodPost, "/v1/secrets", projectID, input, &secret); err != nil {
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
