package api

import (
	"context"
	"fmt"
	"net/http"
)

// VaultConnection is an external vault (e.g. 1Password) pairing.
type VaultConnection struct {
	ID              string  `json:"id"`
	Provider        string  `json:"provider"`
	Status          string  `json:"status"`
	Name            *string `json:"name"`
	LastConnectedAt *string `json:"lastConnectedAt"`
}

// ListVaults returns the project's external vault connections.
func (c *Client) ListVaults(ctx context.Context) ([]VaultConnection, error) {
	var vaults []VaultConnection
	if err := c.do(ctx, http.MethodGet, "/v1/vaults", nil, &vaults); err != nil {
		return nil, fmt.Errorf("listing vaults: %w", err)
	}
	return vaults, nil
}

// ResourceCounts is the project's resource totals.
type ResourceCounts struct {
	Agents  int `json:"agents"`
	Apps    int `json:"apps"`
	Llms    int `json:"llms"`
	Secrets int `json:"secrets"`
}

// GetCounts returns the project's resource counts.
func (c *Client) GetCounts(ctx context.Context) (*ResourceCounts, error) {
	var counts ResourceCounts
	if err := c.do(ctx, http.MethodGet, "/v1/counts", nil, &counts); err != nil {
		return nil, fmt.Errorf("getting counts: %w", err)
	}
	return &counts, nil
}

// OrgSettings holds organization-level settings.
type OrgSettings struct {
	PolicyMode string `json:"policyMode"`
}

// GetOrgSettings returns the organization's settings.
func (c *Client) GetOrgSettings(ctx context.Context) (*OrgSettings, error) {
	var settings OrgSettings
	if err := c.do(ctx, http.MethodGet, "/v1/org/settings", nil, &settings); err != nil {
		return nil, fmt.Errorf("getting org settings: %w", err)
	}
	return &settings, nil
}

// UpdateOrgSettings updates the organization's policy mode
// ("allow" = YOLO default-allow, "deny" = lockdown default-deny).
func (c *Client) UpdateOrgSettings(ctx context.Context, policyMode string) (*OrgSettings, error) {
	var settings OrgSettings
	body := map[string]string{"policyMode": policyMode}
	if err := c.do(ctx, http.MethodPatch, "/v1/org/settings", body, &settings); err != nil {
		return nil, fmt.Errorf("updating org settings: %w", err)
	}
	return &settings, nil
}

// AppConfigStatus is a provider's BYOC config status (project scope).
type AppConfigStatus struct {
	Settings       map[string]string `json:"settings,omitempty"`
	HasCredentials bool              `json:"hasCredentials"`
	Enabled        bool              `json:"enabled"`
}

// GetAppConfig returns the project-scope config status for a provider.
func (c *Client) GetAppConfig(ctx context.Context, provider string) (*AppConfigStatus, error) {
	var config AppConfigStatus
	if err := c.do(ctx, http.MethodGet, "/v1/apps/"+provider+"/config", nil, &config); err != nil {
		return nil, fmt.Errorf("getting app config: %w", err)
	}
	return &config, nil
}

// ToggleAppConfig enables or disables a provider's config (project scope).
func (c *Client) ToggleAppConfig(ctx context.Context, provider string, enabled bool) error {
	var resp SuccessResponse
	body := ToggleInput{Enabled: enabled}
	if err := c.do(ctx, http.MethodPatch, "/v1/apps/"+provider+"/config/toggle", body, &resp); err != nil {
		return fmt.Errorf("toggling app config: %w", err)
	}
	return nil
}

// ListConfiguredApps returns providers with an enabled config (project).
func (c *Client) ListConfiguredApps(ctx context.Context) ([]string, error) {
	var providers []string
	if err := c.do(ctx, http.MethodGet, "/v1/apps/configured", nil, &providers); err != nil {
		return nil, fmt.Errorf("listing configured apps: %w", err)
	}
	return providers, nil
}

// ListEnvDefaultApps returns providers with platform default credentials.
func (c *Client) ListEnvDefaultApps(ctx context.Context) ([]string, error) {
	var providers []string
	if err := c.do(ctx, http.MethodGet, "/v1/apps/env-defaults", nil, &providers); err != nil {
		return nil, fmt.Errorf("listing env-default apps: %w", err)
	}
	return providers, nil
}
