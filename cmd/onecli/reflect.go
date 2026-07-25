package main

import (
	"fmt"

	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// Read-only policy reflections.
//
// These report what the PUBLISHED policy actually allows, replacing the retired
// reads that returned stored assignment lists (`rules permissions get`,
// `agents secrets`, `agents connections`, `agents granular-access`). The
// difference is load-bearing: a credential granted by a policy rule was
// invisible to the old reads, and is not invisible here.

// PolicyEffectivePermissionsCmd is `onecli policy effective-permissions`.
type PolicyEffectivePermissionsCmd struct {
	Provider string `required:"" help:"App provider (e.g. 'gmail', 'github')."`
	Agent    string `optional:"" help:"Narrow to one agent's identity; omit for the all-agents baseline."`
	Project  string `optional:"" short:"p" help:"Project slug."`
	Fields   string `optional:"" help:"Comma-separated fields to include."`
	Quiet    string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
}

func (c *PolicyEffectivePermissionsCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	if c.Agent != "" {
		if err := validate.ResourceID(c.Agent); err != nil {
			return fmt.Errorf("invalid agent ID: %w", err)
		}
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
	result, err := client.GetEffectiveAppPermissions(newContext(), project, c.Provider, c.Agent)
	if err != nil {
		return err
	}
	if c.Quiet != "" {
		return out.WriteQuiet(result, c.Quiet)
	}
	return out.WriteFiltered(result, c.Fields)
}

// OrgPolicyEffectivePermissionsCmd is `onecli org policy effective-permissions`.
type OrgPolicyEffectivePermissionsCmd struct {
	Provider string `required:"" help:"App provider (e.g. 'gmail', 'github')."`
	Fields   string `optional:"" help:"Comma-separated fields to include."`
	Quiet    string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
}

func (c *OrgPolicyEffectivePermissionsCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	result, err := client.GetOrgEffectiveAppPermissions(newContext(), c.Provider)
	if err != nil {
		return err
	}
	if c.Quiet != "" {
		return out.WriteQuiet(result, c.Quiet)
	}
	return out.WriteFiltered(result, c.Fields)
}

// AgentsCredentialsCmd is `onecli agents credentials`.
type AgentsCredentialsCmd struct {
	ID      string `required:"" help:"ID of the agent."`
	Project string `optional:"" short:"p" help:"Project slug."`
	Fields  string `optional:"" help:"Comma-separated fields to include."`
	Quiet   string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
}

func (c *AgentsCredentialsCmd) Run(out *output.Writer) error {
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
	result, err := client.GetEffectiveCredentials(newContext(), project, c.ID)
	if err != nil {
		return err
	}
	if c.Quiet != "" {
		return out.WriteQuiet(result, c.Quiet)
	}
	return out.WriteFiltered(result, c.Fields)
}

// AppsConnectionsAgentAccessCmd is `onecli apps connections agent-access`.
//
// Named agent-access, not agents: `/v1/connections/:id/agents` is the retired
// path, and reusing its name would read as though it still worked.
type AppsConnectionsAgentAccessCmd struct {
	ID      string `required:"" help:"ID of the app connection."`
	Project string `optional:"" short:"p" help:"Project slug."`
	Fields  string `optional:"" help:"Comma-separated fields to include."`
	Quiet   string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
}

func (c *AppsConnectionsAgentAccessCmd) Run(out *output.Writer) error {
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
	result, err := client.GetConnectionAgentAccess(newContext(), project, c.ID)
	if err != nil {
		return err
	}
	if c.Quiet != "" {
		return out.WriteQuiet(result, c.Quiet)
	}
	return out.WriteFiltered(result, c.Fields)
}
