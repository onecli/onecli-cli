package api

import (
	"context"
	"fmt"
	"net/http"
)

// blocklistBase returns the blocklist path prefix for a scope.
func blocklistBase(org bool, provider string) string {
	if org {
		return "/v1/org/apps/" + provider + "/blocklist"
	}
	return "/v1/apps/" + provider + "/blocklist"
}

// GetBlocklist returns the blocklist state for a provider (default hosts and
// custom rules with their enabled state).
func (c *Client) GetBlocklist(ctx context.Context, provider string, org bool) (any, error) {
	var states any
	if err := c.do(ctx, http.MethodGet, blocklistBase(org, provider), nil, &states); err != nil {
		return nil, fmt.Errorf("getting blocklist: %w", err)
	}
	return states, nil
}

// ActivateBlocklistHost enables one of the app's predefined blocklist hosts.
func (c *Client) ActivateBlocklistHost(ctx context.Context, provider, hostID string, org bool) (any, error) {
	var result any
	body := map[string]string{"hostId": hostID}
	if err := c.do(ctx, http.MethodPost, blocklistBase(org, provider), body, &result); err != nil {
		return nil, fmt.Errorf("activating blocklist host: %w", err)
	}
	return result, nil
}

// AddBlocklistRule adds a custom blocklist rule for a provider.
func (c *Client) AddBlocklistRule(ctx context.Context, provider, name, hostPattern string, org bool) (any, error) {
	var result any
	body := map[string]string{"name": name, "hostPattern": hostPattern}
	if err := c.do(ctx, http.MethodPost, blocklistBase(org, provider), body, &result); err != nil {
		return nil, fmt.Errorf("adding blocklist rule: %w", err)
	}
	return result, nil
}

// ToggleBlocklistRule enables or disables a blocklist rule.
func (c *Client) ToggleBlocklistRule(ctx context.Context, provider, ruleID string, enabled, org bool) error {
	body := map[string]bool{"enabled": enabled}
	if err := c.do(ctx, http.MethodPatch, blocklistBase(org, provider)+"/"+ruleID, body, nil); err != nil {
		return fmt.Errorf("toggling blocklist rule: %w", err)
	}
	return nil
}

// RemoveBlocklistRule removes a blocklist rule.
func (c *Client) RemoveBlocklistRule(ctx context.Context, provider, ruleID string, org bool) error {
	if err := c.do(ctx, http.MethodDelete, blocklistBase(org, provider)+"/"+ruleID, nil, nil); err != nil {
		return fmt.Errorf("removing blocklist rule: %w", err)
	}
	return nil
}
