package api

import (
	"context"
	"fmt"
	"net/http"
)

// Project represents a project returned by the API.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"createdAt"`
}

// CreateProjectInput is the request body for creating a project.
type CreateProjectInput struct {
	Name string `json:"name"`
}

// ListProjects returns all projects for the authenticated user.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var projects []Project
	if err := c.do(ctx, http.MethodGet, "/api/projects", nil, &projects); err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	return projects, nil
}

// CreateProject creates a new project.
func (c *Client) CreateProject(ctx context.Context, input CreateProjectInput) (*Project, error) {
	var project Project
	if err := c.do(ctx, http.MethodPost, "/api/projects", input, &project); err != nil {
		return nil, fmt.Errorf("creating project: %w", err)
	}
	return &project, nil
}

// DeleteProject deletes a project by ID.
func (c *Client) DeleteProject(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/projects/"+id, nil, nil); err != nil {
		return fmt.Errorf("deleting project: %w", err)
	}
	return nil
}
