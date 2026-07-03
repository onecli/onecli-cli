package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// configureResult is the structured response after configuring an app.
type configureResult struct {
	App    string `json:"app"`
	Status string `json:"status"`
}

// AppsCmd is the `onecli apps` command group.
type AppsCmd struct {
	List                 AppsListCmd                 `cmd:"" help:"List all apps with config and connection status."`
	Get                  AppsGetCmd                  `cmd:"" help:"Get a single app with setup guidance."`
	Configure            AppsConfigureCmd            `cmd:"" help:"Save credentials (BYOC) for a provider."`
	Remove               AppsRemoveCmd               `cmd:"" help:"Remove BYOC credentials for a provider."`
	Disconnect           AppsDisconnectCmd           `cmd:"" help:"Disconnect an app connection."`
	Connections          AppsConnectionsCmd          `cmd:"" help:"Manage app connections."`
	PermissionDefinition AppsPermissionDefinitionCmd `cmd:"" name:"permission-definition" help:"Show an app's tool catalog (groups + toolIds) for permission rules."`
	Config               AppsConfigCmd               `cmd:"" help:"Inspect and toggle a provider's BYOC config."`
	Configured           AppsConfiguredCmd           `cmd:"" help:"List providers with an enabled config."`
	EnvDefaults          AppsEnvDefaultsCmd          `cmd:"" name:"env-defaults" help:"List providers with platform default credentials."`
	Blocklist            AppsBlocklistCmd            `cmd:"" help:"Manage an app's endpoint blocklist."`
}

// AppsConnectionsCmd is the `onecli apps connections` command group.
type AppsConnectionsCmd struct {
	List   AppsConnectionsListCmd   `cmd:"" help:"List app connections, optionally filtered by provider."`
	Rename AppsConnectionsRenameCmd `cmd:"" help:"Rename an app connection."`
}

// AppsConnectionsListCmd is `onecli apps connections list`.
type AppsConnectionsListCmd struct {
	Provider string `optional:"" help:"Filter by provider name (e.g. 'github', 'gmail')."`
	Fields   string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet    string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
	Max      int    `optional:"" default:"20" help:"Maximum number of results to return."`
}

func (c *AppsConnectionsListCmd) Run(out *output.Writer) error {
	if c.Provider != "" {
		if err := validate.ResourceID(c.Provider); err != nil {
			return fmt.Errorf("invalid provider: %w", err)
		}
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	connections, err := client.ListConnections(newContext(), c.Provider)
	if err != nil {
		return err
	}
	if c.Max > 0 && len(connections) > c.Max {
		connections = connections[:c.Max]
	}
	if c.Quiet != "" {
		return out.WriteQuiet(connections, c.Quiet)
	}
	return out.WriteFiltered(connections, c.Fields)
}

// AppsConnectionsRenameCmd is `onecli apps connections rename`.
type AppsConnectionsRenameCmd struct {
	ID     string `required:"" help:"ID of the connection to rename."`
	Label  string `required:"" help:"New display label."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AppsConnectionsRenameCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid connection ID: %w", err)
	}
	if c.Label == "" {
		return fmt.Errorf("label must not be empty")
	}
	if c.DryRun {
		return out.WriteDryRun("Would rename connection", map[string]string{"id": c.ID, "label": c.Label})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	conn, err := client.RenameConnection(newContext(), c.ID, c.Label)
	if err != nil {
		return err
	}
	return out.Write(conn)
}

// AppsPermissionDefinitionCmd is `onecli apps permission-definition`.
type AppsPermissionDefinitionCmd struct {
	Provider string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	Fields   string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *AppsPermissionDefinitionCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	def, err := client.GetPermissionDefinition(newContext(), c.Provider)
	if err != nil {
		return err
	}
	return out.WriteFiltered(def, c.Fields)
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

// AppsGetCmd is `onecli apps get`.
type AppsGetCmd struct {
	Provider string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	Fields   string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *AppsGetCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	app, err := client.GetApp(newContext(), c.Provider)
	if err != nil {
		return err
	}

	if app.Hint != "" {
		out.SetHint(app.Hint)
		app.Hint = ""
	}

	return out.WriteFiltered(app, c.Fields)
}

// buildConfigFields assembles the credential fields for an app configure
// command from, in precedence order: a raw --json object, repeated
// --field key=value flags, and the --client-id/--client-secret sugar for
// OAuth-style apps. Field names must match the app's own configurable field
// definitions (e.g. github-app uses appId/appSlug/privateKey).
func buildConfigFields(jsonPayload string, fieldFlags []string, clientID, clientSecret string) (api.ConfigFields, error) {
	fields := api.ConfigFields{}
	if jsonPayload != "" {
		if err := json.Unmarshal([]byte(jsonPayload), &fields); err != nil {
			return nil, fmt.Errorf("invalid JSON payload (expected an object of string fields): %w", err)
		}
	}
	for _, f := range fieldFlags {
		key, value, ok := strings.Cut(f, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid --field %q: expected key=value", f)
		}
		fields[key] = value
	}
	if clientID != "" {
		fields["clientId"] = clientID
	}
	if clientSecret != "" {
		fields["clientSecret"] = clientSecret
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("no credential fields provided: use --field key=value (repeatable), --json, or --client-id/--client-secret")
	}
	return fields, nil
}

// maskedFieldPreview lists the field names being set with masked values, so
// dry-run output never echoes secrets.
func maskedFieldPreview(provider string, fields api.ConfigFields) map[string]string {
	preview := map[string]string{"provider": provider}
	for key := range fields {
		preview[key] = "***"
	}
	return preview
}

// AppsConfigureCmd is `onecli apps configure`.
type AppsConfigureCmd struct {
	Provider     string   `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	Field        []string `optional:"" help:"Credential field as key=value (repeatable); names per the app's field definitions."`
	ClientID     string   `optional:"" name:"client-id" help:"OAuth client ID (shorthand for --field clientId=...)."`
	ClientSecret string   `optional:"" name:"client-secret" help:"OAuth client secret (shorthand for --field clientSecret=...)."`
	Json         string   `optional:"" help:"Raw JSON object of credential fields. Merged first; flags override."`
	DryRun       bool     `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AppsConfigureCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	fields, err := buildConfigFields(c.Json, c.Field, c.ClientID, c.ClientSecret)
	if err != nil {
		return err
	}

	if c.DryRun {
		return out.WriteDryRun("Would configure app", maskedFieldPreview(c.Provider, fields))
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.ConfigureApp(newContext(), c.Provider, fields); err != nil {
		return err
	}

	app, err := client.GetApp(newContext(), c.Provider)
	if err != nil {
		return err
	}

	if app.Hint != "" {
		out.SetHint(app.Hint)
	}

	return out.Write(configureResult{
		App:    c.Provider,
		Status: "configured",
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
	Provider     string `required:"" help:"Provider name (e.g. 'github', 'gmail')."`
	ConnectionID string `optional:"" name:"connection-id" help:"Connection ID to disconnect (required if multiple connections exist)."`
	DryRun       bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AppsDisconnectCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.Provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}

	connectionID := c.ConnectionID
	if connectionID == "" {
		connections, err := client.ListConnections(newContext(), c.Provider)
		if err != nil {
			return err
		}
		if len(connections) == 0 {
			return fmt.Errorf("no connections found for %s", c.Provider)
		}
		if len(connections) > 1 {
			out.Stderr(fmt.Sprintf("Multiple connections found for %s:", c.Provider))
			for _, conn := range connections {
				label := conn.Label
				if label == "" {
					label = conn.ID
				}
				out.Stderr(fmt.Sprintf("  %s  %s  (%s)", conn.ID, label, conn.Status))
			}
			return fmt.Errorf("specify --connection-id to disconnect a specific connection")
		}
		connectionID = connections[0].ID
	}

	if c.DryRun {
		return out.WriteDryRun("Would disconnect app", map[string]string{"provider": c.Provider, "connectionId": connectionID})
	}
	if err := client.DisconnectApp(newContext(), connectionID); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "disconnected", "provider": c.Provider, "connectionId": connectionID})
}
