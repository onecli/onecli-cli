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
func (c *Client) ListAgents(ctx context.Context) ([]Agent, error) {
	var agents []Agent
	if err := c.do(ctx, http.MethodGet, "/api/agents", nil, &agents); err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}
	return agents, nil
}

// GetDefaultAgent returns the user's default agent.
func (c *Client) GetDefaultAgent(ctx context.Context) (*Agent, error) {
	var agent Agent
	if err := c.do(ctx, http.MethodGet, "/api/agents/default", nil, &agent); err != nil {
		return nil, fmt.Errorf("getting default agent: %w", err)
	}
	return &agent, nil
}

// CreateAgent creates a new agent.
func (c *Client) CreateAgent(ctx context.Context, input CreateAgentInput) (*Agent, error) {
	var agent Agent
	if err := c.do(ctx, http.MethodPost, "/api/agents", input, &agent); err != nil {
		return nil, fmt.Errorf("creating agent: %w", err)
	}
	return &agent, nil
}

// DeleteAgent deletes an agent by ID.
func (c *Client) DeleteAgent(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/agents/"+id, nil, nil); err != nil {
		return fmt.Errorf("deleting agent: %w", err)
	}
	return nil
}

// RenameAgent renames an agent.
func (c *Client) RenameAgent(ctx context.Context, id string, input RenameAgentInput) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPatch, "/api/agents/"+id, input, &resp); err != nil {
		return fmt.Errorf("renaming agent: %w", err)
	}
	return nil
}

// RegenerateAgentToken regenerates an agent's access token.
func (c *Client) RegenerateAgentToken(ctx context.Context, id string) (*RegenerateTokenResponse, error) {
	var resp RegenerateTokenResponse
	if err := c.do(ctx, http.MethodPost, "/api/agents/"+id+"/regenerate-token", nil, &resp); err != nil {
		return nil, fmt.Errorf("regenerating agent token: %w", err)
	}
	return &resp, nil
}

// GetAgentSecrets returns the secret IDs assigned to an agent.
func (c *Client) GetAgentSecrets(ctx context.Context, id string) ([]string, error) {
	var secretIDs []string
	if err := c.do(ctx, http.MethodGet, "/api/agents/"+id+"/secrets", nil, &secretIDs); err != nil {
		return nil, fmt.Errorf("getting agent secrets: %w", err)
	}
	return secretIDs, nil
}

// SetAgentSecrets replaces an agent's secret assignments.
func (c *Client) SetAgentSecrets(ctx context.Context, id string, input SetAgentSecretsInput) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPut, "/api/agents/"+id+"/secrets", input, &resp); err != nil {
		return fmt.Errorf("setting agent secrets: %w", err)
	}
	return nil
}

// SetAgentSecretMode updates an agent's secret mode.
func (c *Client) SetAgentSecretMode(ctx context.Context, id string, input SetSecretModeInput) error {
	var resp SuccessResponse
	if err := c.do(ctx, http.MethodPatch, "/api/agents/"+id+"/secret-mode", input, &resp); err != nil {
		return fmt.Errorf("setting agent secret mode: %w", err)
	}
	return nil
}
