package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// The attach-model grants surface: a grant attaches one credential (an app
// connection, secret, or LLM key) to one agent, and is the only project-scope
// policy writer — the server compiles grants into policy rules. Grants are
// the attach INTENT; the effective view (org guardrails applied) stays with
// 'agents credentials' / 'apps connections agent-access'.

// AgentGrantConnection is one attached app connection with its per-tool access.
type AgentGrantConnection struct {
	ConnectionID string   `json:"connectionId"`
	Provider     string   `json:"provider"`
	Label        *string  `json:"label"`
	Scope        string   `json:"scope"`  // "project" | "organization"
	Access       string   `json:"access"` // "full" | "custom"
	Allow        []string `json:"allow"`
	Ask          []string `json:"ask"`
}

// AgentGrantSecret is one attached secret or LLM key.
type AgentGrantSecret struct {
	SecretID string `json:"secretId"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Scope    string `json:"scope"`
}

// AgentGrants is an agent's full grant set — returned by reads AND mutations
// (the server answers every grant write with the agent's fresh state).
type AgentGrants struct {
	AgentID     string                 `json:"agentId"`
	Mode        string                 `json:"mode"` // "grants" on current servers; "all" only from older ones
	Connections []AgentGrantConnection `json:"connections"`
	Secrets     []AgentGrantSecret     `json:"secrets"`
}

// ConnectionGrantAgent is one agent granted a connection (the reverse view).
type ConnectionGrantAgent struct {
	AgentID string   `json:"agentId"`
	Access  string   `json:"access"`
	Allow   []string `json:"allow"`
	Ask     []string `json:"ask"`
}

// ConnectionGrants lists which agents a connection is granted to.
type ConnectionGrants struct {
	ConnectionID string                 `json:"connectionId"`
	Agents       []ConnectionGrantAgent `json:"agents"`
}

// ConnectionGrantInput describes the desired connection grant.
// Access "full" ignores the tool lists; "custom" uses them (each tool is
// always-allowed via Allow or approval-gated via Ask; the rest is blocked).
type ConnectionGrantInput struct {
	Access string
	Allow  []string
	Ask    []string
}

// fullGrantBody is the uncustomized attach: exactly {"access":"full"}.
type fullGrantBody struct {
	Access string `json:"access"`
}

// customGrantBody must carry BOTH arrays — the server's schema requires the
// keys, and rejects null where it expects a list, so empties stay [].
type customGrantBody struct {
	Access string   `json:"access"`
	Allow  []string `json:"allow"`
	Ask    []string `json:"ask"`
}

// GrantsSummaryEntry is one attached credential in the summary projection.
// Kind selects the arms: "app" carries Provider/ConnectionID/Label;
// "secret" and "llm" carry ID/Name.
type GrantsSummaryEntry struct {
	Kind         string  `json:"kind"`
	Provider     string  `json:"provider,omitempty"`
	ConnectionID string  `json:"connectionId,omitempty"`
	Label        *string `json:"label,omitempty"`
	ID           string  `json:"id,omitempty"`
	Name         string  `json:"name,omitempty"`
}

// AgentGrantsSummary is the attach-list summary for one agent.
type AgentGrantsSummary struct {
	Mode    string               `json:"mode"`
	Entries []GrantsSummaryEntry `json:"entries"`
	Total   int                  `json:"total"`
}

// AgentWithGrantsSummary is an agent plus its grants summary
// (GET /v1/agents?include=grants-summary).
type AgentWithGrantsSummary struct {
	Agent
	GrantsSummary AgentGrantsSummary `json:"grantsSummary"`
}

// GetAgentGrants returns the agent's grants: attached connections (with
// per-tool access) and secrets.
func (c *Client) GetAgentGrants(ctx context.Context, project, agentID string) (*AgentGrants, error) {
	path := "/v1/agents/" + url.PathEscape(agentID) + "/grants"
	var result AgentGrants
	if err := c.doProject(ctx, http.MethodGet, path, project, nil, &result); err != nil {
		return nil, fmt.Errorf("getting agent grants: %w", err)
	}
	return &result, nil
}

// SetAgentConnectionGrant attaches an app connection to the agent (or replaces
// its per-tool access). Returns the agent's fresh grant set.
func (c *Client) SetAgentConnectionGrant(ctx context.Context, project, agentID, connectionID string, input ConnectionGrantInput) (*AgentGrants, error) {
	path := "/v1/agents/" + url.PathEscape(agentID) + "/grants/connections/" + url.PathEscape(connectionID)
	var result AgentGrants
	if err := c.doProject(ctx, http.MethodPut, path, project, normalizeGrantBody(input), &result); err != nil {
		return nil, fmt.Errorf("attaching connection: %w", err)
	}
	return &result, nil
}

// RemoveAgentConnectionGrant detaches an app connection from the agent.
// The server answers 204 with no body.
func (c *Client) RemoveAgentConnectionGrant(ctx context.Context, project, agentID, connectionID string) error {
	path := "/v1/agents/" + url.PathEscape(agentID) + "/grants/connections/" + url.PathEscape(connectionID)
	if err := c.doProject(ctx, http.MethodDelete, path, project, nil, nil); err != nil {
		return fmt.Errorf("detaching connection: %w", err)
	}
	return nil
}

// SetAgentSecretGrant attaches a secret (or LLM key) to the agent. The PUT
// takes no body — a secret grant is attach-only, with no per-tool axis.
func (c *Client) SetAgentSecretGrant(ctx context.Context, project, agentID, secretID string) (*AgentGrants, error) {
	path := "/v1/agents/" + url.PathEscape(agentID) + "/grants/secrets/" + url.PathEscape(secretID)
	var result AgentGrants
	if err := c.doProject(ctx, http.MethodPut, path, project, nil, &result); err != nil {
		return nil, fmt.Errorf("attaching secret: %w", err)
	}
	return &result, nil
}

// RemoveAgentSecretGrant detaches a secret from the agent (204, no body).
func (c *Client) RemoveAgentSecretGrant(ctx context.Context, project, agentID, secretID string) error {
	path := "/v1/agents/" + url.PathEscape(agentID) + "/grants/secrets/" + url.PathEscape(secretID)
	if err := c.doProject(ctx, http.MethodDelete, path, project, nil, nil); err != nil {
		return fmt.Errorf("detaching secret: %w", err)
	}
	return nil
}

// GetConnectionGrants returns which agents a connection is granted to (the
// attach intent; 'apps connections agent-access' is the effective view).
func (c *Client) GetConnectionGrants(ctx context.Context, project, connectionID string) (*ConnectionGrants, error) {
	path := "/v1/connections/" + url.PathEscape(connectionID) + "/grants"
	var result ConnectionGrants
	if err := c.doProject(ctx, http.MethodGet, path, project, nil, &result); err != nil {
		return nil, fmt.Errorf("getting connection grants: %w", err)
	}
	return &result, nil
}

// ListAgentsWithGrantsSummary lists agents with each one's attach summary.
func (c *Client) ListAgentsWithGrantsSummary(ctx context.Context, project string) ([]AgentWithGrantsSummary, error) {
	var agents []AgentWithGrantsSummary
	if err := c.doProject(ctx, http.MethodGet, "/v1/agents?include=grants-summary", project, nil, &agents); err != nil {
		return nil, fmt.Errorf("listing agents with grants summary: %w", err)
	}
	return agents, nil
}

// normalizeGrantBody shapes the PUT body exactly as the server's schema
// expects: "full" is the bare {"access":"full"}; "custom" carries both allow
// and ask keys as arrays (a nil Go slice marshals to JSON null — normalized
// to [] here).
func normalizeGrantBody(input ConnectionGrantInput) any {
	if input.Access != "custom" {
		return fullGrantBody{Access: input.Access}
	}
	body := customGrantBody{Access: "custom", Allow: input.Allow, Ask: input.Ask}
	if body.Allow == nil {
		body.Allow = []string{}
	}
	if body.Ask == nil {
		body.Ask = []string{}
	}
	return body
}
