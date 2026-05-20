package api

import (
	"context"
	"fmt"
	"net/http"
)

// PermissionState describes the permission state for a single tool.
type PermissionState struct {
	Permission string `json:"permission"`
	Conditions []any  `json:"conditions"`
}

// PermissionChange is a single tool permission change for SetAppPermissions.
type PermissionChange struct {
	ToolID     string `json:"toolId"`
	Permission string `json:"permission"`
}

// SetPermissionsInput is the request body for setting app permissions.
type SetPermissionsInput struct {
	Changes    []PermissionChange `json:"changes"`
	Conditions []any              `json:"conditions,omitempty"`
}

// ListOrgRules returns all policy rules scoped to the organization.
func (c *Client) ListOrgRules(ctx context.Context) ([]Rule, error) {
	var rules []Rule
	if err := c.do(ctx, http.MethodGet, "/v1/org/rules", nil, &rules); err != nil {
		return nil, fmt.Errorf("listing org rules: %w", err)
	}
	return rules, nil
}

// GetOrgRule returns a single org-scoped policy rule by ID.
func (c *Client) GetOrgRule(ctx context.Context, id string) (*Rule, error) {
	var rule Rule
	if err := c.do(ctx, http.MethodGet, "/v1/org/rules/"+id, nil, &rule); err != nil {
		return nil, fmt.Errorf("getting org rule: %w", err)
	}
	return &rule, nil
}

// CreateOrgRule creates a policy rule at the organization level.
func (c *Client) CreateOrgRule(ctx context.Context, input CreateRuleInput) (*Rule, error) {
	var rule Rule
	if err := c.do(ctx, http.MethodPost, "/v1/org/rules", input, &rule); err != nil {
		return nil, fmt.Errorf("creating org rule: %w", err)
	}
	return &rule, nil
}

// UpdateOrgRule updates an org-scoped policy rule.
func (c *Client) UpdateOrgRule(ctx context.Context, id string, input UpdateRuleInput) (*Rule, error) {
	var rule Rule
	if err := c.do(ctx, http.MethodPatch, "/v1/org/rules/"+id, input, &rule); err != nil {
		return nil, fmt.Errorf("updating org rule: %w", err)
	}
	return &rule, nil
}

// DeleteOrgRule deletes an org-scoped policy rule.
func (c *Client) DeleteOrgRule(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/v1/org/rules/"+id, nil, nil); err != nil {
		return fmt.Errorf("deleting org rule: %w", err)
	}
	return nil
}

// GetAppPermissions returns the tool-level permission states for a provider.
func (c *Client) GetAppPermissions(ctx context.Context, provider string) (map[string]PermissionState, error) {
	var states map[string]PermissionState
	if err := c.do(ctx, http.MethodGet, "/v1/org/rules/permissions/"+provider, nil, &states); err != nil {
		return nil, fmt.Errorf("getting app permissions: %w", err)
	}
	return states, nil
}

// SetAppPermissions updates tool-level permissions for a provider.
func (c *Client) SetAppPermissions(ctx context.Context, provider string, input SetPermissionsInput) error {
	var resp any
	if err := c.do(ctx, http.MethodPut, "/v1/org/rules/permissions/"+provider, input, &resp); err != nil {
		return fmt.Errorf("setting app permissions: %w", err)
	}
	return nil
}
