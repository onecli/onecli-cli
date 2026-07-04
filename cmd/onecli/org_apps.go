package main

import (
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
	Connect    OrgAppsConnectCmd    `cmd:"" help:"Connect an app at the org level with direct credentials (API-key apps)."`
	Authorize  OrgAppsAuthorizeCmd  `cmd:"" help:"Get the OAuth authorize URL for an org-level app connection."`
	Blocklist  OrgAppsBlocklistCmd  `cmd:"" help:"Manage an app's endpoint blocklist at the org level."`
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
	Provider     string   `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	Field        []string `optional:"" help:"Credential field as key=value (repeatable); names per the app's field definitions."`
	ClientID     string   `optional:"" name:"client-id" help:"OAuth client ID (shorthand for --field clientId=...)."`
	ClientSecret string   `optional:"" name:"client-secret" help:"OAuth client secret (shorthand for --field clientSecret=...)."`
	Json         string   `optional:"" help:"Raw JSON object of credential fields. Merged first; flags override."`
	DryRun       bool     `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgAppsConfigureCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	fields, err := buildConfigFields(c.Json, c.Field, c.ClientID, c.ClientSecret)
	if err != nil {
		return err
	}

	if c.DryRun {
		return out.WriteDryRun("Would configure org app", maskedFieldPreview(c.Provider, fields))
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.UpsertOrgAppConfig(newContext(), c.Provider, fields); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "configured", "provider": c.Provider})
}

// OrgAppsConnectCmd is `onecli org apps connect`.
type OrgAppsConnectCmd struct {
	Provider     string   `required:"" help:"Provider name (e.g. 'fireflies', 'slack')."`
	Field        []string `optional:"" help:"Credential field as key=value (repeatable); names per the app's connection method fields."`
	Json         string   `optional:"" help:"Raw JSON object of credential fields. Merged first; --field overrides."`
	Label        string   `optional:"" help:"Optional label for the connection (defaults to metadata-derived)."`
	ConnectionID string   `optional:"" name:"connection-id" help:"Existing connection id to reconnect instead of creating a new one."`
	Method       string   `optional:"" help:"Connection method for apps with alternates (e.g. 'api_key' on OAuth-primary apps)."`
	DryRun       bool     `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgAppsConnectCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	if c.ConnectionID != "" {
		if err := validate.ResourceID(c.ConnectionID); err != nil {
			return fmt.Errorf("invalid connection id: %w", err)
		}
	}
	fields, err := buildConnectFields(c.Json, c.Field)
	if err != nil {
		return err
	}

	if c.DryRun {
		return out.WriteDryRun("Would connect org app", maskedFieldPreview(c.Provider, fields))
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	input := api.ConnectAppInput{
		Fields:       fields,
		ConnectionID: c.ConnectionID,
		Label:        c.Label,
		Method:       c.Method,
	}
	if err := client.ConnectOrgApp(newContext(), c.Provider, input); err != nil {
		return err
	}
	return out.Write(map[string]string{
		"status":   "connected",
		"provider": c.Provider,
		"scope":    "organization",
	})
}

// OrgAppsAuthorizeCmd is `onecli org apps authorize`.
type OrgAppsAuthorizeCmd struct {
	Provider     string `required:"" help:"Provider name (e.g. 'github', 'google-drive')."`
	ConnectionID string `optional:"" name:"connection-id" help:"Existing connection id to re-authenticate."`
}

func (c *OrgAppsAuthorizeCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	if c.ConnectionID != "" {
		if err := validate.ResourceID(c.ConnectionID); err != nil {
			return fmt.Errorf("invalid connection id: %w", err)
		}
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	authorizeURL, err := client.OrgAppAuthorizeURL(newContext(), c.Provider, c.ConnectionID)
	if err != nil {
		return err
	}
	out.SetHint("Open authorizeUrl in a browser to finish the OAuth flow; the connection is created at the organization level.")
	return out.Write(map[string]string{
		"authorizeUrl": authorizeURL,
		"provider":     c.Provider,
		"scope":        "organization",
	})
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
