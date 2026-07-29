package main

import (
	"strings"

	"github.com/alecthomas/kong"
	"github.com/onecli/onecli-cli/pkg/output"
)

// HelpCmd shows available commands as JSON.
type HelpCmd struct{}

// HelpResponse is the JSON output of the help command.
type HelpResponse struct {
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Description string        `json:"description"`
	Commands    []CommandInfo `json:"commands"`
	Hint        string        `json:"hint"`
}

// CommandInfo describes a single available command.
type CommandInfo struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Args        []ArgInfo `json:"args,omitempty"`
}

// ArgInfo describes a command argument or flag.
type ArgInfo struct {
	Name        string `json:"name"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

func (cmd *HelpCmd) Run(out *output.Writer) error {
	return out.Write(HelpResponse{
		Name:        "onecli",
		Version:     version,
		Description: "CLI for managing OneCLI agents, secrets, rules, projects, and configuration.",
		Commands: []CommandInfo{
			{Name: "run", Description: "Run a command with OneCLI gateway access.", Args: []ArgInfo{
				{Name: "<command>", Required: true, Description: "Command to execute (e.g. claude, cursor, codex)."},
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--agent", Description: "OneCLI agent identifier (uses default if omitted)."},
				{Name: "--gateway", Description: "Gateway host:port override (default: derived from API host)."},
				{Name: "--no-ca", Description: "Skip CA cert write and CA trust env injection."},
				{Name: "--dry-run", Description: "Print resolved env and command without executing."},
			}},
			{Name: "agents list", Description: "List all agents.", Args: []ArgInfo{
				{Name: "--with-grants", Description: "Include each agent's grants summary (attached connections, secrets, LLM keys)."},
				{Name: "--project, -p", Description: "Project slug."},
			}},
			{Name: "agents get-default", Description: "Get the default agent."},
			{Name: "agents create", Description: "Create a new agent.", Args: []ArgInfo{
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--name", Required: true, Description: "Display name for the agent."},
				{Name: "--identifier", Required: true, Description: "Unique identifier (lowercase letters, numbers, hyphens)."},
			}},
			{Name: "agents delete", Description: "Delete an agent.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent to delete."},
			}},
			{Name: "agents rename", Description: "Rename an agent.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent to rename."},
				{Name: "--name", Required: true, Description: "New display name."},
			}},
			{Name: "agents regenerate-token", Description: "Regenerate an agent's access token.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
			}},
			{Name: "agents secrets", Description: "RETIRED — updated servers answer 410 Gone. Use 'agents grants list' (attached) or 'agents credentials' (effective).", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
			}},
			{Name: "agents credentials", Description: "Show which credentials the agent can use and what each one can do (read-only reflection of the published policy).", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--fields", Description: "Comma-separated fields to include."},
				{Name: "--quiet", Description: "Output only the specified field, one per line."},
			}},
			{Name: "agents grants list", Description: "Show the agent's grants: attached app connections (with per-tool access) and secrets. Grants are attach INTENT; 'agents credentials' is the effective view.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--fields", Description: "Comma-separated fields to include."},
				{Name: "--quiet", Description: "Output only the specified field, one per line."},
			}},
			{Name: "agents grants attach-connection", Description: "Attach an app connection to the agent. No tool flags = full access; --allow/--ask set per-tool access (the rest is blocked).", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
				{Name: "--connection-id", Required: true, Description: "ID of the app connection to attach."},
				{Name: "--allow", Description: "Comma-separated tool IDs to always allow (from 'apps permission-definition')."},
				{Name: "--ask", Description: "Comma-separated tool IDs that require manual approval before running."},
				{Name: "--json", Description: "Raw JSON grant body (do not combine with --allow/--ask)."},
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--dry-run", Description: "Validate the request without executing it."},
			}},
			{Name: "agents grants detach-connection", Description: "Detach an app connection from the agent.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
				{Name: "--connection-id", Required: true, Description: "ID of the app connection to detach."},
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--dry-run", Description: "Validate the request without executing it."},
			}},
			{Name: "agents grants attach-secret", Description: "Attach a secret or LLM key to the agent.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
				{Name: "--secret-id", Required: true, Description: "ID of the secret (or LLM key) to attach."},
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--dry-run", Description: "Validate the request without executing it."},
			}},
			{Name: "agents grants detach-secret", Description: "Detach a secret from the agent.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
				{Name: "--secret-id", Required: true, Description: "ID of the secret to detach."},
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--dry-run", Description: "Validate the request without executing it."},
			}},
			{Name: "agents set-secrets", Description: "RETIRED — updated servers answer 410 Gone. Attach with 'agents grants attach-secret'.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
				{Name: "--secret-ids", Required: true, Description: "Comma-separated list of secret IDs."},
			}},
			{Name: "agents set-default", Description: "Mark an agent as the project default.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
			}},
			{Name: "agents granular-access", Description: "RETIRED — updated servers answer 410 Gone. Resource scoping rides the granting rule's conditions; see 'agents credentials'."},
			{Name: "agents connections get", Description: "RETIRED — updated servers answer 410 Gone. Use 'agents grants list'.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
			}},
			{Name: "agents connections set", Description: "RETIRED — updated servers answer 410 Gone. Attach with 'agents grants attach-connection'.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
				{Name: "--json", Required: true, Description: "JSON array of connection assignments."},
			}},
			{Name: "agents set-secret-mode", Description: "RETIRED — updated servers answer 410 Gone; agents are always selective. Attach credentials with 'agents grants'.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
				{Name: "--mode", Required: true, Description: "Secret mode: 'all' or 'selective'."},
			}},
			{Name: "secrets list", Description: "List all secrets.", Args: []ArgInfo{
				{Name: "--project, -p", Description: "Project slug."},
			}},
			{Name: "secrets create", Description: "Create a new secret.", Args: []ArgInfo{
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--name", Required: true, Description: "Display name for the secret."},
				{Name: "--type", Required: true, Description: "Secret type: 'anthropic' or 'generic'."},
				{Name: "--value", Required: true, Description: "Secret value (e.g. API key)."},
				{Name: "--host-pattern", Required: true, Description: "Host pattern to match."},
			}},
			{Name: "secrets update", Description: "Update an existing secret.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the secret to update."},
			}},
			{Name: "secrets delete", Description: "Delete a secret.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the secret to delete."},
			}},
			{Name: "apps list", Description: "List all apps with config and connection status."},
			{Name: "apps get", Description: "Get a single app with setup guidance.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
			}},
			{Name: "apps configure", Description: "Save credentials (BYOC) for a provider.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
				{Name: "--field", Description: "Credential field as key=value (repeatable); names per the app's field definitions."},
				{Name: "--client-id", Description: "OAuth client ID (shorthand for --field clientId=...)."},
				{Name: "--client-secret", Description: "OAuth client secret (shorthand for --field clientSecret=...)."},
				{Name: "--json", Description: "Raw JSON object of credential fields."},
			}},
			{Name: "apps remove", Description: "Remove BYOC credentials for a provider.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
			}},
			{Name: "apps disconnect", Description: "Disconnect an app connection.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
			}},
			{Name: "apps connections list", Description: "List app connections, optionally filtered by provider.", Args: []ArgInfo{
				{Name: "--provider", Description: "Filter by provider name."},
			}},
			{Name: "apps connections rename", Description: "Rename an app connection.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the connection."},
				{Name: "--label", Required: true, Description: "New display label."},
			}},
			{Name: "apps connections agent-access", Description: "Show which agents can reach a connection, and what each can do (read-only).", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the app connection."},
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--fields", Description: "Comma-separated fields to include."},
				{Name: "--quiet", Description: "Output only the specified field, one per line."},
			}},
			{Name: "apps connections grants", Description: "Show which agents a connection is granted to (attach intent, read-only; 'agent-access' is the effective view).", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the app connection."},
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--fields", Description: "Comma-separated fields to include."},
				{Name: "--quiet", Description: "Output only the specified field, one per line."},
			}},
			{Name: "apps permission-definition", Description: "Show an app's tool catalog (groups + toolIds) for permission rules.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
			}},
			{Name: "apps config get", Description: "Get config status for a provider.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
			}},
			{Name: "apps config toggle", Description: "Enable or disable a provider's config.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
				{Name: "--enabled", Required: true, Description: "Set to true to enable, false to disable."},
			}},
			{Name: "apps configured", Description: "List providers with an enabled config."},
			{Name: "apps env-defaults", Description: "List providers with platform default credentials."},
			{Name: "apps blocklist list", Description: "Show blocklist state for a provider.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
			}},
			{Name: "apps blocklist activate", Description: "Activate a predefined blocklist host.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
				{Name: "--host-id", Required: true, Description: "Predefined blocklist host ID."},
			}},
			{Name: "apps blocklist add", Description: "Add a custom blocklist rule.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
				{Name: "--name", Required: true, Description: "Display name for the rule."},
				{Name: "--host-pattern", Required: true, Description: "Host pattern to block."},
			}},
			{Name: "apps blocklist toggle", Description: "Enable or disable a blocklist rule.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
				{Name: "--rule-id", Required: true, Description: "Blocklist rule ID."},
				{Name: "--enabled", Required: true, Description: "Set to true to enable, false to disable."},
			}},
			{Name: "apps blocklist remove", Description: "Remove a blocklist rule.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
				{Name: "--rule-id", Required: true, Description: "Blocklist rule ID."},
			}},
			{Name: "rules list", Description: "RETIRED — updated servers answer 410 Gone. Grant access with 'agents grants'; read it with 'agents credentials'.", Args: []ArgInfo{
				{Name: "--project, -p", Description: "Project slug."},
			}},
			{Name: "rules get", Description: "RETIRED — updated servers answer 410 Gone. Grant access with 'agents grants'.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the rule."},
			}},
			{Name: "rules create", Description: "RETIRED — updated servers answer 410 Gone. Attach credentials with 'agents grants attach-connection' / 'attach-secret'.", Args: []ArgInfo{
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--name", Required: true, Description: "Display name for the rule."},
				{Name: "--host-pattern", Required: true, Description: "Host pattern to match."},
				{Name: "--action", Required: true, Description: "Action: 'block', 'rate_limit', 'manual_approval', or 'allow'."},
				{Name: "--conditions", Description: "Content conditions as a JSON array."},
			}},
			{Name: "rules update", Description: "RETIRED — updated servers answer 410 Gone. Manage per-tool access with 'agents grants attach-connection --allow/--ask'.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the rule to update."},
			}},
			{Name: "rules delete", Description: "RETIRED — updated servers answer 410 Gone. Detach with 'agents grants detach-connection' / 'detach-secret'.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the rule to delete."},
			}},
			{Name: "rules permissions get", Description: "RETIRED — updated servers answer 410 Gone. Use 'policy effective-permissions' to read; set per-tool access with 'agents grants'.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
				{Name: "--agent-id", Description: "Show only this agent's override layer."},
			}},
			{Name: "rules permissions set", Description: "RETIRED — updated servers answer 410 Gone. Set per-tool access with 'agents grants attach-connection --allow/--ask'.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
				{Name: "--tool", Description: "Tool ID (see 'apps permission-definition')."},
				{Name: "--permission", Description: "Permission: 'allow', 'manual_approval', 'block', or 'inherit' (agent layer only)."},
				{Name: "--agent-id", Description: "Target one agent's override layer."},
				{Name: "--json", Description: "Raw JSON payload with 'changes' array."},
			}},
			{Name: "rules overlap", Description: "RETIRED — updated servers answer 410 Gone. No replacement — overlap detection lives in the Policy console.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
			}},
			{Name: "policy rules list", Description: "RETIRED at project scope — updated servers answer 410 Gone (project rules are compiled from grants). Use 'agents grants list'; org rules: 'org policy rules list'.", Args: []ArgInfo{
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--status", Description: "'draft' (default) or 'published' (enforced)."},
			}},
			{Name: "policy rules get", Description: "RETIRED at project scope — updated servers answer 410 Gone. Use 'agents grants list'.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "Draft rule id (published ids regenerate every publish — match by logicalId)."},
			}},
			{Name: "policy rules create", Description: "RETIRED at project scope — updated servers answer 410 Gone. Attach credentials with 'agents grants'; org rules: 'org policy rules create'.", Args: []ArgInfo{
				{Name: "--name", Description: "Display name (required unless --json)."},
				{Name: "--action", Description: "'allow' or 'block' (required unless --json)."},
				{Name: "--targets", Description: "JSON array of targets: app/connection/secret/network (required unless --json)."},
				{Name: "--identities", Description: "JSON array of identities; omit for all agents."},
				{Name: "--rate-limit", Description: "Max requests per window (allow rules; pair with --rate-limit-window)."},
				{Name: "--require-approval", Description: "Require manual approval (allow rules)."},
				{Name: "--json", Description: "Raw JSON payload for the full rule (do not combine with field flags)."},
				{Name: "--no-publish", Description: "Stage only."},
				{Name: "--publish-all", Description: "Publish even when the draft holds other staged changes."},
			}},
			{Name: "policy rules update", Description: "RETIRED at project scope — updated servers answer 410 Gone. Manage per-tool access with 'agents grants attach-connection --allow/--ask'.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "Draft rule id."},
			}},
			{Name: "policy rules delete", Description: "RETIRED at project scope — updated servers answer 410 Gone. Detach with 'agents grants detach-connection' / 'detach-secret'.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "Draft rule id."},
			}},
			{Name: "policy rules reorder", Description: "RETIRED at project scope — updated servers answer 410 Gone. Grants have no ordering; org rules: 'org policy rules reorder'.", Args: []ArgInfo{
				{Name: "--ordered-ids", Required: true, Description: "JSON array of all draft rule ids (from 'policy rules list --quiet id')."},
			}},
			{Name: "policy default get", Description: "RETIRED at project scope — updated servers answer 410 Gone. The posture is the org Default Rule: 'org policy default get'.", Args: []ArgInfo{
				{Name: "--status", Description: "'draft' (default) or 'published'."},
			}},
			{Name: "policy default set", Description: "RETIRED at project scope — updated servers answer 410 Gone. Set the posture with 'org policy default set --action'.", Args: []ArgInfo{
				{Name: "--action", Required: true, Description: "'allow' or 'block'."},
			}},
			{Name: "policy publish", Description: "RETIRED at project scope — updated servers answer 410 Gone. Grant changes publish immediately; org drafts: 'org policy publish'."},
			{Name: "policy status", Description: "RETIRED at project scope — updated servers answer 410 Gone. Use 'agents grants list' and 'org policy status'."},
			{Name: "projects list", Description: "List all projects."},
			{Name: "projects get", Description: "Get a single project by ID.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the project to retrieve."},
			}},
			{Name: "policy effective-permissions", Description: "Show what the published policy allows for an app, per tool (read-only). Replaces 'rules permissions get'.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "App provider (e.g. 'gmail', 'github')."},
				{Name: "--agent", Description: "Narrow to one agent's identity; omit for the all-agents baseline."},
				{Name: "--project, -p", Description: "Project slug."},
				{Name: "--fields", Description: "Comma-separated fields to include."},
				{Name: "--quiet", Description: "Output only the specified field, one per line."},
			}},
			{Name: "projects create", Description: "Create a new project.", Args: []ArgInfo{
				{Name: "--name", Required: true, Description: "Display name for the project."},
			}},
			{Name: "projects update", Description: "Update an existing project.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the project to update."},
				{Name: "--name", Description: "New display name."},
			}},
			{Name: "projects delete", Description: "Delete a project.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the project to delete."},
			}},
			{Name: "org secrets list", Description: "List all org-scoped secrets."},
			{Name: "org secrets create", Description: "Create a new org-scoped secret.", Args: []ArgInfo{
				{Name: "--name", Required: true, Description: "Display name for the secret."},
				{Name: "--type", Required: true, Description: "Secret type: 'anthropic', 'openai', or 'generic'."},
				{Name: "--value", Required: true, Description: "Secret value (e.g. API key)."},
				{Name: "--host-pattern", Required: true, Description: "Host pattern to match."},
			}},
			{Name: "org secrets update", Description: "Update an org-scoped secret.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the secret to update."},
			}},
			{Name: "org secrets delete", Description: "Delete an org-scoped secret.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the secret to delete."},
			}},
			{Name: "org rules list", Description: "RETIRED — updated servers answer 410 Gone. Use 'org policy rules list'."},
			{Name: "org rules get", Description: "RETIRED — updated servers answer 410 Gone. Use 'org policy rules get'.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the rule to retrieve."},
			}},
			{Name: "org rules create", Description: "RETIRED — updated servers answer 410 Gone. Use 'org policy rules create'.", Args: []ArgInfo{
				{Name: "--name", Required: true, Description: "Display name for the rule."},
				{Name: "--host-pattern", Required: true, Description: "Host pattern to match."},
				{Name: "--action", Required: true, Description: "Action: 'block', 'rate_limit', 'manual_approval', or 'allow'."},
			}},
			{Name: "org rules update", Description: "RETIRED — updated servers answer 410 Gone. Use 'org policy rules update'.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the rule to update."},
			}},
			{Name: "org rules delete", Description: "RETIRED — updated servers answer 410 Gone. Use 'org policy rules delete'.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the rule to delete."},
			}},
			{Name: "org rules permissions get", Description: "RETIRED — updated servers answer 410 Gone. Use 'org policy effective-permissions'.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
			}},
			{Name: "org rules permissions set", Description: "RETIRED — updated servers answer 410 Gone. Author an app-target rule with 'org policy rules create' instead.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
				{Name: "--json", Required: true, Description: "JSON payload with 'changes' array."},
			}},
			{Name: "org rules overlap", Description: "RETIRED — updated servers answer 410 Gone. No replacement — overlap detection lives in the Policy console.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
			}},
			{Name: "org policy rules list", Description: "List org policy-engine rules (draft or published).", Args: []ArgInfo{
				{Name: "--status", Description: "'draft' (default) or 'published' (enforced)."},
			}},
			{Name: "org policy rules get", Description: "Get one DRAFT org policy rule by id.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "Draft rule id (published ids regenerate every publish — match by logicalId)."},
			}},
			{Name: "org policy rules create", Description: "Create an org policy rule (group/user identities; auto-publishes when the draft is clean).", Args: []ArgInfo{
				{Name: "--name", Description: "Display name (required unless --json)."},
				{Name: "--action", Description: "'allow' or 'block' (required unless --json)."},
				{Name: "--targets", Description: "JSON array of targets (required unless --json)."},
				{Name: "--identities", Description: "JSON array — org rules take agentGroup/user/group identities."},
				{Name: "--no-publish", Description: "Stage only."},
				{Name: "--publish-all", Description: "Publish even when the draft holds other staged changes."},
			}},
			{Name: "org policy rules update", Description: "Update a DRAFT org policy rule.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "Draft rule id."},
			}},
			{Name: "org policy rules delete", Description: "Delete a DRAFT org policy rule.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "Draft rule id."},
			}},
			{Name: "org policy rules reorder", Description: "Reorder the org draft (full id permutation).", Args: []ArgInfo{
				{Name: "--ordered-ids", Required: true, Description: "JSON array of all org draft rule ids."},
			}},
			{Name: "org policy default get", Description: "Show the org's terminal Default Rule."},
			{Name: "org policy default set", Description: "Set the org Default Rule's action.", Args: []ArgInfo{
				{Name: "--action", Required: true, Description: "'allow' or 'block'."},
			}},
			{Name: "org policy publish", Description: "Publish the org's WHOLE staged draft."},
			{Name: "org policy status", Description: "Show the org's staged changes and last publish."},
			{Name: "org connections list", Description: "List all org-scoped connections.", Args: []ArgInfo{
				{Name: "--provider", Description: "Filter by provider name."},
			}},
			{Name: "org policy effective-permissions", Description: "Show what the published org policy allows for an app, per tool (read-only). Replaces 'org rules permissions get'.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "App provider (e.g. 'gmail', 'github')."},
				{Name: "--fields", Description: "Comma-separated fields to include."},
				{Name: "--quiet", Description: "Output only the specified field, one per line."},
			}},
			{Name: "org connections rename", Description: "Rename an org-scoped connection.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the connection."},
				{Name: "--label", Required: true, Description: "New display label."},
			}},
			{Name: "org connections delete", Description: "Delete an org-scoped connection.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the connection to delete."},
			}},
			{Name: "org apps configured", Description: "List providers with org-level credentials configured."},
			{Name: "org apps get", Description: "Get app config status for a provider.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
			}},
			{Name: "org apps configure", Description: "Save BYOC credentials at the org level.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
				{Name: "--field", Description: "Credential field as key=value (repeatable)."},
				{Name: "--client-id", Description: "OAuth client ID (shorthand)."},
				{Name: "--client-secret", Description: "OAuth client secret (shorthand)."},
				{Name: "--json", Description: "Raw JSON object of credential fields."},
			}},
			{Name: "org apps remove", Description: "Remove BYOC credentials at the org level.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
			}},
			{Name: "org apps toggle", Description: "Enable or disable an app config at the org level.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
				{Name: "--enabled", Required: true, Description: "Set to true to enable, false to disable."},
			}},
			{Name: "org apps connect", Description: "Connect an app at the org level with direct credentials (API-key apps); the connection is shared by every project.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'fireflies', 'slack')."},
				{Name: "--field", Description: "Credential field as key=value (repeatable)."},
				{Name: "--json", Description: "Raw JSON object of credential fields."},
				{Name: "--label", Description: "Optional label for the connection."},
				{Name: "--connection-id", Description: "Existing connection id to reconnect."},
				{Name: "--method", Description: "Connection method for apps with alternates (e.g. 'api_key')."},
			}},
			{Name: "org apps authorize", Description: "Get the OAuth authorize URL for an org-level app connection; open it in a browser to finish.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'google-drive')."},
				{Name: "--connection-id", Description: "Existing connection id to re-authenticate."},
			}},
			{Name: "org apps blocklist list", Description: "Show org blocklist state for a provider.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
			}},
			{Name: "org apps blocklist activate", Description: "Activate a predefined blocklist host (org).", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
				{Name: "--host-id", Required: true, Description: "Predefined blocklist host ID."},
			}},
			{Name: "org apps blocklist add", Description: "Add a custom blocklist rule (org).", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
				{Name: "--name", Required: true, Description: "Display name for the rule."},
				{Name: "--host-pattern", Required: true, Description: "Host pattern to block."},
			}},
			{Name: "org apps blocklist toggle", Description: "Enable or disable a blocklist rule (org).", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
				{Name: "--rule-id", Required: true, Description: "Blocklist rule ID."},
				{Name: "--enabled", Required: true, Description: "Set to true to enable, false to disable."},
			}},
			{Name: "org apps blocklist remove", Description: "Remove a blocklist rule (org).", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name."},
				{Name: "--rule-id", Required: true, Description: "Blocklist rule ID."},
			}},
			{Name: "org settings get", Description: "RETIRED — updated servers answer 410 Gone. The allow/deny posture is the Default Rule — use 'org policy default'."},
			{Name: "org settings set", Description: "RETIRED — updated servers answer 410 Gone. Set the posture with 'org policy default set --action'.", Args: []ArgInfo{
				{Name: "--policy-mode", Required: true, Description: "Policy mode: 'allow' or 'deny'."},
			}},
			{Name: "vaults list", Description: "List external vault connections (e.g. 1Password)."},
			{Name: "counts", Description: "Show the project's resource counts."},
			{Name: "auth login", Description: "Store API key for authentication."},
			{Name: "auth logout", Description: "Remove stored API key."},
			{Name: "auth status", Description: "Show authentication status."},
			{Name: "auth update", Description: "Update your profile (display name).", Args: []ArgInfo{
				{Name: "--name", Required: true, Description: "New display name."},
			}},
			{Name: "auth api-key", Description: "Show your current API key."},
			{Name: "auth regenerate-api-key", Description: "Regenerate your API key."},
			{Name: "config get <key>", Description: "Get a config value."},
			{Name: "config set <key> <value>", Description: "Set a config value."},
			{Name: "migrate", Description: "Migrate data to OneCLI Cloud.", Args: []ArgInfo{
				{Name: "--cloud-key", Required: true, Description: "OneCLI Cloud API key."},
			}},
			{Name: "version", Description: "Print version information."},
		},
		Hint: "run 'onecli <command> --help' to see available subcommands and flags",
	})
}

// subcommandHelpResponse is the JSON output for subcommand-level --help.
type subcommandHelpResponse struct {
	Commands []CommandInfo `json:"commands"`
}

// jsonHelpPrinter returns a kong.HelpPrinter that outputs JSON.
func jsonHelpPrinter(out *output.Writer) kong.HelpPrinter {
	return func(options kong.HelpOptions, ctx *kong.Context) error {
		selected := ctx.Selected()

		// Root level -> full help response.
		if selected == nil || selected.Type == kong.ApplicationNode {
			cmd := &HelpCmd{}
			return cmd.Run(out)
		}

		// Subcommand level -> collect leaf commands under this node.
		var commands []CommandInfo
		prefix := kongParentPrefix(selected)
		collectKongLeafCommands(selected, prefix, &commands)
		return out.Write(subcommandHelpResponse{Commands: commands})
	}
}

// collectKongLeafCommands walks a Kong node tree and collects leaf commands.
func collectKongLeafCommands(node *kong.Node, prefix string, commands *[]CommandInfo) {
	if node.Hidden {
		return
	}

	path := node.Name
	if prefix != "" {
		path = prefix + " " + node.Name
	}

	// Intermediate node -> recurse into children.
	if len(node.Children) > 0 {
		for _, child := range node.Children {
			collectKongLeafCommands(child, path, commands)
		}
		return
	}

	// Leaf command -> collect positional args and flags.
	cmd := CommandInfo{
		Name:        path,
		Description: node.Help,
	}
	for _, pos := range node.Positional {
		cmd.Args = append(cmd.Args, ArgInfo{
			Name:        "<" + pos.Name + ">",
			Required:    pos.Required,
			Description: pos.Help,
		})
	}
	for _, flag := range node.Flags {
		if flag.Name == "help" || flag.Hidden {
			continue
		}
		cmd.Args = append(cmd.Args, ArgInfo{
			Name:        "--" + flag.Name,
			Required:    flag.Required,
			Description: flag.Help,
		})
	}
	*commands = append(*commands, cmd)
}

// kongParentPrefix builds the command path prefix from a node's parent chain,
// excluding the application root.
func kongParentPrefix(node *kong.Node) string {
	var parts []string
	for n := node.Parent; n != nil && n.Type != kong.ApplicationNode; n = n.Parent {
		parts = append([]string{n.Name}, parts...)
	}
	return strings.Join(parts, " ")
}
