package main

import (
	"encoding/json"
	"fmt"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// AppsCmd is the `onecli apps` command group.
type AppsCmd struct {
	List       AppsListCmd       `cmd:"" help:"List all apps with config and connection status."`
	Configure  AppsConfigureCmd  `cmd:"" help:"Save OAuth credentials (BYOC) for a provider."`
	Remove     AppsRemoveCmd     `cmd:"" help:"Remove OAuth credentials for a provider."`
	Disconnect AppsDisconnectCmd `cmd:"" help:"Disconnect an app connection."`
}

// AppsListCmd is `onecli apps list`.
type AppsListCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet  string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
	Max    int    `optional:"" default:"20" help:"Maximum number of results to return."`
}

func (c *AppsListCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	apps, err := client.ListApps(newContext())
	if err != nil {
		return err
	}
	if c.Max > 0 && len(apps) > c.Max {
		apps = apps[:c.Max]
	}
	if c.Quiet != "" {
		return out.WriteQuiet(apps, c.Quiet)
	}
	return out.WriteFiltered(apps, c.Fields)
}

// AppsConfigureCmd is `onecli apps configure`.
type AppsConfigureCmd struct {
	Provider     string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	ClientID     string `required:"" name:"client-id" help:"OAuth client ID."`
	ClientSecret string `required:"" name:"client-secret" help:"OAuth client secret."`
	Json         string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun       bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AppsConfigureCmd) Run(out *output.Writer) error {
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
		return out.WriteDryRun("Would configure app", preview)
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.ConfigureApp(newContext(), c.Provider, input); err != nil {
		return err
	}

	return out.Write(map[string]string{
		"status":   "configured",
		"provider": c.Provider,
	})
}

// AppsRemoveCmd is `onecli apps remove`.
type AppsRemoveCmd struct {
	Provider string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AppsRemoveCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would remove app config", map[string]string{"provider": c.Provider})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.UnconfigureApp(newContext(), c.Provider); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "removed", "provider": c.Provider})
}

// AppsDisconnectCmd is `onecli apps disconnect`.
type AppsDisconnectCmd struct {
	Provider string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AppsDisconnectCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would disconnect app", map[string]string{"provider": c.Provider})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.DisconnectApp(newContext(), c.Provider); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "disconnected", "provider": c.Provider})
}
