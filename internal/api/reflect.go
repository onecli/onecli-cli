package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Read-only policy reflections.
//
// These report what the PUBLISHED policy actually allows. They replace the
// retired equipment/permission reads (`/v1/rules/permissions/:provider`,
// `/v1/agents/:id/{secrets,connections}`, `/v1/agents/granular-access`,
// `/v1/connections/:id/agents`), which returned stored assignment lists — a
// credential granted by a policy rule was invisible to them. Resolving the
// rules instead means it is not.

// EffectiveProvenance attributes a verdict to the rule that decided it. An org
// rule's name is visible only to org admins; other viewers get Redacted with
// the name withheld.
type EffectiveProvenance struct {
	Scope    string `json:"scope"`
	Redacted bool   `json:"redacted,omitempty"`
	Rule     *struct {
		LogicalID string `json:"logicalId"`
		Name      string `json:"name"`
	} `json:"rule,omitempty"`
}

// EffectiveTool is one tool's verdict under the published policy. Verdict is
// one of allowed | blocked | needsApproval | rateLimited | unmanaged; a group
// rollup may also read "mixed".
type EffectiveTool struct {
	ToolID          string               `json:"toolId"`
	Verdict         string               `json:"verdict"`
	RateLimit       *int                 `json:"rateLimit"`
	RateLimitWindow *string              `json:"rateLimitWindow"`
	DecidedBy       *EffectiveProvenance `json:"decidedBy"`
}

type EffectiveToolGroup struct {
	Category string          `json:"category"`
	Verdict  string          `json:"verdict"`
	Tools    []EffectiveTool `json:"tools"`
}

// EffectiveAppPermissions is the per-tool reflection for one provider.
type EffectiveAppPermissions struct {
	Provider string `json:"provider"`
	Basis    struct {
		AgentID            *string `json:"agentId"`
		CredentialAttached bool    `json:"credentialAttached"`
		Scope              string  `json:"scope"`
	} `json:"basis"`
	// Identity-scoped rules the agent-less baseline cannot show.
	VariesByIdentity int                  `json:"variesByIdentity"`
	Groups           []EffectiveToolGroup `json:"groups"`
}

// CredentialProvenance explains why a credential reaches an agent.
type CredentialProvenance struct {
	Kind     string `json:"kind"`
	Scope    string `json:"scope"`
	Redacted bool   `json:"redacted,omitempty"`
	Rule     *struct {
		LogicalID string `json:"logicalId"`
		Name      string `json:"name"`
	} `json:"rule,omitempty"`
}

// EffectiveCredential is one secret or connection the agent can use. Kind
// selects which of Name/Host (secret) or Label/Provider (connection) is set.
type EffectiveCredential struct {
	Kind       string                 `json:"kind"`
	ID         string                 `json:"id"`
	Name       string                 `json:"name,omitempty"`
	Host       string                 `json:"host,omitempty"`
	Label      *string                `json:"label,omitempty"`
	Provider   string                 `json:"provider,omitempty"`
	Status     string                 `json:"status"`
	Provenance []CredentialProvenance `json:"provenance"`
}

// EffectiveCredentials is an agent's usable credential set. Mode "all" draws
// the whole fenced pool; "selective" gets only what rules grant.
type EffectiveCredentials struct {
	AgentID     string                `json:"agentId"`
	Mode        string                `json:"mode"`
	Secrets     []EffectiveCredential `json:"secrets"`
	Connections []EffectiveCredential `json:"connections"`
}

// ConnectionAgent is one agent's effective access to a connection.
type ConnectionAgent struct {
	AgentID    string `json:"agentId"`
	Name       string `json:"name"`
	Access     string `json:"access"`
	Credential struct {
		Status     string                 `json:"status"`
		Provenance []CredentialProvenance `json:"provenance,omitempty"`
	} `json:"credential"`
	Decisions *struct {
		AllowedTools int  `json:"allowedTools"`
		TotalTools   int  `json:"totalTools"`
		AnyApproval  bool `json:"anyApproval"`
		AnyRateLimit bool `json:"anyRateLimit"`
	} `json:"decisions"`
}

// ConnectionAgentAccess lists which agents reach a connection. Catalog false
// means the provider has no tool catalog, so Decisions is absent by honesty.
type ConnectionAgentAccess struct {
	ConnectionID string            `json:"connectionId"`
	Provider     string            `json:"provider"`
	Catalog      bool              `json:"catalog"`
	Agents       []ConnectionAgent `json:"agents"`
}

// AppPermissionDefinition is a provider's public tool catalog — the tool ids an
// app-target policy rule can name. The endpoint mapping never leaves the server.
type AppPermissionDefinition struct {
	Provider string `json:"provider"`
	Groups   []struct {
		Category string `json:"category"`
		Tools    []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
		} `json:"tools"`
	} `json:"groups"`
}

// GetEffectiveAppPermissions returns the project-scope per-tool reflection for
// a provider. A non-empty agentID narrows it to that agent's identity.
func (c *Client) GetEffectiveAppPermissions(ctx context.Context, project, provider, agentID string) (*EffectiveAppPermissions, error) {
	path := "/v1/policy/effective-app-permissions?provider=" + url.QueryEscape(provider)
	if agentID != "" {
		path += "&agentId=" + url.QueryEscape(agentID)
	}
	var result EffectiveAppPermissions
	if err := c.doProject(ctx, http.MethodGet, path, project, nil, &result); err != nil {
		return nil, fmt.Errorf("getting effective app permissions: %w", err)
	}
	return &result, nil
}

// GetOrgEffectiveAppPermissions is the org-scope twin. Org admins only.
func (c *Client) GetOrgEffectiveAppPermissions(ctx context.Context, provider string) (*EffectiveAppPermissions, error) {
	path := "/v1/org/policy/effective-app-permissions?provider=" + url.QueryEscape(provider)
	var result EffectiveAppPermissions
	if err := c.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, fmt.Errorf("getting org effective app permissions: %w", err)
	}
	return &result, nil
}

// GetEffectiveCredentials returns which credentials an agent can use.
func (c *Client) GetEffectiveCredentials(ctx context.Context, project, agentID string) (*EffectiveCredentials, error) {
	path := "/v1/agents/" + url.PathEscape(agentID) + "/effective-credentials"
	var result EffectiveCredentials
	if err := c.doProject(ctx, http.MethodGet, path, project, nil, &result); err != nil {
		return nil, fmt.Errorf("getting effective credentials: %w", err)
	}
	return &result, nil
}

// GetConnectionAgentAccess returns which agents can reach a connection.
func (c *Client) GetConnectionAgentAccess(ctx context.Context, project, connectionID string) (*ConnectionAgentAccess, error) {
	path := "/v1/connections/" + url.PathEscape(connectionID) + "/effective-agents"
	var result ConnectionAgentAccess
	if err := c.doProject(ctx, http.MethodGet, path, project, nil, &result); err != nil {
		return nil, fmt.Errorf("getting connection agent access: %w", err)
	}
	return &result, nil
}

// ListAppPermissionDefinitions returns every provider's public tool catalog.
// Global data — no project context required.
func (c *Client) ListAppPermissionDefinitions(ctx context.Context) ([]AppPermissionDefinition, error) {
	var defs []AppPermissionDefinition
	if err := c.do(ctx, http.MethodGet, "/v1/apps/permission-definitions", nil, &defs); err != nil {
		return nil, fmt.Errorf("listing app permission definitions: %w", err)
	}
	return defs, nil
}
