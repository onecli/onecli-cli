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
	List   RulesListCmd   `cmd:"" help:"List all policy rules."`
	Get    RulesGetCmd    `cmd:"" help:"Get a single policy rule by ID."`
	Create RulesCreateCmd `cmd:"" help:"Create a new policy rule."`
	Update RulesUpdateCmd `cmd:"" help:"Update an existing policy rule."`
	Delete RulesDeleteCmd `cmd:"" help:"Delete a policy rule."`
}

// RulesListCmd is `onecli rules list`.
type RulesListCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet  string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
	Max    int    `optional:"" default:"20" help:"Maximum number of results to return."`
}

func (c *RulesListCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	rules, err := client.ListRules(newContext())
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

func (c *RulesCreateCmd) Run(out *output.Writer) error {
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
		return out.WriteDryRun("Would create rule", input)
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	rule, err := client.CreateRule(newContext(), input)
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
	Action          string `optional:"" help:"New action: 'block' or 'rate_limit'."`
	Enabled         *bool  `optional:"" help:"Enable or disable the rule."`
	AgentID         string `optional:"" name:"agent-id" help:"New agent ID scope."`
	RateLimit       *int   `optional:"" name:"rate-limit" help:"New max requests per window."`
	RateLimitWindow string `optional:"" name:"rate-limit-window" help:"New time window."`
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
	if action != "" && action != "block" && action != "rate_limit" {
		return fmt.Errorf("invalid action %q: must be 'block' or 'rate_limit'", action)
	}
	return nil
}
