package main

import (
	"encoding/json"
	"fmt"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// ProjectsCmd is the `onecli projects` command group.
type ProjectsCmd struct {
	List   ProjectsListCmd   `cmd:"" help:"List all projects."`
	Get    ProjectsGetCmd    `cmd:"" help:"Get a single project by ID."`
	Create ProjectsCreateCmd `cmd:"" help:"Create a new project."`
	Update ProjectsUpdateCmd `cmd:"" help:"Update an existing project."`
	Delete ProjectsDeleteCmd `cmd:"" help:"Delete a project."`
}

// ProjectsListCmd is `onecli projects list`.
type ProjectsListCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet  string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
	Max    int    `optional:"" default:"20" help:"Maximum number of results to return."`
}

func (c *ProjectsListCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	projects, err := client.ListProjects(newContext())
	if err != nil {
		return err
	}
	if c.Max > 0 && len(projects) > c.Max {
		projects = projects[:c.Max]
	}
	if c.Quiet != "" {
		return out.WriteQuiet(projects, c.Quiet)
	}
	return out.WriteFiltered(projects, c.Fields)
}

// ProjectsGetCmd is `onecli projects get`.
type ProjectsGetCmd struct {
	ID     string `required:"" help:"ID of the project to retrieve."`
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *ProjectsGetCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	project, err := client.GetProject(newContext(), c.ID)
	if err != nil {
		return err
	}
	return out.WriteFiltered(project, c.Fields)
}

// ProjectsCreateCmd is `onecli projects create`.
type ProjectsCreateCmd struct {
	Name   string `required:"" help:"Display name for the project."`
	Json   string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *ProjectsCreateCmd) Run(out *output.Writer) error {
	var input api.CreateProjectInput
	if c.Json != "" {
		if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	} else {
		input = api.CreateProjectInput{
			Name: c.Name,
		}
	}

	if c.DryRun {
		return out.WriteDryRun("Would create project", input)
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	project, err := client.CreateProject(newContext(), input)
	if err != nil {
		return err
	}
	return out.Write(project)
}

// ProjectsUpdateCmd is `onecli projects update`.
type ProjectsUpdateCmd struct {
	ID     string `required:"" help:"ID of the project to update."`
	Name   string `optional:"" help:"New display name."`
	Json   string `optional:"" help:"Raw JSON payload. Overrides individual flags."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *ProjectsUpdateCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}

	var input api.UpdateProjectInput
	if c.Json != "" {
		if err := json.Unmarshal([]byte(c.Json), &input); err != nil {
			return fmt.Errorf("invalid JSON payload: %w", err)
		}
	} else {
		if c.Name != "" {
			input.Name = &c.Name
		}
	}

	if c.DryRun {
		return out.WriteDryRun("Would update project", map[string]any{"id": c.ID, "input": input})
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	project, err := client.UpdateProject(newContext(), c.ID, input)
	if err != nil {
		return err
	}
	return out.Write(project)
}

// ProjectsDeleteCmd is `onecli projects delete`.
type ProjectsDeleteCmd struct {
	ID      string `required:"" help:"ID of the project to delete."`
	Confirm string `optional:"" help:"Project ID to confirm deletion. Required for destructive operation."`
	DryRun  bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *ProjectsDeleteCmd) Run(out *output.Writer) error {
	if err := validate.ResourceID(c.ID); err != nil {
		return fmt.Errorf("invalid project ID: %w", err)
	}
	if c.DryRun {
		return out.WriteDryRun("Would delete project", map[string]string{"id": c.ID})
	}

	if c.Confirm != c.ID {
		return fmt.Errorf("confirmation failed: pass --confirm %q to delete this project and all its data", c.ID)
	}

	client, err := newClient()
	if err != nil {
		return err
	}

	if err := client.DeleteProject(newContext(), c.ID); err != nil {
		return err
	}
	return out.Write(map[string]string{"status": "deleted", "id": c.ID})
}
