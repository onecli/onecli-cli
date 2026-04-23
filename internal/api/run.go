package api

import (
	"context"
	"fmt"
	"net/http"
)

// ContainerConfig is the response from GET /api/container-config.
// The server controls all env var names, values, and paths.
type ContainerConfig struct {
	Env                        map[string]string `json:"env"`
	CACertificate              string            `json:"caCertificate"`
	CACertificateContainerPath string            `json:"caCertificateContainerPath"`
}

// GetContainerConfig returns gateway configuration for a local agent process.
// agentIdentifier may be empty, in which case the server uses the default agent.
func (c *Client) GetContainerConfig(ctx context.Context, agentIdentifier string) (*ContainerConfig, error) {
	path := "/api/container-config"
	if agentIdentifier != "" {
		path += "?agent=" + agentIdentifier
	}
	var cfg ContainerConfig
	if err := c.do(ctx, http.MethodGet, path, nil, &cfg); err != nil {
		return nil, fmt.Errorf("getting container config: %w", err)
	}
	return &cfg, nil
}
