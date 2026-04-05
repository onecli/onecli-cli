package main

import (
	"encoding/json"
	"fmt"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// SecretsCmd is the `onecli secrets` command group.
type SecretsCmd struct {
	List   SecretsListCmd   `cmd:"" help:"List all secrets."`
	Create SecretsCreateCmd `cmd:"" help:"Create a new secret."`
	Update SecretsUpdateCmd `cmd:"" help:"Update an existing secret."`
	Delete SecretsDeleteCmd `cmd:"" help:"Delete a secret."`
}

// SecretsListCmd is `onecli secrets list`.
type SecretsListCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet  string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
	Max    int    `optional:"" default:"20" help:"Maximum number of results to return."`
}

func (c *SecretsListCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	secrets, err := client.ListSecrets(newContext())
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

// SecretsCreateCmd is `onecli secrets create`.
type SecretsCreateCmd struct {
	Name        string `required:"" help:"Display name for the secret."`
	Type        string `required:"" help:"Secret type: 'anthropic' or 'generic'."`
	Value       string `required:"" help:"Secret value (e.g. API key)."`
	HostPattern string `required:"" name:"host-pattern" help:"Host pattern to match (e.g. 'api.anthropic.com')."`
	PathPattern string `optional:"" name:"path-pattern" help:"Path pattern to match (e.g. '/v1/*')."`
	HeaderName  string `optional:"" name:"header-name" help:"Header name for injection (required for generic type)."`
	ValueFormat string `optional:"" name:"value-format" help:"Value format template (default: '{value}')."`
	Json        string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun      bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *SecretsCreateCmd) Run(out *output.Writer) error {
	var input api.CreateSecretInput
	if c.Json != "" {
		if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	} else {
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
		}
	}

	if input.Type != "anthropic" && input.Type != "generic" {
		return fmt.Errorf("invalid type %q: must be 'anthropic' or 'generic'", input.Type)
	}

	if c.DryRun {
		// Redact the value in dry-run output.
		preview := input
		preview.Value = "***"
		return out.WriteDryRun("Would create secret", preview)
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	secret, err := client.CreateSecret(newContext(), input)
	if err != nil {
		return err
	}
	return out.Write(secret)
}

// SecretsUpdateCmd is `onecli secrets update`.
type SecretsUpdateCmd struct {
	ID          string `required:"" help:"ID of the secret to update."`
	Value       string `optional:"" help:"New secret value."`
	HostPattern string `optional:"" name:"host-pattern" help:"New host pattern."`
	PathPattern string `optional:"" name:"path-pattern" help:"New path pattern."`
	HeaderName  string `optional:"" name:"header-name" help:"New header name for injection."`
	ValueFormat string `optional:"" name:"value-format" help:"New value format template."`
	Json        string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun      bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *SecretsUpdateCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid secret ID: %w", err)
	}

	var input api.UpdateSecretInput
	if c.Json != "" {
		if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	} else {
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
		}
	}

	if c.DryRun {
		return out.WriteDryRun("Would update secret", map[string]any{"id": c.ID, "input": input})
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.UpdateSecret(newContext(), c.ID, input); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "updated", "id": c.ID})
}

// SecretsDeleteCmd is `onecli secrets delete`.
type SecretsDeleteCmd struct {
	ID     string `required:"" help:"ID of the secret to delete."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *SecretsDeleteCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid secret ID: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would delete secret", map[string]string{"id": c.ID})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.DeleteSecret(newContext(), c.ID); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "deleted", "id": c.ID})
}

