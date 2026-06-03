package api

import (
	"context"
	"fmt"
	"net/http"
)

// OrgAppConfig is the config status for an org-scoped app provider.
type OrgAppConfig struct {
	HasCredentials bool `json:"hasCredentials"`
	Enabled        bool `json:"enabled"`
}

// ToggleInput is the request body for toggling app config.
type ToggleInput struct {
	Enabled bool `json:"enabled"`
}

// ListConfiguredProviders returns providers that have org-level credentials configured.
func (c *Client) ListConfiguredProviders(ctx context.Context) ([]any, error) {
	var providers []any
	if err := c.do(ctx, http.MethodGet, "/v1/org/apps/configured", nil, &providers); err != nil {
		return nil, fmt.Errorf("listing configured providers: %w", err)
	}
	return providers, nil
}

// GetOrgAppConfig returns the app config for a provider at the org level.
func (c *Client) GetOrgAppConfig(ctx context.Context, provider string) (*OrgAppConfig, error) {
	var config OrgAppConfig
	if err := c.do(ctx, http.MethodGet, "/v1/org/apps/"+provider+"/config", nil, &config); err != nil {
		return nil, fmt.Errorf("getting org app config: %w", err)
	}
	return &config, nil
}

// UpsertOrgAppConfig saves BYOC credentials for a provider at the org level.
func (c *Client) UpsertOrgAppConfig(ctx context.Context, provider string, input ConfigAppInput) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPost, "/v1/org/apps/"+provider+"/config", input, &resp); err != nil {
		return fmt.Errorf("configuring org app: %w", err)
	}
	return nil
}

// DeleteOrgAppConfig removes BYOC credentials for a provider at the org level.
func (c *Client) DeleteOrgAppConfig(ctx context.Context, provider string) error {
	if err := c.do(ctx, http.MethodDelete, "/v1/org/apps/"+provider+"/config", nil, nil); err != nil {
		return fmt.Errorf("removing org app config: %w", err)
	}
	return nil
}

// ToggleOrgAppConfig enables or disables an app config at the org level.
func (c *Client) ToggleOrgAppConfig(ctx context.Context, provider string, enabled bool) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPatch, "/v1/org/apps/"+provider+"/config/toggle", ToggleInput{Enabled: enabled}, &resp); err != nil {
		return fmt.Errorf("toggling org app config: %w", err)
	}
	return nil
}
