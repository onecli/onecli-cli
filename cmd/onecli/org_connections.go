package main

import (
	"fmt"

	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// OrgConnectionsCmd is the `onecli org connections` command group.
type OrgConnectionsCmd struct {
	List   OrgConnectionsListCmd   `cmd:"" help:"List all org-scoped connections."`
	Rename OrgConnectionsRenameCmd `cmd:"" help:"Rename an org-scoped connection."`
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
	if c.Provider != "" {
		if err := validate.ResourceID(c.Provider); err != nil {
			return fmt.Errorf("invalid provider: %w", err)
		}
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	connections, err := client.ListOrgConnections(newContext(), c.Provider)
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

// OrgConnectionsRenameCmd is `onecli org connections rename`.
type OrgConnectionsRenameCmd struct {
	ID     string `required:"" help:"ID of the connection to rename."`
	Label  string `required:"" help:"New display label."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *OrgConnectionsRenameCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid connection ID: %w", err)
	}
	if c.Label == "" {
		return fmt.Errorf("label must not be empty")
	}
	if c.DryRun {
		return out.WriteDryRun("Would rename org connection", map[string]string{"id": c.ID, "label": c.Label})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	conn, err := client.RenameOrgConnection(newContext(), c.ID, c.Label)
	if err != nil {
		return err
	}
	return out.Write(conn)
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
