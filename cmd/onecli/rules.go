package main

import (
	"encoding/json"
	"fmt"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// RulesCmd is the `onecli rules` command group.
type RulesCmd struct {
	List        RulesListCmd        `cmd:"" help:"List all policy rules."`
	Get         RulesGetCmd         `cmd:"" help:"Get a single policy rule by ID."`
	Create      RulesCreateCmd      `cmd:"" help:"Create a new policy rule."`
	Update      RulesUpdateCmd      `cmd:"" help:"Update an existing policy rule."`
	Delete      RulesDeleteCmd      `cmd:"" help:"Delete a policy rule."`
	Permissions RulesPermissionsCmd `cmd:"" help:"Manage app-level tool permissions (supports per-agent overrides)."`
	Overlap     RulesOverlapCmd     `cmd:"" help:"Count custom rules overlapping an app's hosts."`
}

// RulesPermissionsCmd is `onecli rules permissions`.
type RulesPermissionsCmd struct {
	Get RulesPermissionsGetCmd `cmd:"" help:"Get layered tool permissions for a provider."`
	Set RulesPermissionsSetCmd `cmd:"" help:"Set tool permissions for a provider (optionally for one agent)."`
}

// RulesPermissionsGetCmd is `onecli rules permissions get`.
type RulesPermissionsGetCmd struct {
	Provider string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	AgentID  string `optional:"" name:"agent-id" help:"Show only this agent's override layer."`
	Fields   string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *RulesPermissionsGetCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	states, err := client.GetRulePermissions(newContext(), c.Provider)
	if err != nil {
		return err
	}
	if c.AgentID != "" {
		if err := validate.ResourceID(c.AgentID); err != nil {
			return fmt.Errorf("invalid agent-id: %w", err)
		}
		layer := states.ByAgent[c.AgentID]
		if layer == nil {
			layer = map[string]api.PermissionState{}
		}
		return out.WriteFiltered(map[string]any{
			"agentId":   c.AgentID,
			"overrides": layer,
			"defaults":  states.Defaults,
		}, c.Fields)
	}
	return out.WriteFiltered(states, c.Fields)
}

// RulesPermissionsSetCmd is `onecli rules permissions set`.
type RulesPermissionsSetCmd struct {
	Provider   string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	Tool       string `optional:"" help:"Tool ID to change (see 'onecli apps permission-definition'). Alternative to --json."`
	Permission string `optional:"" help:"Permission: 'allow', 'manual_approval', 'block', or 'inherit' (agent layer only)."`
	AgentID    string `optional:"" name:"agent-id" help:"Target one agent's override layer instead of the all-agents defaults."`
	Conditions string `optional:"" help:"Content conditions as a JSON array."`
	Json       string `optional:"" help:"Raw JSON payload with 'changes' array of {toolId, permission}. Overrides --tool/--permission/--conditions."`
	DryRun     bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *RulesPermissionsSetCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}

	var input api.SetPermissionsInput
	if c.Json != "" {
		if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	} else {
		if c.Tool == "" || c.Permission == "" {
			return fmt.Errorf("provide --tool and --permission, or a raw --json payload")
		}
		conditions, err := parseConditions(c.Conditions)
		if err != nil {
			return err
		}
		input.Changes = []api.PermissionChange{{ToolID: c.Tool, Permission: c.Permission}}
		for _, cond := range conditions {
			input.Conditions = append(input.Conditions, cond)
		}
	}
	if c.AgentID != "" {
		if err := validate.ResourceID(c.AgentID); err != nil {
			return fmt.Errorf("invalid agent-id: %w", err)
		}
		input.AgentID = c.AgentID
	}

	if len(input.Changes) == 0 {
		return fmt.Errorf("'changes' array must contain at least one entry")
	}
	for _, ch := range input.Changes {
		if ch.ToolID == "" {
			return fmt.Errorf("each change must have a non-empty 'toolId'")
		}
		if !validPermissionSettings[ch.Permission] {
			return fmt.Errorf("invalid permission %q for tool %q: must be 'allow', 'manual_approval', 'block', or 'inherit'", ch.Permission, ch.ToolID)
		}
		if ch.Permission == "inherit" && input.AgentID == "" {
			return fmt.Errorf("'inherit' removes an agent's override and requires --agent-id")
		}
	}

	if c.DryRun {
		return out.WriteDryRun("Would set rule permissions", map[string]any{"provider": c.Provider, "input": input})
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.SetRulePermissions(newContext(), c.Provider, input); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "updated", "provider": c.Provider})
}

// validPermissionSettings mirrors the server's project permission enum
// (org excludes "inherit" — enforced in the org command).
var validPermissionSettings = map[string]bool{
	"allow":           true,
	"manual_approval": true,
	"block":           true,
	"inherit":         true,
}

// RulesOverlapCmd is `onecli rules overlap`.
type RulesOverlapCmd struct {
	Provider string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
}

func (c *RulesOverlapCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	count, err := client.GetRuleOverlap(newContext(), c.Provider)
	if err != nil {
		return err
	}
	return out.Write(count)
}

// RulesListCmd is `onecli rules list`.
type RulesListCmd struct {
	Project string `optional:"" short:"p" help:"Project slug."`
	Fields  string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet   string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
	Max     int    `optional:"" default:"20" help:"Maximum number of results to return."`
}

func (c *RulesListCmd) Run(out *output.Writer) error {
	project, err := resolveProject(c.Project)
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	rules, err := client.ListRules(newContext(), project)
	if err != nil {
		return err
	}
	if c.Max > 0 && len(rules) > c.Max {
		rules = rules[:c.Max]
	}
	if c.Quiet != "" {
		return out.WriteQuiet(rules, c.Quiet)
	}
	return out.WriteFiltered(rules, c.Fields)
}

// RulesGetCmd is `onecli rules get`.
type RulesGetCmd struct {
	ID     string `required:"" help:"ID of the rule to retrieve."`
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *RulesGetCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid rule ID: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	rule, err := client.GetRule(newContext(), c.ID)
	if err != nil {
		return err
	}
	return out.WriteFiltered(rule, c.Fields)
}

// RulesCreateCmd is `onecli rules create`.
type RulesCreateCmd struct {
	Project         string `optional:"" short:"p" help:"Project slug."`
	Name            string `required:"" help:"Display name for the rule."`
	HostPattern     string `required:"" name:"host-pattern" help:"Host pattern to match (e.g. 'api.anthropic.com')."`
	Action          string `required:"" help:"Action to take: 'block', 'rate_limit', 'manual_approval', or 'allow'."`
	PathPattern     string `optional:"" name:"path-pattern" help:"Path pattern to match (e.g. '/v1/*')."`
	Method          string `optional:"" help:"HTTP method to match (GET, POST, PUT, PATCH, DELETE)."`
	AgentID         string `optional:"" name:"agent-id" help:"Agent ID to scope this rule to. Omit for all agents."`
	RateLimit       *int   `optional:"" name:"rate-limit" help:"Max requests per window (required for rate_limit action)."`
	RateLimitWindow string `optional:"" name:"rate-limit-window" help:"Time window: 'minute', 'hour', or 'day'."`
	Enabled         bool   `optional:"" default:"true" help:"Enable rule immediately."`
	Conditions      string `optional:"" help:"Content conditions as a JSON array, e.g. '[{\"target\":\"body\",\"operator\":\"contains\",\"value\":\"x\"}]'."`
	Json            string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun          bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *RulesCreateCmd) Run(out *output.Writer) error {
	var input api.CreateRuleInput
	if c.Json != "" {
		if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	} else {
		conditions, err := parseConditions(c.Conditions)
		if err != nil {
			return err
		}
		input = api.CreateRuleInput{
			Name:            c.Name,
			HostPattern:     c.HostPattern,
			PathPattern:     c.PathPattern,
			Method:          c.Method,
			Action:          c.Action,
			Enabled:         c.Enabled,
			AgentID:         c.AgentID,
			RateLimit:       c.RateLimit,
			RateLimitWindow: c.RateLimitWindow,
			Conditions:      conditions,
		}
	}

	if err := validateRuleInput(input.HostPattern, input.PathPattern, input.Method, input.AgentID, input.Action); err != nil {
		return err
	}

	if input.Action == "rate_limit" && (input.RateLimit == nil || input.RateLimitWindow == "") {
		return fmt.Errorf("--rate-limit and --rate-limit-window are required when action is 'rate_limit'")
	}

	if c.DryRun {
		return out.WriteDryRun("Would create rule", input)
	}

	project, err := resolveProject(c.Project)
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	rule, err := client.CreateRule(newContext(), project, input)
	if err != nil {
		return err
	}
	return out.Write(rule)
}

// RulesUpdateCmd is `onecli rules update`.
type RulesUpdateCmd struct {
	ID              string `required:"" help:"ID of the rule to update."`
	Name            string `optional:"" help:"New display name."`
	HostPattern     string `optional:"" name:"host-pattern" help:"New host pattern."`
	PathPattern     string `optional:"" name:"path-pattern" help:"New path pattern."`
	Method          string `optional:"" help:"New HTTP method."`
	Action          string `optional:"" help:"New action: 'block', 'rate_limit', 'manual_approval', or 'allow'."`
	Enabled         *bool  `optional:"" help:"Enable or disable the rule."`
	AgentID         string `optional:"" name:"agent-id" help:"New agent ID scope."`
	RateLimit       *int   `optional:"" name:"rate-limit" help:"New max requests per window."`
	RateLimitWindow string `optional:"" name:"rate-limit-window" help:"New time window."`
	Conditions      string `optional:"" help:"New content conditions as a JSON array; '[]' clears existing conditions."`
	Json            string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun          bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *RulesUpdateCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid rule ID: %w", err)
	}

	var input api.UpdateRuleInput
	if c.Json != "" {
		if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	} else {
		if c.Name != "" {
			input.Name = &c.Name
		}
		if c.HostPattern != "" {
			input.HostPattern = &c.HostPattern
		}
		if c.PathPattern != "" {
			input.PathPattern = &c.PathPattern
		}
		if c.Method != "" {
			input.Method = &c.Method
		}
		if c.Action != "" {
			input.Action = &c.Action
		}
		if c.Enabled != nil {
			input.Enabled = c.Enabled
		}
		if c.AgentID != "" {
			input.AgentID = &c.AgentID
		}
		if c.RateLimit != nil {
			input.RateLimit = c.RateLimit
		}
		if c.RateLimitWindow != "" {
			input.RateLimitWindow = &c.RateLimitWindow
		}
		if c.Conditions != "" {
			conditions, err := parseConditions(c.Conditions)
			if err != nil {
				return err
			}
			input.Conditions = &conditions
		}
	}

	var hostPattern, pathPattern, method, agentID, action string
	if input.HostPattern != nil {
		hostPattern = *input.HostPattern
	}
	if input.PathPattern != nil {
		pathPattern = *input.PathPattern
	}
	if input.Method != nil {
		method = *input.Method
	}
	if input.AgentID != nil {
		agentID = *input.AgentID
	}
	if input.Action != nil {
		action = *input.Action
	}
	if err := validateRuleInput(hostPattern, pathPattern, method, agentID, action); err != nil {
		return err
	}

	if c.DryRun {
		return out.WriteDryRun("Would update rule", map[string]any{"id": c.ID, "input": input})
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	rule, err := client.UpdateRule(newContext(), c.ID, input)
	if err != nil {
		return err
	}
	return out.Write(rule)
}

// RulesDeleteCmd is `onecli rules delete`.
type RulesDeleteCmd struct {
	ID     string `required:"" help:"ID of the rule to delete."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *RulesDeleteCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid rule ID: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would delete rule", map[string]string{"id": c.ID})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.DeleteRule(newContext(), c.ID); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "deleted", "id": c.ID})
}

// validHTTPMethods is the set of HTTP methods accepted for rule matching.
var validHTTPMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
}

// validateRuleInput validates shared fields across create and update commands.
// Empty strings are skipped (relevant for partial updates).
func validateRuleInput(hostPattern, pathPattern, method, agentID, action string) error {
	if hostPattern != "" {
		if err := validate.NoControlChars(hostPattern); err != nil {
			return fmt.Errorf("invalid host-pattern: %w", err)
		}
	}
	if pathPattern != "" {
		if err := validate.NoControlChars(pathPattern); err != nil {
			return fmt.Errorf("invalid path-pattern: %w", err)
		}
	}
	if method != "" {
		if !validHTTPMethods[method] {
			return fmt.Errorf("invalid method %q: must be one of GET, POST, PUT, PATCH, DELETE", method)
		}
	}
	if agentID != "" {
		if err := validate.ResourceID(agentID); err != nil {
			return fmt.Errorf("invalid agent-id: %w", err)
		}
	}
	if action != "" && !validRuleActions[action] {
		return fmt.Errorf("invalid action %q: must be one of 'block', 'rate_limit', 'manual_approval', 'allow'", action)
	}
	return nil
}

// validRuleActions mirrors the server's policy-rule action enum.
var validRuleActions = map[string]bool{
	"block":           true,
	"rate_limit":      true,
	"manual_approval": true,
	"allow":           true,
}

// parseConditions decodes a --conditions JSON array flag into rule conditions.
func parseConditions(raw string) ([]api.RuleCondition, error) {
	if raw == "" {
		return nil, nil
	}
	var conditions []api.RuleCondition
	if err := json.Unmarshal([]byte(raw), &conditions); err != nil {
		return nil, fmt.Errorf(`invalid --conditions (expected JSON array like [{"target":"body","operator":"contains","value":"x"}]): %w`, err)
	}
	return conditions, nil
}
