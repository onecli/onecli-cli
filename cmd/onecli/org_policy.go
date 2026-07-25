package main

import (
	"encoding/json"
	"fmt"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// `onecli org policy` — the org-scoped mirror of the policy family. Requires
// an organization API key with the admin role; org rules take agent-group /
// user / user-group identities (never a specific agent — the server rejects
// those). Available on OneCLI Cloud and Enterprise self-hosted releases.

// OrgPolicyCmd is `onecli org policy`.
type OrgPolicyCmd struct {
	Rules                OrgPolicyRulesCmd                `cmd:"" help:"Manage the organization's policy rules (draft → publish)."`
	Default              OrgPolicyDefaultCmd              `cmd:"" help:"Show or set the organization's terminal Default Rule."`
	Publish              OrgPolicyPublishCmd              `cmd:"" help:"Publish the organization's staged draft (all staged changes)."`
	Status               OrgPolicyStatusCmd               `cmd:"" help:"Show staged changes and the last publish."`
	EffectivePermissions OrgPolicyEffectivePermissionsCmd `cmd:"" name:"effective-permissions" help:"Show what the published org policy allows for an app, per tool (read-only)."`
}

// OrgPolicyRulesCmd groups the org rule subcommands.
type OrgPolicyRulesCmd struct {
	List    OrgPolicyRulesListCmd    `cmd:"" help:"List org policy rules (draft by default; --status published = the enforced set)."`
	Get     OrgPolicyRulesGetCmd     `cmd:"" help:"Get one DRAFT org rule by id."`
	Create  OrgPolicyRulesCreateCmd  `cmd:"" help:"Create an org policy rule."`
	Update  OrgPolicyRulesUpdateCmd  `cmd:"" help:"Update a DRAFT org policy rule."`
	Delete  OrgPolicyRulesDeleteCmd  `cmd:"" help:"Delete a DRAFT org policy rule."`
	Reorder OrgPolicyRulesReorderCmd `cmd:"" help:"Reorder the org draft (full id permutation)."`
}

func orgScopeOps(client *api.Client) policyScopeOps {
	return policyScopeOps{
		diffCount: func() (int, error) {
			return stagedPolicyDiffCount(client, "", true)
		},
		publish: func() (*api.PolicyPublishResult, error) {
			return client.PublishOrgPolicy(newContext())
		},
		statusCmd:   "onecli org policy status",
		publishCmd:  "onecli org policy publish",
		publishFlag: "--publish-all",
	}
}

// OrgPolicyRulesListCmd is `onecli org policy rules list`.
type OrgPolicyRulesListCmd struct {
	Status string `optional:"" default:"draft" help:"Rule set: 'draft' (working copy) or 'published' (enforced)."`
	Fields string `optional:"" help:"Comma-separated fields to include."`
	Quiet  string `optional:"" name:"quiet" help:"Output only the specified field, one per line (e.g. 'id')."`
	Max    int    `optional:"" default:"0" help:"Max rules to output (0 = all — reorder needs the complete list)."`
}

func (c *OrgPolicyRulesListCmd) Run(out *output.Writer) error {
	if c.Status != "draft" && c.Status != "published" {
		return fmt.Errorf("invalid --status %q: must be 'draft' or 'published'", c.Status)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	rules, err := client.ListOrgPolicyRules(newContext(), c.Status)
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

// OrgPolicyRulesGetCmd is `onecli org policy rules get`.
type OrgPolicyRulesGetCmd struct {
	ID     string `required:"" help:"DRAFT rule id (published ids regenerate every publish — match by logicalId)."`
	Fields string `optional:"" help:"Comma-separated fields to include."`
}

func (c *OrgPolicyRulesGetCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid rule id: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	rule, err := client.GetOrgPolicyRule(newContext(), c.ID)
	if err != nil {
		return err
	}
	return out.WriteFiltered(rule, c.Fields)
}

// OrgPolicyRulesCreateCmd is `onecli org policy rules create`.
type OrgPolicyRulesCreateCmd struct {
	Name            string `optional:"" help:"Display name (required unless --json)."`
	Action          string `optional:"" help:"'allow' or 'block' (required unless --json)."`
	Targets         string `optional:"" help:"JSON array of targets (required unless --json)."`
	Identities      string `optional:"" help:"JSON array of identities — org rules take agentGroup/user/group kinds, e.g. '[{\"type\":\"group\",\"id\":\"<group-id>\"}]'."`
	Description     string `optional:"" help:"Rule description."`
	Enabled         *bool  `optional:"" help:"Whether the rule is enabled (default true)."`
	RateLimit       *int   `optional:"" name:"rate-limit" help:"Max requests per window (allow rules only)."`
	RateLimitWindow string `optional:"" name:"rate-limit-window" help:"'minute', 'hour', or 'day'."`
	RequireApproval *bool  `optional:"" name:"require-approval" help:"Require manual approval (allow rules only)."`
	Conditions      string `optional:"" help:"JSON conditions array or session-policy object."`
	Json            string `optional:"" help:"Raw JSON payload. Do not combine with field flags."`
	NoPublish       bool   `optional:"" name:"no-publish" help:"Stage only; do not publish."`
	PublishAll      bool   `optional:"" name:"publish-all" help:"Publish even when the draft holds other staged changes."`
	DryRun          bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgPolicyRulesCreateCmd) Run(out *output.Writer) error {
	plan := policyPublishPlan{NoPublish: c.NoPublish, PublishAll: c.PublishAll}
	if err := plan.validate(); err != nil {
		return err
	}
	input, err := buildCreatePolicyInput(
		c.Json, c.Name, c.Action, c.Description, c.Targets, c.Identities,
		c.Conditions, c.Enabled, c.RateLimit, c.RateLimitWindow, c.RequireApproval,
	)
	if err != nil {
		return err
	}
	if c.DryRun {
		return out.WriteDryRun("Would create org policy rule (draft; publish per --no-publish/--publish-all)", input)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	ops := orgScopeOps(client)
	pending, err := prePublishPending(plan, ops)
	if err != nil {
		return err
	}
	rule, err := client.CreateOrgPolicyRule(newContext(), input)
	if err != nil {
		return err
	}
	return finishPolicyWrite(out, policyWriteOutcome{Rule: rule}, plan, pending, ops)
}

// OrgPolicyRulesUpdateCmd is `onecli org policy rules update`.
type OrgPolicyRulesUpdateCmd struct {
	ID              string `required:"" help:"DRAFT rule id."`
	Name            string `optional:"" help:"New display name."`
	Action          string `optional:"" help:"'allow' or 'block'."`
	Targets         string `optional:"" help:"JSON array replacing the rule's targets."`
	Identities      string `optional:"" help:"JSON array replacing the rule's identities ('[]' = everyone)."`
	Description     string `optional:"" help:"New description."`
	Enabled         *bool  `optional:"" help:"Enable or disable the rule."`
	RateLimit       *int   `optional:"" name:"rate-limit" help:"New rate limit (pair with --rate-limit-window)."`
	RateLimitWindow string `optional:"" name:"rate-limit-window" help:"'minute', 'hour', or 'day'."`
	RequireApproval *bool  `optional:"" name:"require-approval" help:"Require manual approval."`
	Conditions      string `optional:"" help:"JSON conditions; 'null' clears."`
	Json            string `optional:"" help:"Raw JSON PATCH payload, sent verbatim (an explicit JSON null clears a nullable field, e.g. '{\"rateLimit\":null}'). Do not combine with field flags."`
	NoPublish       bool   `optional:"" name:"no-publish" help:"Stage only; do not publish."`
	PublishAll      bool   `optional:"" name:"publish-all" help:"Publish even when the draft holds other staged changes."`
	DryRun          bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgPolicyRulesUpdateCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid rule id: %w", err)
	}
	plan := policyPublishPlan{NoPublish: c.NoPublish, PublishAll: c.PublishAll}
	if err := plan.validate(); err != nil {
		return err
	}
	proxy := PolicyRulesUpdateCmd{
		ID: c.ID, Name: c.Name, Action: c.Action, Targets: c.Targets,
		Identities: c.Identities, Description: c.Description, Enabled: c.Enabled,
		RateLimit: c.RateLimit, RateLimitWindow: c.RateLimitWindow,
		RequireApproval: c.RequireApproval, Conditions: c.Conditions, Json: c.Json,
	}
	input, err := proxy.buildInput()
	if err != nil {
		return err
	}
	if c.DryRun {
		return out.WriteDryRun("Would update org policy rule "+c.ID, input)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	ops := orgScopeOps(client)
	pending, err := prePublishPending(plan, ops)
	if err != nil {
		return err
	}
	rule, err := client.UpdateOrgPolicyRule(newContext(), c.ID, input)
	if err != nil {
		return err
	}
	return finishPolicyWrite(out, policyWriteOutcome{Rule: rule}, plan, pending, ops)
}

// OrgPolicyRulesDeleteCmd is `onecli org policy rules delete`.
type OrgPolicyRulesDeleteCmd struct {
	ID         string `required:"" help:"DRAFT rule id."`
	NoPublish  bool   `optional:"" name:"no-publish" help:"Stage only; do not publish."`
	PublishAll bool   `optional:"" name:"publish-all" help:"Publish even when the draft holds other staged changes."`
	DryRun     bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgPolicyRulesDeleteCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid rule id: %w", err)
	}
	plan := policyPublishPlan{NoPublish: c.NoPublish, PublishAll: c.PublishAll}
	if err := plan.validate(); err != nil {
		return err
	}
	if c.DryRun {
		return out.WriteDryRun("Would delete org policy rule "+c.ID, map[string]string{"id": c.ID})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	ops := orgScopeOps(client)
	pending, err := prePublishPending(plan, ops)
	if err != nil {
		return err
	}
	if err := client.DeleteOrgPolicyRule(newContext(), c.ID); err != nil {
		return err
	}
	return finishPolicyWrite(out, policyWriteOutcome{DeletedID: c.ID}, plan, pending, ops)
}

// OrgPolicyRulesReorderCmd is `onecli org policy rules reorder`.
type OrgPolicyRulesReorderCmd struct {
	OrderedIds string `required:"" name:"ordered-ids" help:"JSON array of EVERY non-default draft rule id exactly once (get the full list from 'org policy rules list --quiet id'). The server rejects a partial list."`
	NoPublish  bool   `optional:"" name:"no-publish" help:"Stage only; do not publish."`
	PublishAll bool   `optional:"" name:"publish-all" help:"Publish even when the draft holds other staged changes."`
	DryRun     bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgPolicyRulesReorderCmd) Run(out *output.Writer) error {
	var ids []string
	if err := json.Unmarshal([]byte(c.OrderedIds), &ids); err != nil {
		return fmt.Errorf("invalid --ordered-ids JSON (expected an array of rule ids): %w", err)
	}
	if len(ids) == 0 {
		return fmt.Errorf("--ordered-ids must name every non-default draft rule")
	}
	plan := policyPublishPlan{NoPublish: c.NoPublish, PublishAll: c.PublishAll}
	if err := plan.validate(); err != nil {
		return err
	}
	if c.DryRun {
		return out.WriteDryRun("Would reorder org policy rules", map[string]any{"orderedIds": ids})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	ops := orgScopeOps(client)
	pending, err := prePublishPending(plan, ops)
	if err != nil {
		return err
	}
	rules, err := client.ReorderOrgPolicyRules(newContext(), ids)
	if err != nil {
		return err
	}
	return finishPolicyWrite(out, policyWriteOutcome{Rules: rules}, plan, pending, ops)
}

// OrgPolicyDefaultCmd groups the org Default Rule subcommands.
type OrgPolicyDefaultCmd struct {
	Get OrgPolicyDefaultGetCmd `cmd:"" help:"Show the organization's terminal Default Rule."`
	Set OrgPolicyDefaultSetCmd `cmd:"" help:"Set the org Default Rule's action."`
}

// OrgPolicyDefaultGetCmd is `onecli org policy default get`.
type OrgPolicyDefaultGetCmd struct {
	Status string `optional:"" default:"draft" help:"'draft' or 'published'."`
	Fields string `optional:"" help:"Comma-separated fields to include."`
}

func (c *OrgPolicyDefaultGetCmd) Run(out *output.Writer) error {
	if c.Status != "draft" && c.Status != "published" {
		return fmt.Errorf("invalid --status %q: must be 'draft' or 'published'", c.Status)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	rule, err := client.GetOrgPolicyDefault(newContext(), c.Status)
	if err != nil {
		return err
	}
	return out.WriteFiltered(rule, c.Fields)
}

// OrgPolicyDefaultSetCmd is `onecli org policy default set`.
type OrgPolicyDefaultSetCmd struct {
	Action     string `required:"" help:"'allow' or 'block'."`
	NoPublish  bool   `optional:"" name:"no-publish" help:"Stage only; do not publish."`
	PublishAll bool   `optional:"" name:"publish-all" help:"Publish even when the draft holds other staged changes."`
	DryRun     bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgPolicyDefaultSetCmd) Run(out *output.Writer) error {
	if c.Action != "allow" && c.Action != "block" {
		return fmt.Errorf("invalid action %q: must be 'allow' or 'block'", c.Action)
	}
	plan := policyPublishPlan{NoPublish: c.NoPublish, PublishAll: c.PublishAll}
	if err := plan.validate(); err != nil {
		return err
	}
	if c.DryRun {
		return out.WriteDryRun("Would set the org Default Rule action", map[string]string{"action": c.Action})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	ops := orgScopeOps(client)
	pending, err := prePublishPending(plan, ops)
	if err != nil {
		return err
	}
	rule, err := client.SetOrgPolicyDefault(newContext(), c.Action)
	if err != nil {
		return err
	}
	return finishPolicyWrite(out, policyWriteOutcome{Rule: rule}, plan, pending, ops)
}

// OrgPolicyPublishCmd is `onecli org policy publish`.
type OrgPolicyPublishCmd struct {
	DryRun bool `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgPolicyPublishCmd) Run(out *output.Writer) error {
	if c.DryRun {
		return out.WriteDryRun("Would publish the organization's whole policy draft", struct{}{})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	result, err := client.PublishOrgPolicy(newContext())
	if err != nil {
		return err
	}
	return out.Write(result)
}

// OrgPolicyStatusCmd is `onecli org policy status`.
type OrgPolicyStatusCmd struct {
	Fields string `optional:"" help:"Comma-separated fields to include."`
}

func (c *OrgPolicyStatusCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	ctx := newContext()
	draft, err := client.ListOrgPolicyRules(ctx, "draft")
	if err != nil {
		return err
	}
	published, err := client.ListOrgPolicyRules(ctx, "published")
	if err != nil {
		return err
	}
	dDef, err := client.GetOrgPolicyDefault(ctx, "draft")
	if err != nil {
		return err
	}
	pDef, err := client.GetOrgPolicyDefault(ctx, "published")
	if err != nil {
		return err
	}
	last, err := client.GetOrgPolicyLastPublish(ctx)
	if err != nil {
		return err
	}
	diff := api.DiffPolicyChanges(draft, published, dDef, pDef)
	return out.WriteFiltered(policyStatus{
		LastPublish:    last,
		PendingChanges: diff.Count,
		Diff:           diff,
	}, c.Fields)
}
