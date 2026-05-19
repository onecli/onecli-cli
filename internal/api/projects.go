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

// UpdateProjectInput is the request body for updating a project.
type UpdateProjectInput struct {
	Name *string `json:"name,omitempty"`
}

// ListProjects returns all projects for the authenticated user.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var projects []Project
	if err := c.do(ctx, http.MethodGet, "/v1/projects", nil, &projects); err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	return projects, nil
}

// GetProject returns a single project by ID.
func (c *Client) GetProject(ctx context.Context, id string) (*Project, error) {
	var project Project
	if err := c.do(ctx, http.MethodGet, "/v1/projects/"+id, nil, &project); err != nil {
		return nil, fmt.Errorf("getting project: %w", err)
	}
	return &project, nil
}

// CreateProject creates a new project.
func (c *Client) CreateProject(ctx context.Context, input CreateProjectInput) (*Project, error) {
	var project Project
	if err := c.do(ctx, http.MethodPost, "/v1/projects", input, &project); err != nil {
		return nil, fmt.Errorf("creating project: %w", err)
	}
	return &project, nil
}

// UpdateProject updates an existing project and returns the updated project.
func (c *Client) UpdateProject(ctx context.Context, id string, input UpdateProjectInput) (*Project, error) {
	var project Project
	if err := c.do(ctx, http.MethodPatch, "/v1/projects/"+id, input, &project); err != nil {
		return nil, fmt.Errorf("updating project: %w", err)
	}
	return &project, nil
}

// DeleteProject deletes a project by ID.
func (c *Client) DeleteProject(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/v1/projects/"+id, nil, nil); err != nil {
		return fmt.Errorf("deleting project: %w", err)
	}
	return nil
}
