package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// PermissionState describes the permission state for a single tool.
type PermissionState struct {
	Permission string `json:"permission"`
	Conditions []any  `json:"conditions"`
}

// AppPermissionStates is the layered permission response: Defaults holds the
// all-agents states (toolId → state); ByAgent holds per-agent override
// layers (agentId → toolId → state; always empty at org scope, where rules
// are agent-less).
type AppPermissionStates struct {
	Defaults map[string]PermissionState            `json:"defaults"`
	ByAgent  map[string]map[string]PermissionState `json:"byAgent"`
}

// PermissionChange is a single tool permission change for SetAppPermissions.
type PermissionChange struct {
	ToolID     string `json:"toolId"`
	Permission string `json:"permission"`
}

// SetPermissionsInput is the request body for setting app permissions.
// AgentID targets one agent's override layer (project scope only — org
// permissions are agent-less and the org endpoint rejects it).
type SetPermissionsInput struct {
	Changes    []PermissionChange `json:"changes"`
	Conditions []any              `json:"conditions,omitempty"`
	AgentID    string             `json:"agentId,omitempty"`
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

// UpdateOrgRule updates an org-scoped policy rule. The PATCH endpoint
// returns {success:true}, so the updated rule is re-fetched for output.
func (c *Client) UpdateOrgRule(ctx context.Context, id string, input UpdateRuleInput) (*Rule, error) {
	if err := c.do(ctx, http.MethodPatch, "/v1/org/rules/"+id, input, nil); err != nil {
		return nil, fmt.Errorf("updating org rule: %w", err)
	}
	return c.GetOrgRule(ctx, id)
}

// DeleteOrgRule deletes an org-scoped policy rule.
func (c *Client) DeleteOrgRule(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/v1/org/rules/"+id, nil, nil); err != nil {
		return fmt.Errorf("deleting org rule: %w", err)
	}
	return nil
}

// GetAppPermissions returns the layered tool-level permission states for a
// provider at the organization scope. Servers predating the layered shape
// returned a flat {toolId: state} map — detected and lifted into Defaults so
// the output is never silently empty against an older deployment.
func (c *Client) GetAppPermissions(ctx context.Context, provider string) (*AppPermissionStates, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/v1/org/rules/permissions/"+provider, nil, &raw); err != nil {
		return nil, fmt.Errorf("getting app permissions: %w", err)
	}
	var states AppPermissionStates
	if err := json.Unmarshal(raw, &states); err != nil {
		return nil, fmt.Errorf("decoding app permissions: %w", err)
	}
	if states.Defaults == nil && states.ByAgent == nil {
		var flat map[string]PermissionState
		if err := json.Unmarshal(raw, &flat); err == nil && len(flat) > 0 {
			states.Defaults = flat
			states.ByAgent = map[string]map[string]PermissionState{}
		}
	}
	return &states, nil
}

// SetAppPermissions updates tool-level permissions for a provider.
func (c *Client) SetAppPermissions(ctx context.Context, provider string, input SetPermissionsInput) error {
	var resp any
	if err := c.do(ctx, http.MethodPut, "/v1/org/rules/permissions/"+provider, input, &resp); err != nil {
		return fmt.Errorf("setting app permissions: %w", err)
	}
	return nil
}

// GetOrgRuleOverlap counts custom rules overlapping an app's hosts (org).
func (c *Client) GetOrgRuleOverlap(ctx context.Context, provider string) (*OverlapCount, error) {
	var count OverlapCount
	if err := c.do(ctx, http.MethodGet, "/v1/org/rules/overlap/"+provider, nil, &count); err != nil {
		return nil, fmt.Errorf("getting org rule overlap: %w", err)
	}
	return &count, nil
}
