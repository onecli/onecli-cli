package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// The /v1/policy command family — the first-match policy engine's staged
// model (rules land in a DRAFT; a publish snapshots the whole scope draft
// into the enforced generation). Writes AUTO-PUBLISH when the scope has no
// other staged changes; pre-existing staged changes withhold the publish
// (--publish-all overrides, --no-publish always stages). The legacy `rules`
// commands keep working against pre-cutover self-hosted servers; cloud
// deployments accept only this family for writes.

// PolicyCmd is `onecli policy` (project scope).
type PolicyCmd struct {
	Rules                PolicyRulesCmd                `cmd:"" help:"Manage the project's policy rules (draft → publish)."`
	Default              PolicyDefaultCmd              `cmd:"" help:"Show or set the project's terminal Default Rule."`
	Publish              PolicyPublishCmd              `cmd:"" help:"Publish the project's staged draft (all staged changes)."`
	Status               PolicyStatusCmd               `cmd:"" help:"Show staged changes and the last publish."`
	EffectivePermissions PolicyEffectivePermissionsCmd `cmd:"" name:"effective-permissions" help:"Show what the published policy allows for an app, per tool (read-only)."`
}

// PolicyRulesCmd groups the rule subcommands.
type PolicyRulesCmd struct {
	List    PolicyRulesListCmd    `cmd:"" help:"List policy rules (draft by default; --status published = the enforced set)."`
	Get     PolicyRulesGetCmd     `cmd:"" help:"Get one DRAFT rule by id."`
	Create  PolicyRulesCreateCmd  `cmd:"" help:"Create a policy rule."`
	Update  PolicyRulesUpdateCmd  `cmd:"" help:"Update a DRAFT policy rule."`
	Delete  PolicyRulesDeleteCmd  `cmd:"" help:"Delete a DRAFT policy rule."`
	Reorder PolicyRulesReorderCmd `cmd:"" help:"Reorder the draft (full id permutation)."`
}

// ── Shared write/publish plumbing (used by the org variants too) ────────────

// policyWriteOutcome is the JSON envelope every policy write emits: the
// written entity plus the publish outcome.
type policyWriteOutcome struct {
	Rule           *api.PolicyRule  `json:"rule,omitempty"`
	Rules          []api.PolicyRule `json:"rules,omitempty"`
	DeletedID      string           `json:"deletedId,omitempty"`
	Published      bool             `json:"published"`
	Generation     *int             `json:"generation,omitempty"`
	PublishSkipped string           `json:"publishSkipped,omitempty"`
	PendingChanges *int             `json:"pendingChanges,omitempty"`
}

// policyPublishPlan carries the publish flags shared by every write command.
type policyPublishPlan struct {
	NoPublish  bool
	PublishAll bool
}

// validate rejects the contradictory flag pair loudly (agents hallucinate
// flag combinations; silence would hide which one won).
func (p policyPublishPlan) validate() error {
	if p.NoPublish && p.PublishAll {
		return fmt.Errorf("--no-publish and --publish-all are contradictory — pass at most one")
	}
	return nil
}

// policyScopeOps abstracts project vs org so the guard/publish flow is
// written once.
type policyScopeOps struct {
	diffCount   func() (int, error)
	publish     func() (*api.PolicyPublishResult, error)
	statusCmd   string
	publishCmd  string
	publishFlag string
}

func projectScopeOps(client *api.Client, project string) policyScopeOps {
	return policyScopeOps{
		diffCount: func() (int, error) {
			return stagedPolicyDiffCount(client, project, false)
		},
		publish: func() (*api.PolicyPublishResult, error) {
			return client.PublishPolicy(newContext(), project)
		},
		statusCmd:   "onecli policy status",
		publishCmd:  "onecli policy publish",
		publishFlag: "--publish-all",
	}
}

// stagedPolicyDiffCount composes the staged diff the way the server's web
// console does (no aggregate endpoint exists): draft + published rules,
// draft + published default, diffed client-side.
func stagedPolicyDiffCount(client *api.Client, project string, org bool) (int, error) {
	ctx := newContext()
	var (
		draft, published []api.PolicyRule
		dDef, pDef       *api.PolicyRule
		err              error
	)
	if org {
		if draft, err = client.ListOrgPolicyRules(ctx, "draft"); err != nil {
			return 0, err
		}
		if published, err = client.ListOrgPolicyRules(ctx, "published"); err != nil {
			return 0, err
		}
		if dDef, err = client.GetOrgPolicyDefault(ctx, "draft"); err != nil {
			return 0, err
		}
		if pDef, err = client.GetOrgPolicyDefault(ctx, "published"); err != nil {
			return 0, err
		}
	} else {
		if draft, err = client.ListPolicyRules(ctx, project, "draft"); err != nil {
			return 0, err
		}
		if published, err = client.ListPolicyRules(ctx, project, "published"); err != nil {
			return 0, err
		}
		if dDef, err = client.GetPolicyDefault(ctx, project, "draft"); err != nil {
			return 0, err
		}
		if pDef, err = client.GetPolicyDefault(ctx, project, "published"); err != nil {
			return 0, err
		}
	}
	diff := api.DiffPolicyChanges(draft, published, dDef, pDef)
	return diff.Count, nil
}

// prePublishPending returns the scope's staged-change count BEFORE a write
// (the write itself would contaminate the count), or -1 when the check is
// skipped (--no-publish / --publish-all make the count irrelevant).
func prePublishPending(plan policyPublishPlan, ops policyScopeOps) (int, error) {
	if err := plan.validate(); err != nil {
		return 0, err
	}
	if plan.NoPublish || plan.PublishAll {
		return -1, nil
	}
	return ops.diffCount()
}

// finishPolicyWrite completes a successful write per the publish plan:
// stage-only, withheld (other staged changes exist), or auto-publish. The
// publish snapshots the WHOLE scope draft, so pre-existing staged changes
// withhold it unless --publish-all consents.
func finishPolicyWrite(
	out *output.Writer,
	outcome policyWriteOutcome,
	plan policyPublishPlan,
	pending int,
	ops policyScopeOps,
) error {
	if plan.NoPublish {
		if err := out.Write(outcome); err != nil {
			return err
		}
		out.Stderr(fmt.Sprintf("Staged (not enforced). Run '%s' to publish.", ops.publishCmd))
		return nil
	}
	if !plan.PublishAll && pending > 0 {
		outcome.PublishSkipped = "draft-has-other-staged-changes"
		outcome.PendingChanges = &pending
		if err := out.Write(outcome); err != nil {
			return err
		}
		out.Stderr(fmt.Sprintf(
			"Publish withheld: the draft already has %d other staged change(s). Review with '%s', publish everything with '%s', or pass %s.",
			pending, ops.statusCmd, ops.publishCmd, ops.publishFlag,
		))
		return nil
	}
	result, err := ops.publish()
	if err != nil {
		if writeErr := out.Write(outcome); writeErr != nil {
			return writeErr
		}
		return fmt.Errorf(
			"publish failed: %s — the change IS staged; review with '%s', retry with '%s' (the failure may involve rules staged by others)",
			err.Error(), ops.statusCmd, ops.publishCmd,
		)
	}
	outcome.Published = true
	outcome.Generation = &result.Generation
	return out.Write(outcome)
}

// requireProjectForOrgKey fails fast when an org key is used for a
// project-scope policy command without a project selection — the server
// would reject with a misleading auth error otherwise.
func requireProjectForOrgKey(project string) error {
	if project != "" {
		return nil
	}
	if strings.HasPrefix(loadStoredAPIKey(), "oc_org_") {
		return fmt.Errorf(
			"an organization API key needs an explicit project for 'onecli policy' — pass --project <slug>, set ONECLI_PROJECT, or run 'onecli config set project <slug>'",
		)
	}
	return nil
}

// parsePolicyTargets parses the --targets JSON array (syntax only — the
// server owns semantic validation).
func parsePolicyTargets(raw string) ([]api.PolicyTarget, error) {
	if raw == "" {
		return nil, nil
	}
	var targets []api.PolicyTarget
	if err := json.Unmarshal([]byte(raw), &targets); err != nil {
		return nil, fmt.Errorf("invalid --targets JSON (expected an array of target objects): %w", err)
	}
	return targets, nil
}

// parsePolicyIdentities parses the --identities JSON array.
func parsePolicyIdentities(raw string) ([]api.PolicyIdentity, error) {
	if raw == "" {
		return nil, nil
	}
	var identities []api.PolicyIdentity
	if err := json.Unmarshal([]byte(raw), &identities); err != nil {
		return nil, fmt.Errorf("invalid --identities JSON (expected an array of {type,id} objects): %w", err)
	}
	return identities, nil
}

// parsePolicyConditions parses --conditions into a raw message: a JSON array
// (behavioral conditions), an object (session policy), or the literal 'null'
// (update only: clears). Syntax-checked only.
func parsePolicyConditions(raw string) (*json.RawMessage, error) {
	if raw == "" {
		return nil, nil
	}
	if !json.Valid([]byte(raw)) {
		return nil, fmt.Errorf("invalid --conditions JSON")
	}
	msg := json.RawMessage(raw)
	return &msg, nil
}

// validatePolicyAction rejects anything but the v2 actions with a pointer at
// the modifier flags that replaced the legacy actions.
func validatePolicyAction(action string) error {
	switch action {
	case "allow", "block":
		return nil
	case "rate_limit", "manual_approval":
		return fmt.Errorf(
			"action %q is a legacy action — in the policy engine use action 'allow' with --rate-limit/--rate-limit-window or --require-approval",
			action,
		)
	default:
		return fmt.Errorf("invalid action %q: must be 'allow' or 'block'", action)
	}
}

// buildCreatePolicyInput assembles the create payload from --json XOR the
// field flags (combining them is rejected — unlike the legacy commands'
// silent override — so a hallucinated mixed invocation fails loudly).
func buildCreatePolicyInput(
	rawJSON, name, action, description, targetsRaw, identitiesRaw, conditionsRaw string,
	enabled *bool, rateLimit *int, rateLimitWindow string, requireApproval *bool,
) (api.CreatePolicyRuleInput, error) {
	var input api.CreatePolicyRuleInput
	fieldFlagsUsed := name != "" || action != "" || description != "" ||
		targetsRaw != "" || identitiesRaw != "" || conditionsRaw != "" ||
		enabled != nil || rateLimit != nil || rateLimitWindow != "" || requireApproval != nil
	if rawJSON != "" {
		if fieldFlagsUsed {
			return input, fmt.Errorf("--json is the full payload — do not combine it with field flags")
		}
		if err := json.Unmarshal([]byte(rawJSON), &input); err != nil {
			return input, fmt.Errorf("invalid --json payload: %w", err)
		}
	} else {
		if name == "" || action == "" || targetsRaw == "" {
			return input, fmt.Errorf("--name, --action, and --targets are required (or pass the full payload via --json)")
		}
		targets, err := parsePolicyTargets(targetsRaw)
		if err != nil {
			return input, err
		}
		identities, err := parsePolicyIdentities(identitiesRaw)
		if err != nil {
			return input, err
		}
		conditions, err := parsePolicyConditions(conditionsRaw)
		if err != nil {
			return input, err
		}
		input = api.CreatePolicyRuleInput{
			Name:            name,
			Description:     description,
			Enabled:         enabled,
			Action:          action,
			RateLimit:       rateLimit,
			RateLimitWindow: rateLimitWindow,
			RequireApproval: requireApproval,
			Conditions:      conditions,
			Identities:      identities,
			Targets:         targets,
		}
	}
	if err := validatePolicyAction(input.Action); err != nil {
		return input, err
	}
	return input, nil
}

// ── Reads ───────────────────────────────────────────────────────────────────

// PolicyRulesListCmd is `onecli policy rules list`.
type PolicyRulesListCmd struct {
	Project string `optional:"" short:"p" help:"Project slug."`
	Status  string `optional:"" default:"draft" help:"Rule set: 'draft' (working copy) or 'published' (enforced)."`
	Fields  string `optional:"" help:"Comma-separated fields to include."`
	Quiet   string `optional:"" name:"quiet" help:"Output only the specified field, one per line (e.g. 'id')."`
	Max     int    `optional:"" default:"0" help:"Max rules to output (0 = all — reorder needs the complete list)."`
}

func (c *PolicyRulesListCmd) Run(out *output.Writer) error {
	if c.Status != "draft" && c.Status != "published" {
		return fmt.Errorf("invalid --status %q: must be 'draft' or 'published'", c.Status)
	}
	project, err := resolveProject(c.Project)
	if err != nil {
		return err
	}
	if err := requireProjectForOrgKey(project); err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	rules, err := client.ListPolicyRules(newContext(), project, c.Status)
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

// PolicyRulesGetCmd is `onecli policy rules get`.
type PolicyRulesGetCmd struct {
	Project string `optional:"" short:"p" help:"Project slug."`
	ID      string `required:"" help:"DRAFT rule id (published row ids regenerate every publish — match published rules by logicalId via 'rules list --status published')."`
	Fields  string `optional:"" help:"Comma-separated fields to include."`
}

func (c *PolicyRulesGetCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid rule id: %w", err)
	}
	project, err := resolveProject(c.Project)
	if err != nil {
		return err
	}
	if err := requireProjectForOrgKey(project); err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	rule, err := client.GetPolicyRule(newContext(), project, c.ID)
	if err != nil {
		return err
	}
	return out.WriteFiltered(rule, c.Fields)
}

// ── Writes ──────────────────────────────────────────────────────────────────

// PolicyRulesCreateCmd is `onecli policy rules create`.
type PolicyRulesCreateCmd struct {
	Project         string `optional:"" short:"p" help:"Project slug."`
	Name            string `optional:"" help:"Display name (required unless --json)."`
	Action          string `optional:"" help:"'allow' or 'block' (required unless --json). Legacy 'rate_limit'/'manual_approval' become allow + modifiers."`
	Targets         string `optional:"" help:"JSON array of targets (required unless --json), e.g. '[{\"kind\":\"app\",\"provider\":\"gmail\",\"tools\":[\"send_email\"]}]' or '[{\"kind\":\"network\",\"hostPattern\":\"api.example.com\"}]'."`
	Identities      string `optional:"" help:"JSON array of identities, e.g. '[{\"type\":\"agent\",\"id\":\"<agent-id>\"}]'. Omit for all agents."`
	Description     string `optional:"" help:"Rule description."`
	Enabled         *bool  `optional:"" help:"Whether the rule is enabled (default true)."`
	RateLimit       *int   `optional:"" name:"rate-limit" help:"Max requests per window (allow rules only; pair with --rate-limit-window)."`
	RateLimitWindow string `optional:"" name:"rate-limit-window" help:"'minute', 'hour', or 'day'."`
	RequireApproval *bool  `optional:"" name:"require-approval" help:"Require manual approval (allow rules only)."`
	Conditions      string `optional:"" help:"JSON: a conditions array, or a session-policy object ({\"repositories\":[…]} / {\"folders\":[…]})."`
	Json            string `optional:"" help:"Raw JSON payload for the full rule. Do not combine with field flags."`
	NoPublish       bool   `optional:"" name:"no-publish" help:"Stage only; do not publish."`
	PublishAll      bool   `optional:"" name:"publish-all" help:"Publish even when the draft holds other staged changes."`
	DryRun          bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *PolicyRulesCreateCmd) Run(out *output.Writer) error {
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
		return out.WriteDryRun("Would create policy rule (draft; publish per --no-publish/--publish-all)", input)
	}
	project, err := resolveProject(c.Project)
	if err != nil {
		return err
	}
	if err := requireProjectForOrgKey(project); err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	ops := projectScopeOps(client, project)
	pending, err := prePublishPending(plan, ops)
	if err != nil {
		return err
	}
	rule, err := client.CreatePolicyRule(newContext(), project, input)
	if err != nil {
		return err
	}
	return finishPolicyWrite(out, policyWriteOutcome{Rule: rule}, plan, pending, ops)
}

// PolicyRulesUpdateCmd is `onecli policy rules update`. Field flags follow
// the legacy update convention: a non-empty value sets the field; omitted
// flags leave it unchanged; --conditions accepts the literal 'null' to clear.
type PolicyRulesUpdateCmd struct {
	Project         string `optional:"" short:"p" help:"Project slug."`
	ID              string `required:"" help:"DRAFT rule id."`
	Name            string `optional:"" help:"New display name."`
	Action          string `optional:"" help:"'allow' or 'block'."`
	Targets         string `optional:"" help:"JSON array replacing the rule's targets."`
	Identities      string `optional:"" help:"JSON array replacing the rule's identities ('[]' = all agents)."`
	Description     string `optional:"" help:"New description."`
	Enabled         *bool  `optional:"" help:"Enable or disable the rule."`
	RateLimit       *int   `optional:"" name:"rate-limit" help:"New rate limit (pair with --rate-limit-window)."`
	RateLimitWindow string `optional:"" name:"rate-limit-window" help:"'minute', 'hour', or 'day'."`
	RequireApproval *bool  `optional:"" name:"require-approval" help:"Require manual approval."`
	Conditions      string `optional:"" help:"JSON conditions (array or session-policy object); 'null' clears."`
	Json            string `optional:"" help:"Raw JSON PATCH payload, sent verbatim (an explicit JSON null clears a nullable field, e.g. '{\"rateLimit\":null}'). Do not combine with field flags."`
	NoPublish       bool   `optional:"" name:"no-publish" help:"Stage only; do not publish."`
	PublishAll      bool   `optional:"" name:"publish-all" help:"Publish even when the draft holds other staged changes."`
	DryRun          bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

// buildInput assembles the PATCH payload (same XOR contract as create). The
// --json path returns the payload VERBATIM as json.RawMessage — round-tripping
// it through the typed pointer struct would silently drop explicit JSON nulls
// (the only way to CLEAR rateLimit/conditions/… server-side) and unknown
// fields; it is probed for validation only. The flag path builds the typed
// struct (flags can set fields, never clear them — except --conditions null).
func (c *PolicyRulesUpdateCmd) buildInput() (any, error) {
	var input api.UpdatePolicyRuleInput
	fieldFlagsUsed := c.Name != "" || c.Action != "" || c.Description != "" ||
		c.Targets != "" || c.Identities != "" || c.Conditions != "" ||
		c.Enabled != nil || c.RateLimit != nil || c.RateLimitWindow != "" ||
		c.RequireApproval != nil
	if c.Json != "" {
		if fieldFlagsUsed {
			return nil, fmt.Errorf("--json is the full payload — do not combine it with field flags")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(c.Json), &fields); err != nil {
			return nil, fmt.Errorf("invalid --json payload (expected a JSON object): %w", err)
		}
		if len(fields) == 0 {
			return nil, fmt.Errorf("--json must set at least one field")
		}
		var probe api.UpdatePolicyRuleInput
		if err := json.Unmarshal([]byte(c.Json), &probe); err != nil {
			return nil, fmt.Errorf("invalid --json payload: %w", err)
		}
		if probe.Action != nil {
			if err := validatePolicyAction(*probe.Action); err != nil {
				return nil, err
			}
		}
		return json.RawMessage(c.Json), nil
	}
	if !fieldFlagsUsed {
		return input, fmt.Errorf("provide at least one field flag or --json")
	}
	if c.Name != "" {
		input.Name = &c.Name
	}
	if c.Action != "" {
		if err := validatePolicyAction(c.Action); err != nil {
			return input, err
		}
		input.Action = &c.Action
	}
	if c.Description != "" {
		input.Description = &c.Description
	}
	input.Enabled = c.Enabled
	input.RateLimit = c.RateLimit
	if c.RateLimitWindow != "" {
		input.RateLimitWindow = &c.RateLimitWindow
	}
	input.RequireApproval = c.RequireApproval
	if c.Targets != "" {
		targets, err := parsePolicyTargets(c.Targets)
		if err != nil {
			return input, err
		}
		input.Targets = &targets
	}
	if c.Identities != "" {
		identities, err := parsePolicyIdentities(c.Identities)
		if err != nil {
			return input, err
		}
		input.Identities = &identities
	}
	if c.Conditions != "" {
		conditions, err := parsePolicyConditions(c.Conditions)
		if err != nil {
			return input, err
		}
		input.Conditions = conditions
	}
	return input, nil
}

func (c *PolicyRulesUpdateCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid rule id: %w", err)
	}
	plan := policyPublishPlan{NoPublish: c.NoPublish, PublishAll: c.PublishAll}
	if err := plan.validate(); err != nil {
		return err
	}
	input, err := c.buildInput()
	if err != nil {
		return err
	}
	if c.DryRun {
		return out.WriteDryRun("Would update policy rule "+c.ID, input)
	}
	project, err := resolveProject(c.Project)
	if err != nil {
		return err
	}
	if err := requireProjectForOrgKey(project); err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	ops := projectScopeOps(client, project)
	pending, err := prePublishPending(plan, ops)
	if err != nil {
		return err
	}
	rule, err := client.UpdatePolicyRule(newContext(), project, c.ID, input)
	if err != nil {
		return err
	}
	return finishPolicyWrite(out, policyWriteOutcome{Rule: rule}, plan, pending, ops)
}

// PolicyRulesDeleteCmd is `onecli policy rules delete`.
type PolicyRulesDeleteCmd struct {
	Project    string `optional:"" short:"p" help:"Project slug."`
	ID         string `required:"" help:"DRAFT rule id."`
	NoPublish  bool   `optional:"" name:"no-publish" help:"Stage only; do not publish."`
	PublishAll bool   `optional:"" name:"publish-all" help:"Publish even when the draft holds other staged changes."`
	DryRun     bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *PolicyRulesDeleteCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid rule id: %w", err)
	}
	plan := policyPublishPlan{NoPublish: c.NoPublish, PublishAll: c.PublishAll}
	if err := plan.validate(); err != nil {
		return err
	}
	if c.DryRun {
		return out.WriteDryRun("Would delete policy rule "+c.ID, map[string]string{"id": c.ID})
	}
	project, err := resolveProject(c.Project)
	if err != nil {
		return err
	}
	if err := requireProjectForOrgKey(project); err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	ops := projectScopeOps(client, project)
	pending, err := prePublishPending(plan, ops)
	if err != nil {
		return err
	}
	if err := client.DeletePolicyRule(newContext(), project, c.ID); err != nil {
		return err
	}
	return finishPolicyWrite(out, policyWriteOutcome{DeletedID: c.ID}, plan, pending, ops)
}

// PolicyRulesReorderCmd is `onecli policy rules reorder`.
type PolicyRulesReorderCmd struct {
	Project    string `optional:"" short:"p" help:"Project slug."`
	OrderedIds string `required:"" name:"ordered-ids" help:"JSON array of EVERY non-default draft rule id exactly once (get the full list from 'policy rules list --quiet id'). The server rejects a partial list."`
	NoPublish  bool   `optional:"" name:"no-publish" help:"Stage only; do not publish."`
	PublishAll bool   `optional:"" name:"publish-all" help:"Publish even when the draft holds other staged changes."`
	DryRun     bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *PolicyRulesReorderCmd) Run(out *output.Writer) error {
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
		return out.WriteDryRun("Would reorder policy rules", map[string]any{"orderedIds": ids})
	}
	project, err := resolveProject(c.Project)
	if err != nil {
		return err
	}
	if err := requireProjectForOrgKey(project); err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	ops := projectScopeOps(client, project)
	pending, err := prePublishPending(plan, ops)
	if err != nil {
		return err
	}
	rules, err := client.ReorderPolicyRules(newContext(), project, ids)
	if err != nil {
		return err
	}
	return finishPolicyWrite(out, policyWriteOutcome{Rules: rules}, plan, pending, ops)
}

// ── Default rule / publish / status ─────────────────────────────────────────

// PolicyDefaultCmd groups the Default Rule subcommands.
type PolicyDefaultCmd struct {
	Get PolicyDefaultGetCmd `cmd:"" help:"Show the terminal Default Rule."`
	Set PolicyDefaultSetCmd `cmd:"" help:"Set the Default Rule's action."`
}

// PolicyDefaultGetCmd is `onecli policy default get`.
type PolicyDefaultGetCmd struct {
	Project string `optional:"" short:"p" help:"Project slug."`
	Status  string `optional:"" default:"draft" help:"'draft' or 'published'."`
	Fields  string `optional:"" help:"Comma-separated fields to include."`
}

func (c *PolicyDefaultGetCmd) Run(out *output.Writer) error {
	if c.Status != "draft" && c.Status != "published" {
		return fmt.Errorf("invalid --status %q: must be 'draft' or 'published'", c.Status)
	}
	project, err := resolveProject(c.Project)
	if err != nil {
		return err
	}
	if err := requireProjectForOrgKey(project); err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	rule, err := client.GetPolicyDefault(newContext(), project, c.Status)
	if err != nil {
		return err
	}
	return out.WriteFiltered(rule, c.Fields)
}

// PolicyDefaultSetCmd is `onecli policy default set`.
type PolicyDefaultSetCmd struct {
	Project    string `optional:"" short:"p" help:"Project slug."`
	Action     string `required:"" help:"'allow' or 'block'."`
	NoPublish  bool   `optional:"" name:"no-publish" help:"Stage only; do not publish."`
	PublishAll bool   `optional:"" name:"publish-all" help:"Publish even when the draft holds other staged changes."`
	DryRun     bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *PolicyDefaultSetCmd) Run(out *output.Writer) error {
	if c.Action != "allow" && c.Action != "block" {
		return fmt.Errorf("invalid action %q: must be 'allow' or 'block'", c.Action)
	}
	plan := policyPublishPlan{NoPublish: c.NoPublish, PublishAll: c.PublishAll}
	if err := plan.validate(); err != nil {
		return err
	}
	if c.DryRun {
		return out.WriteDryRun("Would set the Default Rule action", map[string]string{"action": c.Action})
	}
	project, err := resolveProject(c.Project)
	if err != nil {
		return err
	}
	if err := requireProjectForOrgKey(project); err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	ops := projectScopeOps(client, project)
	pending, err := prePublishPending(plan, ops)
	if err != nil {
		return err
	}
	rule, err := client.SetPolicyDefault(newContext(), project, c.Action)
	if err != nil {
		return err
	}
	return finishPolicyWrite(out, policyWriteOutcome{Rule: rule}, plan, pending, ops)
}

// PolicyPublishCmd is `onecli policy publish` — publishes the WHOLE draft.
type PolicyPublishCmd struct {
	Project string `optional:"" short:"p" help:"Project slug."`
	DryRun  bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *PolicyPublishCmd) Run(out *output.Writer) error {
	if c.DryRun {
		return out.WriteDryRun("Would publish the project's whole policy draft", struct{}{})
	}
	project, err := resolveProject(c.Project)
	if err != nil {
		return err
	}
	if err := requireProjectForOrgKey(project); err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	result, err := client.PublishPolicy(newContext(), project)
	if err != nil {
		return err
	}
	return out.Write(result)
}

// PolicyStatusCmd is `onecli policy status` — the staged diff + last publish.
type PolicyStatusCmd struct {
	Project string `optional:"" short:"p" help:"Project slug."`
	Fields  string `optional:"" help:"Comma-separated fields to include."`
}

// policyStatus is the status command's output envelope.
type policyStatus struct {
	LastPublish    *api.PolicyLastPublish `json:"lastPublish"`
	PendingChanges int                    `json:"pendingChanges"`
	Diff           api.PolicyDiff         `json:"diff"`
}

func (c *PolicyStatusCmd) Run(out *output.Writer) error {
	project, err := resolveProject(c.Project)
	if err != nil {
		return err
	}
	if err := requireProjectForOrgKey(project); err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	ctx := newContext()
	draft, err := client.ListPolicyRules(ctx, project, "draft")
	if err != nil {
		return err
	}
	published, err := client.ListPolicyRules(ctx, project, "published")
	if err != nil {
		return err
	}
	dDef, err := client.GetPolicyDefault(ctx, project, "draft")
	if err != nil {
		return err
	}
	pDef, err := client.GetPolicyDefault(ctx, project, "published")
	if err != nil {
		return err
	}
	last, err := client.GetPolicyLastPublish(ctx, project)
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
