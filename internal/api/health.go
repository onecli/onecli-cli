package api

import "context"

// HealthResponse is the response from /v1/health.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// GetHealth calls the /v1/health endpoint.
func (c *Client) GetHealth(ctx context.Context) (*HealthResponse, error) {
	var resp HealthResponse
	if err := c.do(ctx, "GET", "/v1/health", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
