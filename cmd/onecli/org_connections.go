package main

import (
	"fmt"

	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// OrgConnectionsCmd is the `onecli org connections` command group.
type OrgConnectionsCmd struct {
	List   OrgConnectionsListCmd   `cmd:"" help:"List all org-scoped connections."`
	Delete OrgConnectionsDeleteCmd `cmd:"" help:"Delete an org-scoped connection."`
}

// OrgConnectionsListCmd is `onecli org connections list`.
type OrgConnectionsListCmd struct {
	Provider string `optional:"" help:"Filter by provider name (e.g. 'github', 'gmail')."`
	Fields   string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet    string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
	Max      int    `optional:"" default:"20" help:"Maximum number of results to return."`
}

func (c *OrgConnectionsListCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}

	if c.Provider != "" {
		if err := validate.ResourceID(c.Provider); err != nil {
			return fmt.Errorf("invalid provider: %w", err)
		}
		connections, err := client.ListOrgConnectionsByProvider(newContext(), c.Provider)
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

	connections, err := client.ListOrgConnections(newContext())
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

// OrgConnectionsDeleteCmd is `onecli org connections delete`.
type OrgConnectionsDeleteCmd struct {
	ID     string `required:"" help:"ID of the connection to delete."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgConnectionsDeleteCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid connection ID: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would delete org connection", map[string]string{"id": c.ID})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	if err := client.DeleteOrgConnection(newContext(), c.ID); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "deleted", "id": c.ID})
}
