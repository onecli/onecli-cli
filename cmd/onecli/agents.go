package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// AgentsCmd is the `onecli agents` command group.
type AgentsCmd struct {
	List            AgentsListCmd            `cmd:"" help:"List all agents."`
	GetDefault      AgentsGetDefaultCmd      `cmd:"" name:"get-default" help:"Get the default agent."`
	Create          AgentsCreateCmd          `cmd:"" help:"Create a new agent."`
	Delete          AgentsDeleteCmd          `cmd:"" help:"Delete an agent."`
	Rename          AgentsRenameCmd          `cmd:"" help:"Rename an agent."`
	RegenerateToken AgentsRegenerateTokenCmd `cmd:"" name:"regenerate-token" help:"Regenerate an agent's access token."`
	Secrets         AgentsSecretsCmd         `cmd:"" help:"List secrets assigned to an agent."`
	SetSecrets      AgentsSetSecretsCmd      `cmd:"" name:"set-secrets" help:"Set secrets assigned to an agent."`
	SetSecretMode   AgentsSetSecretModeCmd   `cmd:"" name:"set-secret-mode" help:"Set an agent's secret mode."`
}

// AgentsListCmd is `onecli agents list`.
type AgentsListCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet  string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
	Max    int    `optional:"" default:"20" help:"Maximum number of results to return."`
}

func (c *AgentsListCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	agents, err := client.ListAgents(newContext())
	if err != nil {
		return err
	}
	if c.Max > 0 && len(agents) > c.Max {
		agents = agents[:c.Max]
	}
	if c.Quiet != "" {
		return out.WriteQuiet(agents, c.Quiet)
	}
	return out.WriteFiltered(agents, c.Fields)
}

// AgentsGetDefaultCmd is `onecli agents get-default`.
type AgentsGetDefaultCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *AgentsGetDefaultCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	agent, err := client.GetDefaultAgent(newContext())
	if err != nil {
		return err
	}
	return out.WriteFiltered(agent, c.Fields)
}

// AgentsCreateCmd is `onecli agents create`.
type AgentsCreateCmd struct {
	Name       string `required:"" help:"Display name for the agent."`
	Identifier string `required:"" help:"Unique identifier (lowercase letters, numbers, hyphens)."`
	Json       string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun     bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AgentsCreateCmd) Run(out *output.Writer) error {
	var input api.CreateAgentInput
	if c.Json != "" {
		if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	} else {
		input = api.CreateAgentInput{
			Name:       c.Name,
			Identifier: c.Identifier,
		}
	}

	if c.DryRun {
		return out.WriteDryRun("Would create agent", input)
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	agent, err := client.CreateAgent(newContext(), input)
	if err != nil {
		return err
	}
	return out.Write(agent)
}

// AgentsDeleteCmd is `onecli agents delete`.
type AgentsDeleteCmd struct {
	ID     string `required:"" help:"ID of the agent to delete."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AgentsDeleteCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid agent ID: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would delete agent", map[string]string{"id": c.ID})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.DeleteAgent(newContext(), c.ID); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "deleted", "id": c.ID})
}

// AgentsRenameCmd is `onecli agents rename`.
type AgentsRenameCmd struct {
	ID     string `required:"" help:"ID of the agent to rename."`
	Name   string `required:"" help:"New display name."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AgentsRenameCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid agent ID: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would rename agent", map[string]string{"id": c.ID, "name": c.Name})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.RenameAgent(newContext(), c.ID, api.RenameAgentInput{Name: c.Name}); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "renamed", "id": c.ID, "name": c.Name})
}

// AgentsRegenerateTokenCmd is `onecli agents regenerate-token`.
type AgentsRegenerateTokenCmd struct {
	ID     string `required:"" help:"ID of the agent."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AgentsRegenerateTokenCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid agent ID: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would regenerate agent token", map[string]string{"id": c.ID})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	resp, err := client.RegenerateAgentToken(newContext(), c.ID)
	if err != nil {
		return err
	}
	return out.Write(resp)
}

// AgentsSecretsCmd is `onecli agents secrets`.
type AgentsSecretsCmd struct {
	ID     string `required:"" help:"ID of the agent."`
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *AgentsSecretsCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid agent ID: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	secretIDs, err := client.GetAgentSecrets(newContext(), c.ID)
	if err != nil {
		return err
	}
	return out.Write(secretIDs)
}

// AgentsSetSecretsCmd is `onecli agents set-secrets`.
type AgentsSetSecretsCmd struct {
	ID        string `required:"" help:"ID of the agent."`
	SecretIDs string `required:"" name:"secret-ids" help:"Comma-separated list of secret IDs."`
	DryRun    bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AgentsSetSecretsCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid agent ID: %w", err)
	}
	ids := splitCSV(c.SecretIDs)
	for _, id := range ids {
		if err := validate.ResourceID(id); err != nil {
			return fmt.Errorf("invalid secret ID %q: %w", id, err)
		}
	}
	if c.DryRun {
		return out.WriteDryRun("Would set agent secrets and switch to selective mode", map[string]any{"id": c.ID, "secret_ids": ids})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	ctx := newContext()
	if err := client.SetAgentSecrets(ctx, c.ID, api.SetAgentSecretsInput{SecretIDs: ids}); err != nil {
		return err
	}
	if err := client.SetAgentSecretMode(ctx, c.ID, api.SetSecretModeInput{Mode: "selective"}); err != nil {
		return err
	}
	return out.Write(map[string]any{"status": "updated", "id": c.ID, "secret_ids": ids, "secret_mode": "selective"})
}

// AgentsSetSecretModeCmd is `onecli agents set-secret-mode`.
type AgentsSetSecretModeCmd struct {
	ID     string `required:"" help:"ID of the agent."`
	Mode   string `required:"" help:"Secret mode: 'all' or 'selective'."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AgentsSetSecretModeCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid agent ID: %w", err)
	}
	if c.Mode != "all" && c.Mode != "selective" {
		return fmt.Errorf("invalid mode %q: must be 'all' or 'selective'", c.Mode)
	}
	if c.DryRun {
		return out.WriteDryRun("Would set agent secret mode", map[string]string{"id": c.ID, "mode": c.Mode})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.SetAgentSecretMode(newContext(), c.ID, api.SetSecretModeInput{Mode: c.Mode}); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "updated", "id": c.ID, "mode": c.Mode})
}

// splitCSV splits a comma-separated string into trimmed, non-empty parts.
func splitCSV(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
