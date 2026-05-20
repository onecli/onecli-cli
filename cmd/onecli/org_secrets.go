package main

import (
	"encoding/json"
	"fmt"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// OrgSecretsCmd is the `onecli org secrets` command group.
type OrgSecretsCmd struct {
	List   OrgSecretsListCmd   `cmd:"" help:"List all org-scoped secrets."`
	Create OrgSecretsCreateCmd `cmd:"" help:"Create a new org-scoped secret."`
	Update OrgSecretsUpdateCmd `cmd:"" help:"Update an org-scoped secret."`
	Delete OrgSecretsDeleteCmd `cmd:"" help:"Delete an org-scoped secret."`
}

// OrgSecretsListCmd is `onecli org secrets list`.
type OrgSecretsListCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet  string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
	Max    int    `optional:"" default:"20" help:"Maximum number of results to return."`
}

func (c *OrgSecretsListCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	secrets, err := client.ListOrgSecrets(newContext())
	if err != nil {
		return err
	}
	if c.Max > 0 && len(secrets) > c.Max {
		secrets = secrets[:c.Max]
	}
	if c.Quiet != "" {
		return out.WriteQuiet(secrets, c.Quiet)
	}
	return out.WriteFiltered(secrets, c.Fields)
}

// OrgSecretsCreateCmd is `onecli org secrets create`.
type OrgSecretsCreateCmd struct {
	Name        string `required:"" help:"Display name for the secret."`
	Type        string `required:"" help:"Secret type: 'anthropic', 'openai', or 'generic'."`
	Value       string `required:"" help:"Secret value (e.g. API key)."`
	HostPattern string `required:"" name:"host-pattern" help:"Host pattern to match (e.g. 'api.anthropic.com')."`
	PathPattern string `optional:"" name:"path-pattern" help:"Path pattern to match (e.g. '/v1/*')."`
	HeaderName  string `optional:"" name:"header-name" help:"Header name for injection (e.g. 'Authorization')."`
	ValueFormat string `optional:"" name:"value-format" help:"Value format template for header injection (default: '{value}')."`
	ParamName   string `optional:"" name:"param-name" help:"URL query parameter name for injection (e.g. 'key')."`
	ParamFormat string `optional:"" name:"param-format" help:"Value format template for param injection (default: '{value}')."`
	Json        string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun      bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgSecretsCreateCmd) Run(out *output.Writer) error {
	var input api.CreateSecretInput
	if c.Json != "" {
		if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	} else {
		if c.HeaderName != "" && c.ParamName != "" {
			return fmt.Errorf("--header-name and --param-name are mutually exclusive")
		}
		input = api.CreateSecretInput{
			Name:        c.Name,
			Type:        c.Type,
			Value:       c.Value,
			HostPattern: c.HostPattern,
			PathPattern: c.PathPattern,
		}
		if c.HeaderName != "" {
			input.InjectionConfig = &api.InjectionConfig{
				HeaderName:  c.HeaderName,
				ValueFormat: c.ValueFormat,
			}
		} else if c.ParamName != "" {
			input.InjectionConfig = &api.InjectionConfig{
				ParamName:   c.ParamName,
				ParamFormat: c.ParamFormat,
			}
		}
	}

	if input.Type != "anthropic" && input.Type != "openai" && input.Type != "generic" {
		return fmt.Errorf("invalid type %q: must be 'anthropic', 'openai', or 'generic'", input.Type)
	}

	if c.DryRun {
		preview := input
		preview.Value = "***"
		return out.WriteDryRun("Would create org secret", preview)
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	secret, err := client.CreateOrgSecret(newContext(), input)
	if err != nil {
		return err
	}
	return out.Write(secret)
}

// OrgSecretsUpdateCmd is `onecli org secrets update`.
type OrgSecretsUpdateCmd struct {
	ID          string `required:"" help:"ID of the secret to update."`
	Value       string `optional:"" help:"New secret value."`
	HostPattern string `optional:"" name:"host-pattern" help:"New host pattern."`
	PathPattern string `optional:"" name:"path-pattern" help:"New path pattern."`
	HeaderName  string `optional:"" name:"header-name" help:"New header name for injection."`
	ValueFormat string `optional:"" name:"value-format" help:"New value format template for header injection."`
	ParamName   string `optional:"" name:"param-name" help:"New URL query parameter name for injection."`
	ParamFormat string `optional:"" name:"param-format" help:"New value format template for param injection."`
	Json        string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun      bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgSecretsUpdateCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid secret ID: %w", err)
	}

	var input api.UpdateSecretInput
	if c.Json != "" {
		if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	} else {
		if c.HeaderName != "" && c.ParamName != "" {
			return fmt.Errorf("--header-name and --param-name are mutually exclusive")
		}
		if c.Value != "" {
			input.Value = &c.Value
		}
		if c.HostPattern != "" {
			input.HostPattern = &c.HostPattern
		}
		if c.PathPattern != "" {
			input.PathPattern = &c.PathPattern
		}
		if c.HeaderName != "" {
			input.InjectionConfig = &api.InjectionConfig{
				HeaderName:  c.HeaderName,
				ValueFormat: c.ValueFormat,
			}
		} else if c.ParamName != "" {
			input.InjectionConfig = &api.InjectionConfig{
				ParamName:   c.ParamName,
				ParamFormat: c.ParamFormat,
			}
		}
	}

	if c.DryRun {
		return out.WriteDryRun("Would update org secret", map[string]any{"id": c.ID, "input": input})
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.UpdateOrgSecret(newContext(), c.ID, input); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "updated", "id": c.ID})
}

// OrgSecretsDeleteCmd is `onecli org secrets delete`.
type OrgSecretsDeleteCmd struct {
	ID     string `required:"" help:"ID of the secret to delete."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgSecretsDeleteCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid secret ID: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would delete org secret", map[string]string{"id": c.ID})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.DeleteOrgSecret(newContext(), c.ID); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "deleted", "id": c.ID})
}
