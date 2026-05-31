package main

import (
	"fmt"

	"github.com/onecli/onecli-cli/internal/config"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// MigrateCmd is `onecli migrate`.
type MigrateCmd struct {
	CloudKey string `required:"" name:"cloud-key" help:"OneCLI Cloud API key."`
	CloudUrl string `optional:"" name:"cloud-url" help:"OneCLI Cloud API URL (default: production)."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *MigrateCmd) Run(out *output.Writer) error {
	if err := validate.APIKey(c.CloudKey); err != nil {
		return fmt.Errorf("invalid cloud API key: %w", err)
	}
	if c.CloudUrl != "" {
		if err := validate.URL(c.CloudUrl); err != nil {
			return fmt.Errorf("invalid cloud URL: %w", err)
		}
	}

	if c.DryRun {
		target := "https://api.onecli.sh"
		if c.CloudUrl != "" {
			target = c.CloudUrl
		}
		return out.WriteDryRun("Would migrate data to OneCLI Cloud", map[string]string{
			"source": config.APIHost(),
			"target": target,
		})
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	result, err := client.MigrateToCloud(newContext(), c.CloudKey, c.CloudUrl)
	if err != nil {
		return err
	}
	return out.Write(result)
}
