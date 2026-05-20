package main

import (
	"encoding/json"
	"fmt"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// OrgAppsCmd is the `onecli org apps` command group.
type OrgAppsCmd struct {
	Configured OrgAppsConfiguredCmd `cmd:"" help:"List providers with org-level credentials configured."`
	Get        OrgAppsGetCmd        `cmd:"" help:"Get app config status for a provider."`
	Configure  OrgAppsConfigureCmd  `cmd:"" help:"Save BYOC credentials for a provider at the org level."`
	Remove     OrgAppsRemoveCmd     `cmd:"" help:"Remove BYOC credentials for a provider at the org level."`
	Toggle     OrgAppsToggleCmd     `cmd:"" help:"Enable or disable an app config at the org level."`
}

// OrgAppsConfiguredCmd is `onecli org apps configured`.
type OrgAppsConfiguredCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *OrgAppsConfiguredCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	providers, err := client.ListConfiguredProviders(newContext())
	if err != nil {
		return err
	}
	return out.WriteFiltered(providers, c.Fields)
}

// OrgAppsGetCmd is `onecli org apps get`.
type OrgAppsGetCmd struct {
	Provider string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	Fields   string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *OrgAppsGetCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	config, err := client.GetOrgAppConfig(newContext(), c.Provider)
	if err != nil {
		return err
	}
	return out.WriteFiltered(config, c.Fields)
}

// OrgAppsConfigureCmd is `onecli org apps configure`.
type OrgAppsConfigureCmd struct {
	Provider     string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	ClientID     string `required:"" name:"client-id" help:"OAuth client ID."`
	ClientSecret string `required:"" name:"client-secret" help:"OAuth client secret."`
	Json         string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun       bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgAppsConfigureCmd) Run(out *output.Writer) error {
	var input api.ConfigAppInput
	if c.Json != "" {
		if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	} else {
		input = api.ConfigAppInput{
			ClientID:     c.ClientID,
			ClientSecret: c.ClientSecret,
		}
	}

	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}

	if c.DryRun {
		preview := map[string]string{
			"provider":     c.Provider,
			"clientId":     input.ClientID,
			"clientSecret": "***",
		}
		return out.WriteDryRun("Would configure org app", preview)
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.UpsertOrgAppConfig(newContext(), c.Provider, input); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "configured", "provider": c.Provider})
}

// OrgAppsRemoveCmd is `onecli org apps remove`.
type OrgAppsRemoveCmd struct {
	Provider string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgAppsRemoveCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would remove org app config", map[string]string{"provider": c.Provider})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.DeleteOrgAppConfig(newContext(), c.Provider); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "removed", "provider": c.Provider})
}

// OrgAppsToggleCmd is `onecli org apps toggle`.
type OrgAppsToggleCmd struct {
	Provider string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	Enabled  bool   `required:"" help:"Set to true to enable, false to disable."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgAppsToggleCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would toggle org app config", map[string]any{"provider": c.Provider, "enabled": c.Enabled})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.ToggleOrgAppConfig(newContext(), c.Provider, c.Enabled); err != nil {
		return err
	}
	status := "disabled"
	if c.Enabled {
		status = "enabled"
	}
	return out.Write(map[string]string{"status": status, "provider": c.Provider})
}
