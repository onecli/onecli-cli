package main

import (
	"fmt"

	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// VaultsCmd is the `onecli vaults` command group.
type VaultsCmd struct {
	List VaultsListCmd `cmd:"" help:"List external vault connections (e.g. 1Password)."`
}

// VaultsListCmd is `onecli vaults list`.
type VaultsListCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet  string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
}

func (c *VaultsListCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	vaults, err := client.ListVaults(newContext())
	if err != nil {
		return err
	}
	if c.Quiet != "" {
		return out.WriteQuiet(vaults, c.Quiet)
	}
	return out.WriteFiltered(vaults, c.Fields)
}

// CountsCmd is `onecli counts` — the project's resource totals.
type CountsCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *CountsCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	counts, err := client.GetCounts(newContext())
	if err != nil {
		return err
	}
	return out.WriteFiltered(counts, c.Fields)
}

// OrgSettingsCmd is the `onecli org settings` command group.
type OrgSettingsCmd struct {
	Get OrgSettingsGetCmd `cmd:"" help:"RETIRED — updated servers answer 410 Gone. The allow/deny posture is the Default Rule — use 'org policy default'."`
	Set OrgSettingsSetCmd `cmd:"" help:"RETIRED — updated servers answer 410 Gone. Set the posture with 'org policy default set --action'."`
}

// OrgSettingsGetCmd is `onecli org settings get`.
type OrgSettingsGetCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *OrgSettingsGetCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	settings, err := client.GetOrgSettings(newContext())
	if err != nil {
		return err
	}
	return out.WriteFiltered(settings, c.Fields)
}

// OrgSettingsSetCmd is `onecli org settings set`.
type OrgSettingsSetCmd struct {
	PolicyMode string `required:"" name:"policy-mode" help:"Policy mode: 'allow' (default-allow) or 'deny' (lockdown default-deny)."`
	DryRun     bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgSettingsSetCmd) Run(out *output.Writer) error {
	if c.PolicyMode != "allow" && c.PolicyMode != "deny" {
		return fmt.Errorf("invalid policy-mode %q: must be 'allow' or 'deny'", c.PolicyMode)
	}
	if c.DryRun {
		return out.WriteDryRun("Would update org settings", map[string]string{"policyMode": c.PolicyMode})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	settings, err := client.UpdateOrgSettings(newContext(), c.PolicyMode)
	if err != nil {
		return err
	}
	return out.Write(settings)
}

// AppsConfigCmd is the `onecli apps config` command group (project scope).
type AppsConfigCmd struct {
	Get    AppsConfigGetCmd    `cmd:"" help:"Get config status for a provider."`
	Toggle AppsConfigToggleCmd `cmd:"" help:"Enable or disable a provider's config."`
}

// AppsConfigGetCmd is `onecli apps config get`.
type AppsConfigGetCmd struct {
	Provider string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	Fields   string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *AppsConfigGetCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	config, err := client.GetAppConfig(newContext(), c.Provider)
	if err != nil {
		return err
	}
	return out.WriteFiltered(config, c.Fields)
}

// AppsConfigToggleCmd is `onecli apps config toggle`.
type AppsConfigToggleCmd struct {
	Provider string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	Enabled  bool   `required:"" help:"Set to true to enable, false to disable."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AppsConfigToggleCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would toggle app config", map[string]any{"provider": c.Provider, "enabled": c.Enabled})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.ToggleAppConfig(newContext(), c.Provider, c.Enabled); err != nil {
		return err
	}
	status := "disabled"
	if c.Enabled {
		status = "enabled"
	}
	return out.Write(map[string]string{"status": status, "provider": c.Provider})
}

// AppsConfiguredCmd is `onecli apps configured` (project scope).
type AppsConfiguredCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *AppsConfiguredCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	providers, err := client.ListConfiguredApps(newContext())
	if err != nil {
		return err
	}
	return out.WriteFiltered(providers, c.Fields)
}

// AppsEnvDefaultsCmd is `onecli apps env-defaults`.
type AppsEnvDefaultsCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *AppsEnvDefaultsCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	providers, err := client.ListEnvDefaultApps(newContext())
	if err != nil {
		return err
	}
	return out.WriteFiltered(providers, c.Fields)
}
