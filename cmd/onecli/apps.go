package main

import (
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
	DryRun       bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

// connectResponse wraps the API response with agent-facing guidance.
type connectResponse struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

const docsBaseURL = "https://onecli.sh/docs/guides/credential-stubs"

func (c *AppsConnectCmd) Run(out *output.Writer) error {
	input := api.ConnectAppInput{
		Provider:     c.Provider,
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
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

	docsURL := docsBaseURL + "/" + input.Provider + ".md"
	fallbackURL := docsBaseURL + "/general-app.md"
	out.SetHint("Your MCP server needs local credential stub files to start. Create them in the format and location the MCP server expects, but use 'onecli-managed' as a placeholder for all secrets. See " + docsURL + " for examples (fallback: " + fallbackURL + " ). The OneCLI gateway handles real OAuth token exchange at request time.")
	resp := connectResponse{
		ID:        app.ID,
		Provider:  app.Provider,
		Status:    app.Status,
		CreatedAt: app.CreatedAt,
	}
	return out.Write(resp)
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
