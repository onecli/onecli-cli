package api

import (
	"context"
	"fmt"
	"net/http"
)

// Agent represents an agent returned by the API.
type Agent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Identifier  string `json:"identifier"`
	AccessToken string `json:"accessToken,omitempty"`
	IsDefault   bool   `json:"isDefault"`
	SecretMode  string `json:"secretMode,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

// CreateAgentInput is the request body for creating an agent.
type CreateAgentInput struct {
	Name       string `json:"name"`
	Identifier string `json:"identifier"`
}

// RenameAgentInput is the request body for renaming an agent.
type RenameAgentInput struct {
	Name string `json:"name"`
}

// SetSecretModeInput is the request body for updating an agent's secret mode.
type SetSecretModeInput struct {
	Mode string `json:"mode"`
}

// SetAgentSecretsInput is the request body for updating an agent's secrets.
type SetAgentSecretsInput struct {
	SecretIDs []string `json:"secretIds"`
}

// RegenerateTokenResponse is the response from regenerating an agent token.
type RegenerateTokenResponse struct {
	AccessToken string `json:"accessToken"`
}

// SuccessResponse is a generic success response.
type SuccessResponse struct {
	Success bool `json:"success"`
}

// ListAgents returns all agents for the authenticated user.
// If projectID is non-empty, results are scoped to that project.
func (c *Client) ListAgents(ctx context.Context, projectID string) ([]Agent, error) {
	var agents []Agent
	if err := c.doProject(ctx, http.MethodGet, "/v1/agents", projectID, nil, &agents); err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}
	return agents, nil
}

// GetDefaultAgent returns the user's default agent.
func (c *Client) GetDefaultAgent(ctx context.Context) (*Agent, error) {
	var agent Agent
	if err := c.do(ctx, http.MethodGet, "/v1/agents/default", nil, &agent); err != nil {
		return nil, fmt.Errorf("getting default agent: %w", err)
	}
	return &agent, nil
}

// CreateAgent creates a new agent.
// If projectID is non-empty, the agent is created in that project.
func (c *Client) CreateAgent(ctx context.Context, projectID string, input CreateAgentInput) (*Agent, error) {
	var agent Agent
	if err := c.doProject(ctx, http.MethodPost, "/v1/agents", projectID, input, &agent); err != nil {
		return nil, fmt.Errorf("creating agent: %w", err)
	}
	return &agent, nil
}

// DeleteAgent deletes an agent by ID.
func (c *Client) DeleteAgent(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/v1/agents/"+id, nil, nil); err != nil {
		return fmt.Errorf("deleting agent: %w", err)
	}
	return nil
}

// RenameAgent renames an agent.
func (c *Client) RenameAgent(ctx context.Context, id string, input RenameAgentInput) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPatch, "/v1/agents/"+id, input, &resp); err != nil {
		return fmt.Errorf("renaming agent: %w", err)
	}
	return nil
}

// RegenerateAgentToken regenerates an agent's access token.
func (c *Client) RegenerateAgentToken(ctx context.Context, id string) (*RegenerateTokenResponse, error) {
	var resp RegenerateTokenResponse
	if err := c.do(ctx, http.MethodPost, "/v1/agents/"+id+"/regenerate-token", nil, &resp); err != nil {
		return nil, fmt.Errorf("regenerating agent token: %w", err)
	}
	return &resp, nil
}

// GetAgentSecrets returns the secret IDs assigned to an agent.
func (c *Client) GetAgentSecrets(ctx context.Context, id string) ([]string, error) {
	var secretIDs []string
	if err := c.do(ctx, http.MethodGet, "/v1/agents/"+id+"/secrets", nil, &secretIDs); err != nil {
		return nil, fmt.Errorf("getting agent secrets: %w", err)
	}
	return secretIDs, nil
}

// SetAgentSecrets replaces an agent's secret assignments.
func (c *Client) SetAgentSecrets(ctx context.Context, id string, input SetAgentSecretsInput) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPut, "/v1/agents/"+id+"/secrets", input, &resp); err != nil {
		return fmt.Errorf("setting agent secrets: %w", err)
	}
	return nil
}

// SetAgentSecretMode updates an agent's secret mode.
func (c *Client) SetAgentSecretMode(ctx context.Context, id string, input SetSecretModeInput) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPatch, "/v1/agents/"+id+"/secret-mode", input, &resp); err != nil {
		return fmt.Errorf("setting agent secret mode: %w", err)
	}
	return nil
}

// SetDefaultAgent marks an agent as the project default.
func (c *Client) SetDefaultAgent(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodPost, "/v1/agents/"+id+"/set-default", nil, nil); err != nil {
		return fmt.Errorf("setting default agent: %w", err)
	}
	return nil
}

// ListGranularAccess returns the per-agent granular-access overview
// (GitHub repos, Dropbox folders) across the project.
func (c *Client) ListGranularAccess(ctx context.Context) (any, error) {
	var entries any
	if err := c.do(ctx, http.MethodGet, "/v1/agents/granular-access", nil, &entries); err != nil {
		return nil, fmt.Errorf("listing granular access: %w", err)
	}
	return entries, nil
}

// GetAgentConnections returns an agent's app-connection assignments and
// per-connection granular-access policies.
func (c *Client) GetAgentConnections(ctx context.Context, id string) (any, error) {
	var connections any
	if err := c.do(ctx, http.MethodGet, "/v1/agents/"+id+"/connections", nil, &connections); err != nil {
		return nil, fmt.Errorf("getting agent connections: %w", err)
	}
	return connections, nil
}

// SetAgentConnections replaces an agent's app-connection assignments (and
// their granular-access policies). connections is the raw `connections`
// array as the API defines it — agent callers pass it via --json.
func (c *Client) SetAgentConnections(ctx context.Context, id string, connections []any) error {
	var resp SuccessResponse
	body := map[string]any{"connections": connections}
	if err := c.do(ctx, http.MethodPut, "/v1/agents/"+id+"/connections", body, &resp); err != nil {
		return fmt.Errorf("setting agent connections: %w", err)
	}
	return nil
}
