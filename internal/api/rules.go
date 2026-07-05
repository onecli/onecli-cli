package api

import (
	"context"
	"fmt"
	"net/http"
)

// Rule represents a policy rule returned by the API. The endpoint fields
// (hostPattern/pathPattern/method) are present on custom rules only —
// app-permission rules (metadata.source == "app_permission") omit them and
// are identified by metadata.provider + metadata.toolId instead.
type Rule struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	HostPattern     *string         `json:"hostPattern,omitempty"`
	PathPattern     *string         `json:"pathPattern,omitempty"`
	Method          *string         `json:"method,omitempty"`
	Action          string          `json:"action"`
	Enabled         bool            `json:"enabled"`
	AgentID         *string         `json:"agentId"`
	RateLimit       *int            `json:"rateLimit"`
	RateLimitWindow *string         `json:"rateLimitWindow"`
	Scope           *string         `json:"scope,omitempty"`
	Conditions      []RuleCondition `json:"conditions,omitempty"`
	Metadata        any             `json:"metadata,omitempty"`
	CreatedAt       string          `json:"createdAt"`
}

// RuleCondition matches request content (e.g. body contains a value).
// Key optionally narrows the match to a specific field within the target.
type RuleCondition struct {
	Target   string `json:"target"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
	Key      string `json:"key,omitempty"`
}

// CreateRuleInput is the request body for creating a rule.
type CreateRuleInput struct {
	Name            string          `json:"name"`
	HostPattern     string          `json:"hostPattern"`
	PathPattern     string          `json:"pathPattern,omitempty"`
	Method          string          `json:"method,omitempty"`
	Action          string          `json:"action"`
	Enabled         bool            `json:"enabled"`
	AgentID         string          `json:"agentId,omitempty"`
	RateLimit       *int            `json:"rateLimit,omitempty"`
	RateLimitWindow string          `json:"rateLimitWindow,omitempty"`
	Conditions      []RuleCondition `json:"conditions,omitempty"`
}

// UpdateRuleInput is the request body for updating a rule.
// Conditions uses a pointer so an explicit empty slice clears existing
// conditions while nil omits the field entirely.
type UpdateRuleInput struct {
	Name            *string          `json:"name,omitempty"`
	HostPattern     *string          `json:"hostPattern,omitempty"`
	PathPattern     *string          `json:"pathPattern,omitempty"`
	Method          *string          `json:"method,omitempty"`
	Action          *string          `json:"action,omitempty"`
	Enabled         *bool            `json:"enabled,omitempty"`
	AgentID         *string          `json:"agentId,omitempty"`
	RateLimit       *int             `json:"rateLimit,omitempty"`
	RateLimitWindow *string          `json:"rateLimitWindow,omitempty"`
	Conditions      *[]RuleCondition `json:"conditions,omitempty"`
}

// ListRules returns all policy rules for the authenticated user.
// If projectID is non-empty, results are scoped to that project.
func (c *Client) ListRules(ctx context.Context, projectID string) ([]Rule, error) {
	var rules []Rule
	if err := c.doProject(ctx, http.MethodGet, "/v1/rules", projectID, nil, &rules); err != nil {
		return nil, fmt.Errorf("listing rules: %w", err)
	}
	return rules, nil
}

// GetRule returns a single policy rule by ID.
func (c *Client) GetRule(ctx context.Context, id string) (*Rule, error) {
	var rule Rule
	if err := c.do(ctx, http.MethodGet, "/v1/rules/"+id, nil, &rule); err != nil {
		return nil, fmt.Errorf("getting rule: %w", err)
	}
	return &rule, nil
}

// CreateRule creates a new policy rule.
// If projectID is non-empty, the rule is created in that project.
func (c *Client) CreateRule(ctx context.Context, projectID string, input CreateRuleInput) (*Rule, error) {
	var rule Rule
	if err := c.doProject(ctx, http.MethodPost, "/v1/rules", projectID, input, &rule); err != nil {
		return nil, fmt.Errorf("creating rule: %w", err)
	}
	return &rule, nil
}

// UpdateRule updates an existing policy rule and returns the updated rule.
// The PATCH endpoint returns {success:true}, so the rule is re-fetched.
func (c *Client) UpdateRule(ctx context.Context, id string, input UpdateRuleInput) (*Rule, error) {
	if err := c.do(ctx, http.MethodPatch, "/v1/rules/"+id, input, nil); err != nil {
		return nil, fmt.Errorf("updating rule: %w", err)
	}
	return c.GetRule(ctx, id)
}

// DeleteRule deletes a policy rule by ID.
func (c *Client) DeleteRule(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/v1/rules/"+id, nil, nil); err != nil {
		return fmt.Errorf("deleting rule: %w", err)
	}
	return nil
}

// GetRulePermissions returns the layered app-permission states for a
// provider at the project scope (Defaults = all-agents layer, ByAgent =
// per-agent override layers).
func (c *Client) GetRulePermissions(ctx context.Context, provider string) (*AppPermissionStates, error) {
	var states AppPermissionStates
	if err := c.do(ctx, http.MethodGet, "/v1/rules/permissions/"+provider, nil, &states); err != nil {
		return nil, fmt.Errorf("getting rule permissions: %w", err)
	}
	return &states, nil
}

// SetRulePermissions updates app-permission states for a provider at the
// project scope. Input.AgentID targets one agent's override layer; the
// "inherit" permission removes an agent's override for a tool.
func (c *Client) SetRulePermissions(ctx context.Context, provider string, input SetPermissionsInput) error {
	var resp any
	if err := c.do(ctx, http.MethodPut, "/v1/rules/permissions/"+provider, input, &resp); err != nil {
		return fmt.Errorf("setting rule permissions: %w", err)
	}
	return nil
}

// OverlapCount is the number of custom rules overlapping an app's hosts.
type OverlapCount struct {
	Count int `json:"count"`
}

// GetRuleOverlap counts custom rules overlapping an app's hosts (project).
func (c *Client) GetRuleOverlap(ctx context.Context, provider string) (*OverlapCount, error) {
	var count OverlapCount
	if err := c.do(ctx, http.MethodGet, "/v1/rules/overlap/"+provider, nil, &count); err != nil {
		return nil, fmt.Errorf("getting rule overlap: %w", err)
	}
	return &count, nil
}
