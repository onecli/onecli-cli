package main

import (
	"encoding/json"
	"fmt"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// The attach model's write path: a grant attaches one credential to one agent
// and is the only project-scope policy writer (the server compiles grants
// into rules). 'agents grants' is attach INTENT; 'agents credentials' stays
// the EFFECTIVE view with organization guardrails applied.

// AgentsGrantsCmd is the `onecli agents grants` group.
type AgentsGrantsCmd struct {
	List             AgentsGrantsListCmd             `cmd:"" help:"Show the agent's grants: attached app connections (with per-tool access) and secrets."`
	AttachConnection AgentsGrantsAttachConnectionCmd `cmd:"" name:"attach-connection" help:"Attach an app connection to the agent (full access by default; --allow/--ask set per-tool access)."`
	DetachConnection AgentsGrantsDetachConnectionCmd `cmd:"" name:"detach-connection" help:"Detach an app connection from the agent."`
	AttachSecret     AgentsGrantsAttachSecretCmd     `cmd:"" name:"attach-secret" help:"Attach a secret or LLM key to the agent."`
	DetachSecret     AgentsGrantsDetachSecretCmd     `cmd:"" name:"detach-secret" help:"Detach a secret from the agent."`
}

// AgentsGrantsListCmd is `onecli agents grants list`.
type AgentsGrantsListCmd struct {
	ID      string `required:"" help:"ID of the agent."`
	Project string `optional:"" short:"p" help:"Project slug."`
	Fields  string `optional:"" help:"Comma-separated fields to include."`
	Quiet   string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
}

func (c *AgentsGrantsListCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid agent ID: %w", err)
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
	result, err := client.GetAgentGrants(newContext(), project, c.ID)
	if err != nil {
		return err
	}
	if c.Quiet != "" {
		return out.WriteQuiet(result, c.Quiet)
	}
	return out.WriteFiltered(result, c.Fields)
}

// AgentsGrantsAttachConnectionCmd is `onecli agents grants attach-connection`.
type AgentsGrantsAttachConnectionCmd struct {
	ID           string `required:"" help:"ID of the agent."`
	ConnectionID string `required:"" name:"connection-id" help:"ID of the app connection to attach."`
	Allow        string `optional:"" help:"Comma-separated tool IDs to always allow (from 'apps permission-definition'). With --ask, unlisted tools are blocked."`
	Ask          string `optional:"" help:"Comma-separated tool IDs that require manual approval before running."`
	Json         string `optional:"" help:"Raw JSON grant body: {\"access\":\"full\"} or {\"access\":\"custom\",\"allow\":[...],\"ask\":[...]} (do not combine with --allow/--ask)."`
	Project      string `optional:"" short:"p" help:"Project slug."`
	DryRun       bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AgentsGrantsAttachConnectionCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid agent ID: %w", err)
	}
	if err := validate.ResourceID(c.ConnectionID); err != nil {
		return fmt.Errorf("invalid connection ID: %w", err)
	}
	input, err := buildConnectionGrantInput(c.Json, c.Allow, c.Ask)
	if err != nil {
		return err
	}
	if c.DryRun {
		return out.WriteDryRun("Would attach the connection to the agent", map[string]any{
			"id": c.ID, "connection_id": c.ConnectionID, "grant": input,
		})
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
	result, err := client.SetAgentConnectionGrant(newContext(), project, c.ID, c.ConnectionID, input)
	if err != nil {
		return err
	}
	return out.Write(result)
}

// AgentsGrantsDetachConnectionCmd is `onecli agents grants detach-connection`.
type AgentsGrantsDetachConnectionCmd struct {
	ID           string `required:"" help:"ID of the agent."`
	ConnectionID string `required:"" name:"connection-id" help:"ID of the app connection to detach."`
	Project      string `optional:"" short:"p" help:"Project slug."`
	DryRun       bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AgentsGrantsDetachConnectionCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid agent ID: %w", err)
	}
	if err := validate.ResourceID(c.ConnectionID); err != nil {
		return fmt.Errorf("invalid connection ID: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would detach the connection from the agent", map[string]any{
			"id": c.ID, "connection_id": c.ConnectionID,
		})
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
	if err := client.RemoveAgentConnectionGrant(newContext(), project, c.ID, c.ConnectionID); err != nil {
		return err
	}
	return out.Write(map[string]string{
		"status": "detached", "agentId": c.ID, "connectionId": c.ConnectionID,
	})
}

// AgentsGrantsAttachSecretCmd is `onecli agents grants attach-secret`.
type AgentsGrantsAttachSecretCmd struct {
	ID       string `required:"" help:"ID of the agent."`
	SecretID string `required:"" name:"secret-id" help:"ID of the secret (or LLM key) to attach."`
	Project  string `optional:"" short:"p" help:"Project slug."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AgentsGrantsAttachSecretCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid agent ID: %w", err)
	}
	if err := validate.ResourceID(c.SecretID); err != nil {
		return fmt.Errorf("invalid secret ID: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would attach the secret to the agent", map[string]any{
			"id": c.ID, "secret_id": c.SecretID,
		})
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
	result, err := client.SetAgentSecretGrant(newContext(), project, c.ID, c.SecretID)
	if err != nil {
		return err
	}
	return out.Write(result)
}

// AgentsGrantsDetachSecretCmd is `onecli agents grants detach-secret`.
type AgentsGrantsDetachSecretCmd struct {
	ID       string `required:"" help:"ID of the agent."`
	SecretID string `required:"" name:"secret-id" help:"ID of the secret to detach."`
	Project  string `optional:"" short:"p" help:"Project slug."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AgentsGrantsDetachSecretCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid agent ID: %w", err)
	}
	if err := validate.ResourceID(c.SecretID); err != nil {
		return fmt.Errorf("invalid secret ID: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would detach the secret from the agent", map[string]any{
			"id": c.ID, "secret_id": c.SecretID,
		})
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
	if err := client.RemoveAgentSecretGrant(newContext(), project, c.ID, c.SecretID); err != nil {
		return err
	}
	return out.Write(map[string]string{
		"status": "detached", "agentId": c.ID, "secretId": c.SecretID,
	})
}

// AppsConnectionsGrantsCmd is `onecli apps connections grants` — which agents
// a connection is granted to (attach intent; 'agent-access' is the effective
// view).
type AppsConnectionsGrantsCmd struct {
	ID      string `required:"" help:"ID of the app connection."`
	Project string `optional:"" short:"p" help:"Project slug."`
	Fields  string `optional:"" help:"Comma-separated fields to include."`
	Quiet   string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
}

func (c *AppsConnectionsGrantsCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid connection ID: %w", err)
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
	result, err := client.GetConnectionGrants(newContext(), project, c.ID)
	if err != nil {
		return err
	}
	if c.Quiet != "" {
		return out.WriteQuiet(result, c.Quiet)
	}
	return out.WriteFiltered(result, c.Fields)
}

// buildConnectionGrantInput derives the grant body from the flags: no flags =
// the uncustomized full attach; --allow/--ask derive a custom grant; --json
// takes the raw wire body and cannot combine with the flags. Tool IDs are
// validated for syntax only — catalog membership is the server's call.
func buildConnectionGrantInput(jsonRaw, allowCSV, askCSV string) (api.ConnectionGrantInput, error) {
	if jsonRaw != "" {
		if allowCSV != "" || askCSV != "" {
			return api.ConnectionGrantInput{}, fmt.Errorf("--json carries the whole grant body; do not combine it with --allow/--ask")
		}
		var raw struct {
			Access string   `json:"access"`
			Allow  []string `json:"allow"`
			Ask    []string `json:"ask"`
		}
		if err := json.Unmarshal([]byte(jsonRaw), &raw); err != nil {
			return api.ConnectionGrantInput{}, fmt.Errorf("invalid --json: %w", err)
		}
		if raw.Access != "full" && raw.Access != "custom" {
			return api.ConnectionGrantInput{}, fmt.Errorf("invalid --json: access must be \"full\" or \"custom\"")
		}
		return api.ConnectionGrantInput{Access: raw.Access, Allow: raw.Allow, Ask: raw.Ask}, nil
	}
	allow := splitCSV(allowCSV)
	ask := splitCSV(askCSV)
	if len(allow) == 0 && len(ask) == 0 {
		return api.ConnectionGrantInput{Access: "full"}, nil
	}
	for _, tool := range append(append([]string{}, allow...), ask...) {
		if err := validate.ResourceID(tool); err != nil {
			return api.ConnectionGrantInput{}, fmt.Errorf("invalid tool ID %q: %w", tool, err)
		}
	}
	return api.ConnectionGrantInput{Access: "custom", Allow: allow, Ask: ask}, nil
}
