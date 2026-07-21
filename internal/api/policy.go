package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// The /v1/policy surface (and its /v1/org/policy mirror): the first-match
// policy engine's staged model. Writes land in a DRAFT; POST /publish
// snapshots the whole scope draft into a new published generation; the
// gateway enforces the published set only (?status=published, max
// generation). Published row ids are ephemeral (regenerated every publish) —
// `logicalId` is the only identity stable across statuses and generations.

// PolicyRule is a rule row from /v1/policy (draft working copy or a
// published snapshot). Source "custom" rows are user-owned; "blocklist" and
// "equipment" rows are system-managed (equipment is injection-only) and
// "default" is the scope's terminal Default Rule.
type PolicyRule struct {
	ID              string           `json:"id"`
	Scope           string           `json:"scope"`
	Status          string           `json:"status"`
	Generation      int              `json:"generation"`
	Priority        int              `json:"priority"`
	Enabled         bool             `json:"enabled"`
	IsDefault       bool             `json:"isDefault"`
	LogicalID       string           `json:"logicalId"`
	Source          string           `json:"source"`
	Name            string           `json:"name"`
	Description     *string          `json:"description"`
	Action          string           `json:"action"`
	RateLimit       *int             `json:"rateLimit"`
	RateLimitWindow *string          `json:"rateLimitWindow"`
	RequireApproval bool             `json:"requireApproval"`
	Conditions      any              `json:"conditions,omitempty"`
	Identities      []PolicyIdentity `json:"identities"`
	Targets         []PolicyTarget   `json:"targets"`
	CreatedAt       string           `json:"createdAt"`
}

// PolicyIdentity names a principal a rule applies to. Project rules accept
// type "agent" only; org rules accept "agentGroup", "user", or "group".
// An empty identity list means "any".
type PolicyIdentity struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// PolicyTarget is one destination a rule matches — a tagged union on Kind:
// "app" (Provider + optional Tools/ConnectionScope), "connection"
// (ConnectionID + optional Tools), "secret" (SecretID XOR SecretScope), or
// "network" (HostPattern + optional PathPattern/Method). Optional fields are
// omitted when empty; the server owns semantic validation.
type PolicyTarget struct {
	Kind            string   `json:"kind"`
	Provider        string   `json:"provider,omitempty"`
	Tools           []string `json:"tools,omitempty"`
	ConnectionScope string   `json:"connectionScope,omitempty"`
	ConnectionID    string   `json:"connectionId,omitempty"`
	SecretID        string   `json:"secretId,omitempty"`
	SecretScope     string   `json:"secretScope,omitempty"`
	HostPattern     string   `json:"hostPattern,omitempty"`
	PathPattern     string   `json:"pathPattern,omitempty"`
	Method          string   `json:"method,omitempty"`
}

// CreatePolicyRuleInput is the POST /v1/policy/rules body. Targets must name
// at least one destination; RateLimit/RateLimitWindow are paired and — like
// RequireApproval — valid only on action "allow".
type CreatePolicyRuleInput struct {
	Name            string           `json:"name"`
	Description     string           `json:"description,omitempty"`
	Enabled         *bool            `json:"enabled,omitempty"`
	Action          string           `json:"action"`
	RateLimit       *int             `json:"rateLimit,omitempty"`
	RateLimitWindow string           `json:"rateLimitWindow,omitempty"`
	RequireApproval *bool            `json:"requireApproval,omitempty"`
	Conditions      *json.RawMessage `json:"conditions,omitempty"`
	Identities      []PolicyIdentity `json:"identities,omitempty"`
	Targets         []PolicyTarget   `json:"targets,omitempty"`
}

// UpdatePolicyRuleInput is the flag-built PATCH body — nil omits a field
// (omitempty), so this struct can only SET values, never clear them. Explicit
// JSON-null clears (rateLimit: null, conditions: null, …) are expressed via
// the raw --json path, which bypasses this struct entirely and sends the
// payload verbatim as json.RawMessage.
type UpdatePolicyRuleInput struct {
	Name            *string           `json:"name,omitempty"`
	Description     *string           `json:"description,omitempty"`
	Enabled         *bool             `json:"enabled,omitempty"`
	Action          *string           `json:"action,omitempty"`
	RateLimit       *int              `json:"rateLimit,omitempty"`
	RateLimitWindow *string           `json:"rateLimitWindow,omitempty"`
	RequireApproval *bool             `json:"requireApproval,omitempty"`
	Conditions      *json.RawMessage  `json:"conditions,omitempty"`
	Identities      *[]PolicyIdentity `json:"identities,omitempty"`
	Targets         *[]PolicyTarget   `json:"targets,omitempty"`
}

// PolicyPublishResult is the POST /v1/policy/publish response.
type PolicyPublishResult struct {
	Generation int `json:"generation"`
	RuleCount  int `json:"ruleCount"`
}

// PolicyLastPublish describes the most recent publish; the API returns JSON
// null when the scope was never published (surfaced here as a nil pointer).
type PolicyLastPublish struct {
	Generation int    `json:"generation"`
	RuleCount  int    `json:"ruleCount"`
	AppliedAt  string `json:"appliedAt"`
	AppliedBy  *struct {
		Name  *string `json:"name"`
		Email string  `json:"email"`
	} `json:"appliedBy"`
}

// reorderPolicyInput is the PUT /v1/policy/rules/order body.
type reorderPolicyInput struct {
	OrderedIDs []string `json:"orderedIds"`
}

// setPolicyDefaultInput is the PATCH /v1/policy/default body.
type setPolicyDefaultInput struct {
	Action string `json:"action"`
}

// mapPolicyRouteErr upgrades a route-miss 404 into an actionable message: a
// resource-level 404 from a current server carries type "not_found_error";
// a 404 WITHOUT it means the route itself doesn't exist — an older server
// (the /api-prefix fallback) or, for the org surface, a community edition
// without the org policy routes.
func mapPolicyRouteErr(err error, org bool) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	if apiErr.StatusCode != http.StatusNotFound || apiErr.Type == "not_found_error" {
		return err
	}
	if org {
		return fmt.Errorf(
			"this server does not support the org policy API — it requires OneCLI Cloud or an Enterprise self-hosted release; legacy 'onecli org rules' commands work here",
		)
	}
	return fmt.Errorf(
		"this server does not support the policy API — it requires OneCLI Cloud or an updated self-hosted release; legacy 'onecli rules' commands work here",
	)
}

func policyStatusQuery(base, status string) string {
	if status == "" {
		return base
	}
	return base + "?status=" + url.QueryEscape(status)
}

// ── Project scope (/v1/policy) — project selects via doProject ──────────────

// ListPolicyRules lists the scope's non-default rules (draft by default;
// status "published" = the enforced set at the active generation).
func (c *Client) ListPolicyRules(ctx context.Context, project, status string) ([]PolicyRule, error) {
	var rules []PolicyRule
	path := policyStatusQuery("/v1/policy/rules", status)
	if err := c.doProject(ctx, http.MethodGet, path, project, nil, &rules); err != nil {
		return nil, mapPolicyRouteErr(err, false)
	}
	return rules, nil
}

// GetPolicyRule fetches one DRAFT rule (the working copy). Published row ids
// are ephemeral — list with status "published" and match by logicalId.
func (c *Client) GetPolicyRule(ctx context.Context, project, id string) (*PolicyRule, error) {
	var rule PolicyRule
	path := "/v1/policy/rules/" + url.PathEscape(id)
	if err := c.doProject(ctx, http.MethodGet, path, project, nil, &rule); err != nil {
		return nil, mapPolicyRouteErr(err, false)
	}
	return &rule, nil
}

// CreatePolicyRule creates a DRAFT rule (staged until publish).
func (c *Client) CreatePolicyRule(ctx context.Context, project string, input CreatePolicyRuleInput) (*PolicyRule, error) {
	var rule PolicyRule
	if err := c.doProject(ctx, http.MethodPost, "/v1/policy/rules", project, input, &rule); err != nil {
		return nil, mapPolicyRouteErr(err, false)
	}
	return &rule, nil
}

// UpdatePolicyRule patches a DRAFT rule. The body is sent EXACTLY as given
// (any JSON-marshalable value): callers building from flags pass an
// UpdatePolicyRuleInput; the raw --json path passes json.RawMessage so
// explicit nulls (which clear nullable fields server-side) survive — a typed
// pointer round-trip would silently drop them.
func (c *Client) UpdatePolicyRule(ctx context.Context, project, id string, body any) (*PolicyRule, error) {
	var rule PolicyRule
	path := "/v1/policy/rules/" + url.PathEscape(id)
	if err := c.doProject(ctx, http.MethodPatch, path, project, body, &rule); err != nil {
		return nil, mapPolicyRouteErr(err, false)
	}
	return &rule, nil
}

// DeletePolicyRule removes a DRAFT rule (staged until publish).
func (c *Client) DeletePolicyRule(ctx context.Context, project, id string) error {
	path := "/v1/policy/rules/" + url.PathEscape(id)
	if err := c.doProject(ctx, http.MethodDelete, path, project, nil, nil); err != nil {
		return mapPolicyRouteErr(err, false)
	}
	return nil
}

// ReorderPolicyRules sets the draft's first-match order. orderedIDs must
// name EVERY non-default draft rule exactly once (system rows included) —
// the server 409s on any mismatch.
func (c *Client) ReorderPolicyRules(ctx context.Context, project string, orderedIDs []string) ([]PolicyRule, error) {
	var rules []PolicyRule
	input := reorderPolicyInput{OrderedIDs: orderedIDs}
	if err := c.doProject(ctx, http.MethodPut, "/v1/policy/rules/order", project, input, &rules); err != nil {
		return nil, mapPolicyRouteErr(err, false)
	}
	return rules, nil
}

// GetPolicyDefault fetches the scope's terminal Default Rule (a virtual
// default is returned when none is persisted yet — it reflects the scope's
// baseline posture, not necessarily an enforced snapshot).
func (c *Client) GetPolicyDefault(ctx context.Context, project, status string) (*PolicyRule, error) {
	var rule PolicyRule
	path := policyStatusQuery("/v1/policy/default", status)
	if err := c.doProject(ctx, http.MethodGet, path, project, nil, &rule); err != nil {
		return nil, mapPolicyRouteErr(err, false)
	}
	return &rule, nil
}

// SetPolicyDefault sets the Default Rule's action on the draft.
func (c *Client) SetPolicyDefault(ctx context.Context, project, action string) (*PolicyRule, error) {
	var rule PolicyRule
	input := setPolicyDefaultInput{Action: action}
	if err := c.doProject(ctx, http.MethodPatch, "/v1/policy/default", project, input, &rule); err != nil {
		return nil, mapPolicyRouteErr(err, false)
	}
	return &rule, nil
}

// PublishPolicy snapshots the WHOLE scope draft into a new published
// generation — including any changes staged by other users.
func (c *Client) PublishPolicy(ctx context.Context, project string) (*PolicyPublishResult, error) {
	var result PolicyPublishResult
	if err := c.doProject(ctx, http.MethodPost, "/v1/policy/publish", project, struct{}{}, &result); err != nil {
		return nil, mapPolicyRouteErr(err, false)
	}
	return &result, nil
}

// GetPolicyLastPublish returns the most recent publish, or nil when the
// scope was never published.
func (c *Client) GetPolicyLastPublish(ctx context.Context, project string) (*PolicyLastPublish, error) {
	var last *PolicyLastPublish
	if err := c.doProject(ctx, http.MethodGet, "/v1/policy/last-publish", project, nil, &last); err != nil {
		return nil, mapPolicyRouteErr(err, false)
	}
	return last, nil
}

// ── Org scope (/v1/org/policy) — the key's org; admin role required ─────────

// ListOrgPolicyRules lists the org scope's non-default rules.
func (c *Client) ListOrgPolicyRules(ctx context.Context, status string) ([]PolicyRule, error) {
	var rules []PolicyRule
	path := policyStatusQuery("/v1/org/policy/rules", status)
	if err := c.do(ctx, http.MethodGet, path, nil, &rules); err != nil {
		return nil, mapPolicyRouteErr(err, true)
	}
	return rules, nil
}

// GetOrgPolicyRule fetches one DRAFT org rule.
func (c *Client) GetOrgPolicyRule(ctx context.Context, id string) (*PolicyRule, error) {
	var rule PolicyRule
	path := "/v1/org/policy/rules/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodGet, path, nil, &rule); err != nil {
		return nil, mapPolicyRouteErr(err, true)
	}
	return &rule, nil
}

// CreateOrgPolicyRule creates a DRAFT org rule.
func (c *Client) CreateOrgPolicyRule(ctx context.Context, input CreatePolicyRuleInput) (*PolicyRule, error) {
	var rule PolicyRule
	if err := c.do(ctx, http.MethodPost, "/v1/org/policy/rules", input, &rule); err != nil {
		return nil, mapPolicyRouteErr(err, true)
	}
	return &rule, nil
}

// UpdateOrgPolicyRule patches a DRAFT org rule (body semantics as
// UpdatePolicyRule: sent exactly as given so raw --json nulls survive).
func (c *Client) UpdateOrgPolicyRule(ctx context.Context, id string, body any) (*PolicyRule, error) {
	var rule PolicyRule
	path := "/v1/org/policy/rules/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodPatch, path, body, &rule); err != nil {
		return nil, mapPolicyRouteErr(err, true)
	}
	return &rule, nil
}

// DeleteOrgPolicyRule removes a DRAFT org rule.
func (c *Client) DeleteOrgPolicyRule(ctx context.Context, id string) error {
	path := "/v1/org/policy/rules/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return mapPolicyRouteErr(err, true)
	}
	return nil
}

// ReorderOrgPolicyRules sets the org draft's first-match order (full
// permutation contract, as ReorderPolicyRules).
func (c *Client) ReorderOrgPolicyRules(ctx context.Context, orderedIDs []string) ([]PolicyRule, error) {
	var rules []PolicyRule
	input := reorderPolicyInput{OrderedIDs: orderedIDs}
	if err := c.do(ctx, http.MethodPut, "/v1/org/policy/rules/order", input, &rules); err != nil {
		return nil, mapPolicyRouteErr(err, true)
	}
	return rules, nil
}

// GetOrgPolicyDefault fetches the org's terminal Default Rule.
func (c *Client) GetOrgPolicyDefault(ctx context.Context, status string) (*PolicyRule, error) {
	var rule PolicyRule
	path := policyStatusQuery("/v1/org/policy/default", status)
	if err := c.do(ctx, http.MethodGet, path, nil, &rule); err != nil {
		return nil, mapPolicyRouteErr(err, true)
	}
	return &rule, nil
}

// SetOrgPolicyDefault sets the org Default Rule's action on the draft.
func (c *Client) SetOrgPolicyDefault(ctx context.Context, action string) (*PolicyRule, error) {
	var rule PolicyRule
	input := setPolicyDefaultInput{Action: action}
	if err := c.do(ctx, http.MethodPatch, "/v1/org/policy/default", input, &rule); err != nil {
		return nil, mapPolicyRouteErr(err, true)
	}
	return &rule, nil
}

// PublishOrgPolicy snapshots the whole ORG draft into a new generation.
func (c *Client) PublishOrgPolicy(ctx context.Context) (*PolicyPublishResult, error) {
	var result PolicyPublishResult
	if err := c.do(ctx, http.MethodPost, "/v1/org/policy/publish", struct{}{}, &result); err != nil {
		return nil, mapPolicyRouteErr(err, true)
	}
	return &result, nil
}

// GetOrgPolicyLastPublish returns the org's most recent publish, or nil.
func (c *Client) GetOrgPolicyLastPublish(ctx context.Context) (*PolicyLastPublish, error) {
	var last *PolicyLastPublish
	if err := c.do(ctx, http.MethodGet, "/v1/org/policy/last-publish", nil, &last); err != nil {
		return nil, mapPolicyRouteErr(err, true)
	}
	return last, nil
}
