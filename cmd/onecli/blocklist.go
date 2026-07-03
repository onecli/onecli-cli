package main

import (
	"fmt"

	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// Blocklist command runners shared by the project (`apps blocklist`) and org
// (`org apps blocklist`) groups — the org flag picks the API scope.

func runBlocklistList(out *output.Writer, provider, fields string, org bool) error {
	if err := validate.ResourceID(provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	states, err := client.GetBlocklist(newContext(), provider, org)
	if err != nil {
		return err
	}
	return out.WriteFiltered(states, fields)
}

func runBlocklistActivate(out *output.Writer, provider, hostID string, dryRun, org bool) error {
	if err := validate.ResourceID(provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	if hostID == "" {
		return fmt.Errorf("host-id must not be empty")
	}
	if dryRun {
		return out.WriteDryRun("Would activate blocklist host", map[string]string{"provider": provider, "hostId": hostID})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	result, err := client.ActivateBlocklistHost(newContext(), provider, hostID, org)
	if err != nil {
		return err
	}
	return out.Write(result)
}

func runBlocklistAdd(out *output.Writer, provider, name, hostPattern string, dryRun, org bool) error {
	if err := validate.ResourceID(provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	if name == "" || hostPattern == "" {
		return fmt.Errorf("--name and --host-pattern are required")
	}
	if dryRun {
		return out.WriteDryRun("Would add blocklist rule", map[string]string{"provider": provider, "name": name, "hostPattern": hostPattern})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	result, err := client.AddBlocklistRule(newContext(), provider, name, hostPattern, org)
	if err != nil {
		return err
	}
	return out.Write(result)
}

func runBlocklistToggle(out *output.Writer, provider, ruleID string, enabled, dryRun, org bool) error {
	if err := validate.ResourceID(provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	if err := validate.ResourceID(ruleID); err != nil {
		return fmt.Errorf("invalid rule ID: %w", err)
	}
	if dryRun {
		return out.WriteDryRun("Would toggle blocklist rule", map[string]any{"provider": provider, "ruleId": ruleID, "enabled": enabled})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.ToggleBlocklistRule(newContext(), provider, ruleID, enabled, org); err != nil {
		return err
	}
	status := "disabled"
	if enabled {
		status = "enabled"
	}
	return out.Write(map[string]string{"status": status, "ruleId": ruleID})
}

func runBlocklistRemove(out *output.Writer, provider, ruleID string, dryRun, org bool) error {
	if err := validate.ResourceID(provider); err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}
	if err := validate.ResourceID(ruleID); err != nil {
		return fmt.Errorf("invalid rule ID: %w", err)
	}
	if dryRun {
		return out.WriteDryRun("Would remove blocklist rule", map[string]string{"provider": provider, "ruleId": ruleID})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.RemoveBlocklistRule(newContext(), provider, ruleID, org); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "removed", "ruleId": ruleID})
}

// AppsBlocklistCmd is the `onecli apps blocklist` command group (project).
type AppsBlocklistCmd struct {
	List     AppsBlocklistListCmd     `cmd:"" help:"Show blocklist state for a provider."`
	Activate AppsBlocklistActivateCmd `cmd:"" help:"Activate a predefined blocklist host."`
	Add      AppsBlocklistAddCmd      `cmd:"" help:"Add a custom blocklist rule."`
	Toggle   AppsBlocklistToggleCmd   `cmd:"" help:"Enable or disable a blocklist rule."`
	Remove   AppsBlocklistRemoveCmd   `cmd:"" help:"Remove a blocklist rule."`
}

// AppsBlocklistListCmd is `onecli apps blocklist list`.
type AppsBlocklistListCmd struct {
	Provider string `required:"" help:"Provider name."`
	Fields   string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *AppsBlocklistListCmd) Run(out *output.Writer) error {
	return runBlocklistList(out, c.Provider, c.Fields, false)
}

// AppsBlocklistActivateCmd is `onecli apps blocklist activate`.
type AppsBlocklistActivateCmd struct {
	Provider string `required:"" help:"Provider name."`
	HostID   string `required:"" name:"host-id" help:"Predefined blocklist host ID."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AppsBlocklistActivateCmd) Run(out *output.Writer) error {
	return runBlocklistActivate(out, c.Provider, c.HostID, c.DryRun, false)
}

// AppsBlocklistAddCmd is `onecli apps blocklist add`.
type AppsBlocklistAddCmd struct {
	Provider    string `required:"" help:"Provider name."`
	Name        string `required:"" help:"Display name for the rule."`
	HostPattern string `required:"" name:"host-pattern" help:"Host pattern to block."`
	DryRun      bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AppsBlocklistAddCmd) Run(out *output.Writer) error {
	return runBlocklistAdd(out, c.Provider, c.Name, c.HostPattern, c.DryRun, false)
}

// AppsBlocklistToggleCmd is `onecli apps blocklist toggle`.
type AppsBlocklistToggleCmd struct {
	Provider string `required:"" help:"Provider name."`
	RuleID   string `required:"" name:"rule-id" help:"Blocklist rule ID."`
	Enabled  bool   `required:"" help:"Set to true to enable, false to disable."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AppsBlocklistToggleCmd) Run(out *output.Writer) error {
	return runBlocklistToggle(out, c.Provider, c.RuleID, c.Enabled, c.DryRun, false)
}

// AppsBlocklistRemoveCmd is `onecli apps blocklist remove`.
type AppsBlocklistRemoveCmd struct {
	Provider string `required:"" help:"Provider name."`
	RuleID   string `required:"" name:"rule-id" help:"Blocklist rule ID."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AppsBlocklistRemoveCmd) Run(out *output.Writer) error {
	return runBlocklistRemove(out, c.Provider, c.RuleID, c.DryRun, false)
}

// OrgAppsBlocklistCmd is the `onecli org apps blocklist` command group.
type OrgAppsBlocklistCmd struct {
	List     OrgAppsBlocklistListCmd     `cmd:"" help:"Show org blocklist state for a provider."`
	Activate OrgAppsBlocklistActivateCmd `cmd:"" help:"Activate a predefined blocklist host (org)."`
	Add      OrgAppsBlocklistAddCmd      `cmd:"" help:"Add a custom blocklist rule (org)."`
	Toggle   OrgAppsBlocklistToggleCmd   `cmd:"" help:"Enable or disable a blocklist rule (org)."`
	Remove   OrgAppsBlocklistRemoveCmd   `cmd:"" help:"Remove a blocklist rule (org)."`
}

// OrgAppsBlocklistListCmd is `onecli org apps blocklist list`.
type OrgAppsBlocklistListCmd struct {
	Provider string `required:"" help:"Provider name."`
	Fields   string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *OrgAppsBlocklistListCmd) Run(out *output.Writer) error {
	return runBlocklistList(out, c.Provider, c.Fields, true)
}

// OrgAppsBlocklistActivateCmd is `onecli org apps blocklist activate`.
type OrgAppsBlocklistActivateCmd struct {
	Provider string `required:"" help:"Provider name."`
	HostID   string `required:"" name:"host-id" help:"Predefined blocklist host ID."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgAppsBlocklistActivateCmd) Run(out *output.Writer) error {
	return runBlocklistActivate(out, c.Provider, c.HostID, c.DryRun, true)
}

// OrgAppsBlocklistAddCmd is `onecli org apps blocklist add`.
type OrgAppsBlocklistAddCmd struct {
	Provider    string `required:"" help:"Provider name."`
	Name        string `required:"" help:"Display name for the rule."`
	HostPattern string `required:"" name:"host-pattern" help:"Host pattern to block."`
	DryRun      bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgAppsBlocklistAddCmd) Run(out *output.Writer) error {
	return runBlocklistAdd(out, c.Provider, c.Name, c.HostPattern, c.DryRun, true)
}

// OrgAppsBlocklistToggleCmd is `onecli org apps blocklist toggle`.
type OrgAppsBlocklistToggleCmd struct {
	Provider string `required:"" help:"Provider name."`
	RuleID   string `required:"" name:"rule-id" help:"Blocklist rule ID."`
	Enabled  bool   `required:"" help:"Set to true to enable, false to disable."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgAppsBlocklistToggleCmd) Run(out *output.Writer) error {
	return runBlocklistToggle(out, c.Provider, c.RuleID, c.Enabled, c.DryRun, true)
}

// OrgAppsBlocklistRemoveCmd is `onecli org apps blocklist remove`.
type OrgAppsBlocklistRemoveCmd struct {
	Provider string `required:"" help:"Provider name."`
	RuleID   string `required:"" name:"rule-id" help:"Blocklist rule ID."`
	DryRun   bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgAppsBlocklistRemoveCmd) Run(out *output.Writer) error {
	return runBlocklistRemove(out, c.Provider, c.RuleID, c.DryRun, true)
}
