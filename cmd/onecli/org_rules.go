package main

import (
	"encoding/json"
	"fmt"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// OrgRulesCmd is the `onecli org rules` command group.
type OrgRulesCmd struct {
	List        OrgRulesListCmd        `cmd:"" help:"List all org-scoped policy rules."`
	Get         OrgRulesGetCmd         `cmd:"" help:"Get a single org-scoped policy rule by ID."`
	Create      OrgRulesCreateCmd      `cmd:"" help:"Create a new org-scoped policy rule."`
	Update      OrgRulesUpdateCmd      `cmd:"" help:"Update an org-scoped policy rule."`
	Delete      OrgRulesDeleteCmd      `cmd:"" help:"Delete an org-scoped policy rule."`
	Permissions OrgRulesPermissionsCmd `cmd:"" help:"Manage app-level tool permissions."`
}

// OrgRulesListCmd is `onecli org rules list`.
type OrgRulesListCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet  string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
	Max    int    `optional:"" default:"20" help:"Maximum number of results to return."`
}

func (c *OrgRulesListCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	rules, err := client.ListOrgRules(newContext())
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

// OrgRulesGetCmd is `onecli org rules get`.
type OrgRulesGetCmd struct {
	ID     string `required:"" help:"ID of the rule to retrieve."`
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *OrgRulesGetCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid rule ID: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	rule, err := client.GetOrgRule(newContext(), c.ID)
	if err != nil {
		return err
	}
	return out.WriteFiltered(rule, c.Fields)
}

// OrgRulesCreateCmd is `onecli org rules create`.
type OrgRulesCreateCmd struct {
	Name            string `required:"" help:"Display name for the rule."`
	HostPattern     string `required:"" name:"host-pattern" help:"Host pattern to match (e.g. 'api.anthropic.com')."`
	Action          string `required:"" help:"Action to take: 'block' or 'rate_limit'."`
	PathPattern     string `optional:"" name:"path-pattern" help:"Path pattern to match (e.g. '/v1/*')."`
	Method          string `optional:"" help:"HTTP method to match (GET, POST, PUT, PATCH, DELETE)."`
	AgentID         string `optional:"" name:"agent-id" help:"Agent ID to scope this rule to. Omit for all agents."`
	RateLimit       *int   `optional:"" name:"rate-limit" help:"Max requests per window (required for rate_limit action)."`
	RateLimitWindow string `optional:"" name:"rate-limit-window" help:"Time window: 'minute', 'hour', or 'day'."`
	Enabled         bool   `optional:"" default:"true" help:"Enable rule immediately."`
	Json            string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun          bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgRulesCreateCmd) Run(out *output.Writer) error {
	var input api.CreateRuleInput
	if c.Json != "" {
		if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	} else {
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
		}
	}

	if err := validateRuleInput(input.HostPattern, input.PathPattern, input.Method, input.AgentID, input.Action); err != nil {
		return err
	}

	if input.Action == "rate_limit" && (input.RateLimit == nil || input.RateLimitWindow == "") {
		return fmt.Errorf("--rate-limit and --rate-limit-window are required when action is 'rate_limit'")
	}

	if c.DryRun {
		return out.WriteDryRun("Would create org rule", input)
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	rule, err := client.CreateOrgRule(newContext(), input)
	if err != nil {
		return err
	}
	return out.Write(rule)
}

// OrgRulesUpdateCmd is `onecli org rules update`.
type OrgRulesUpdateCmd struct {
	ID              string `required:"" help:"ID of the rule to update."`
	Name            string `optional:"" help:"New display name."`
	HostPattern     string `optional:"" name:"host-pattern" help:"New host pattern."`
	PathPattern     string `optional:"" name:"path-pattern" help:"New path pattern."`
	Method          string `optional:"" help:"New HTTP method."`
	Action          string `optional:"" help:"New action: 'block' or 'rate_limit'."`
	Enabled         *bool  `optional:"" help:"Enable or disable the rule."`
	AgentID         string `optional:"" name:"agent-id" help:"New agent ID scope."`
	RateLimit       *int   `optional:"" name:"rate-limit" help:"New max requests per window."`
	RateLimitWindow string `optional:"" name:"rate-limit-window" help:"New time window."`
	Json            string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun          bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgRulesUpdateCmd) Run(out *output.Writer) error {
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
		return out.WriteDryRun("Would update org rule", map[string]any{"id": c.ID, "input": input})
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	rule, err := client.UpdateOrgRule(newContext(), c.ID, input)
	if err != nil {
		return err
	}
	return out.Write(rule)
}

// OrgRulesDeleteCmd is `onecli org rules delete`.
type OrgRulesDeleteCmd struct {
	ID     string `required:"" help:"ID of the rule to delete."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgRulesDeleteCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid rule ID: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would delete org rule", map[string]string{"id": c.ID})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.DeleteOrgRule(newContext(), c.ID); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "deleted", "id": c.ID})
}

// OrgRulesPermissionsCmd is `onecli org rules permissions`.
type OrgRulesPermissionsCmd struct {
	Get OrgRulesPermissionsGetCmd `cmd:"" help:"Get tool permissions for a provider."`
	Set OrgRulesPermissionsSetCmd `cmd:"" help:"Set tool permissions for a provider."`
}

// OrgRulesPermissionsGetCmd is `onecli org rules permissions get`.
type OrgRulesPermissionsGetCmd struct {
	Provider string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	Fields   string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *OrgRulesPermissionsGetCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	states, err := client.GetAppPermissions(newContext(), c.Provider)
	if err != nil {
		return err
	}
	return out.WriteFiltered(states, c.Fields)
}

// OrgRulesPermissionsSetCmd is `onecli org rules permissions set`.
type OrgRulesPermissionsSetCmd struct {
	Provider string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	Json     string `required:"" help:"JSON payload with 'changes' array of {toolId, permission} objects."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgRulesPermissionsSetCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}

	var input api.SetPermissionsInput
	if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
		return fmt.Errorf("invalid JSON payload: %w", err)
	}
	if len(input.Changes) == 0 {
		return fmt.Errorf("'changes' array must contain at least one entry")
	}
	for _, ch := range input.Changes {
		if ch.ToolID == "" {
			return fmt.Errorf("each change must have a non-empty 'toolId'")
		}
		if ch.Permission != "allow" && ch.Permission != "manual_approval" && ch.Permission != "block" {
			return fmt.Errorf("invalid permission %q for tool %q: must be 'allow', 'manual_approval', or 'block'", ch.Permission, ch.ToolID)
		}
	}

	if c.DryRun {
		return out.WriteDryRun("Would set app permissions", map[string]any{"provider": c.Provider, "input": input})
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.SetAppPermissions(newContext(), c.Provider, input); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "updated", "provider": c.Provider})
}
