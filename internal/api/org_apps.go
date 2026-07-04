package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// OrgAppConfig is the config status for an org-scoped app provider.
type OrgAppConfig struct {
	Settings       map[string]string `json:"settings,omitempty"`
	HasCredentials bool              `json:"hasCredentials"`
	Enabled        bool              `json:"enabled"`
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
func (c *Client) UpsertOrgAppConfig(ctx context.Context, provider string, fields ConfigFields) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPost, "/v1/org/apps/"+provider+"/config", fields, &resp); err != nil {
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

// ConnectAppInput is the request body for a direct (API key / imported
// credentials) app connect. Field names must match the app's own connection
// method field definitions.
type ConnectAppInput struct {
	Fields       ConfigFields `json:"fields"`
	ConnectionID string       `json:"connectionId,omitempty"`
	Label        string       `json:"label,omitempty"`
	Method       string       `json:"method,omitempty"`
}

// ConnectOrgApp connects an app at the organization level with direct
// credentials. The resulting connection is shared by every project in the
// organization.
func (c *Client) ConnectOrgApp(ctx context.Context, provider string, input ConnectAppInput) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPost, "/v1/org/apps/"+url.PathEscape(provider)+"/connect", input, &resp); err != nil {
		return fmt.Errorf("connecting org app: %w", err)
	}
	return nil
}

// OrgAppAuthorizeURL starts the org-scoped OAuth flow for a provider and
// returns the provider's authorize URL from the redirect Location — without
// following it, so the caller can hand the URL to a browser.
func (c *Client) OrgAppAuthorizeURL(ctx context.Context, provider, connectionID string) (string, error) {
	c.resolvePrefix(ctx)
	path := "/v1/org/apps/" + url.PathEscape(provider) + "/authorize"
	if connectionID != "" {
		path += "?connectionId=" + url.QueryEscape(connectionID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+c.applyPrefix(path), nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	// Shallow copy of the shared client with redirects disabled — the 302
	// Location IS the result here.
	noRedirect := *c.httpClient
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := noRedirect.Do(req)
	if err != nil {
		return "", c.networkError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", parseAPIError(resp.StatusCode, body)
	}
	if resp.StatusCode < 300 {
		return "", fmt.Errorf("expected an authorize redirect, got status %d", resp.StatusCode)
	}
	// Resolves a relative Location against the request URL and errors when
	// the header is missing.
	location, err := resp.Location()
	if err != nil {
		return "", fmt.Errorf("authorize redirect carried no Location header")
	}
	return location.String(), nil
}
