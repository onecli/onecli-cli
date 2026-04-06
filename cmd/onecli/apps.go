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
	List       AppsListCmd       `cmd:"" help:"List all app connections."`
	Connect    AppsConnectCmd    `cmd:"" help:"Connect an OAuth app (e.g. Google)."`
	Disconnect AppsDisconnectCmd `cmd:"" help:"Disconnect an app."`
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

// AppsConnectCmd is `onecli apps connect`.
type AppsConnectCmd struct {
	Provider     string `required:"" help:"Provider name (e.g. 'google')."`
	ClientID     string `required:"" name:"client-id" help:"OAuth client ID."`
	ClientSecret string `required:"" name:"client-secret" help:"OAuth client secret."`
	Json         string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun       bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

const docsBaseURL = "https://onecli.sh/docs/guides/credential-stubs"

// connectResult wraps the API response with onboarding guidance as structured fields.
type connectResult struct {
	api.App
	NextSteps string `json:"next_steps"`
	DocsURL   string `json:"docs_url"`
}

func (c *AppsConnectCmd) Run(out *output.Writer) error {
	var input api.ConnectAppInput
	if c.Json != "" {
		if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	} else {
		input = api.ConnectAppInput{
			Provider:     c.Provider,
			ClientID:     c.ClientID,
			ClientSecret: c.ClientSecret,
		}
	}

	if err := validate.ResourceID(input.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}

	if c.DryRun {
		preview := map[string]string{
			"provider":     input.Provider,
			"clientId":     input.ClientID,
			"clientSecret": "***",
		}
		return out.WriteDryRun("Would connect app", preview)
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	app, err := client.ConnectApp(newContext(), input)
	if err != nil {
		return err
	}

	result := connectResult{
		App:       *app,
		NextSteps: "Create local credential stub files using 'onecli-managed' as placeholder for all secrets. The OneCLI gateway handles real OAuth token exchange at request time.",
		DocsURL:   docsBaseURL + "/" + input.Provider + ".md",
	}
	return out.Write(result)
}

// AppsDisconnectCmd is `onecli apps disconnect`.
type AppsDisconnectCmd struct {
	ID     string `required:"" help:"ID of the app connection to disconnect."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AppsDisconnectCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid app ID: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would disconnect app", map[string]string{"id": c.ID})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.DisconnectApp(newContext(), c.ID); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "disconnected", "id": c.ID})
}
