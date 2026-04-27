package api

import "context"

// HealthResponse is the response from /api/health.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// GetHealth calls the /api/health endpoint.
func (c *Client) GetHealth(ctx context.Context) (*HealthResponse, error) {
	var resp HealthResponse
	if err := c.do(ctx, "GET", "/api/health", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
