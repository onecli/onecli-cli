package api

import (
	"context"
	"fmt"
	"net/http"
)

// Rule represents a policy rule returned by the API.
type Rule struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	HostPattern     string  `json:"hostPattern"`
	PathPattern     *string `json:"pathPattern"`
	Method          *string `json:"method"`
	Action          string  `json:"action"`
	Enabled         bool    `json:"enabled"`
	AgentID         *string `json:"agentId"`
	RateLimit       *int    `json:"rateLimit"`
	RateLimitWindow *string `json:"rateLimitWindow"`
	CreatedAt       string  `json:"createdAt"`
}

// CreateRuleInput is the request body for creating a rule.
type CreateRuleInput struct {
	Name            string `json:"name"`
	HostPattern     string `json:"hostPattern"`
	PathPattern     string `json:"pathPattern,omitempty"`
	Method          string `json:"method,omitempty"`
	Action          string `json:"action"`
	Enabled         bool   `json:"enabled"`
	AgentID         string `json:"agentId,omitempty"`
	RateLimit       *int   `json:"rateLimit,omitempty"`
	RateLimitWindow string `json:"rateLimitWindow,omitempty"`
}

// UpdateRuleInput is the request body for updating a rule.
type UpdateRuleInput struct {
	Name            *string `json:"name,omitempty"`
	HostPattern     *string `json:"hostPattern,omitempty"`
	PathPattern     *string `json:"pathPattern,omitempty"`
	Method          *string `json:"method,omitempty"`
	Action          *string `json:"action,omitempty"`
	Enabled         *bool   `json:"enabled,omitempty"`
	AgentID         *string `json:"agentId,omitempty"`
	RateLimit       *int    `json:"rateLimit,omitempty"`
	RateLimitWindow *string `json:"rateLimitWindow,omitempty"`
}

// ListRules returns all policy rules for the authenticated user.
func (c *Client) ListRules(ctx context.Context) ([]Rule, error) {
	var rules []Rule
	if err := c.do(ctx, http.MethodGet, "/api/rules", nil, &rules); err != nil {
		return nil, fmt.Errorf("listing rules: %w", err)
	}
	return rules, nil
}

// GetRule returns a single policy rule by ID.
func (c *Client) GetRule(ctx context.Context, id string) (*Rule, error) {
	var rule Rule
	if err := c.do(ctx, http.MethodGet, "/api/rules/"+id, nil, &rule); err != nil {
		return nil, fmt.Errorf("getting rule: %w", err)
	}
	return &rule, nil
}

// CreateRule creates a new policy rule.
func (c *Client) CreateRule(ctx context.Context, input CreateRuleInput) (*Rule, error) {
	var rule Rule
	if err := c.do(ctx, http.MethodPost, "/api/rules", input, &rule); err != nil {
		return nil, fmt.Errorf("creating rule: %w", err)
	}
	return &rule, nil
}

// UpdateRule updates an existing policy rule and returns the updated rule.
func (c *Client) UpdateRule(ctx context.Context, id string, input UpdateRuleInput) (*Rule, error) {
	var rule Rule
	if err := c.do(ctx, http.MethodPatch, "/api/rules/"+id, input, &rule); err != nil {
		return nil, fmt.Errorf("updating rule: %w", err)
	}
	return &rule, nil
}

// DeleteRule deletes a policy rule by ID.
func (c *Client) DeleteRule(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/rules/"+id, nil, nil); err != nil {
		return fmt.Errorf("deleting rule: %w", err)
	}
	return nil
}
