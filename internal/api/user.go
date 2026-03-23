package api

import (
	"context"
	"fmt"
	"net/http"
)

// User represents the authenticated user.
type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

// APIKeyResponse is the response from the API key endpoint.
type APIKeyResponse struct {
	APIKey string `json:"apiKey"`
}

// GetUser returns the authenticated user's profile.
func (c *Client) GetUser(ctx context.Context) (*User, error) {
	var user User
	if err := c.do(ctx, http.MethodGet, "/api/user", nil, &user); err != nil {
		return nil, fmt.Errorf("getting user: %w", err)
	}
	return &user, nil
}

// GetAPIKey returns the authenticated user's API key.
func (c *Client) GetAPIKey(ctx context.Context) (*APIKeyResponse, error) {
	var resp APIKeyResponse
	if err := c.do(ctx, http.MethodGet, "/api/user/api-key", nil, &resp); err != nil {
		return nil, fmt.Errorf("getting API key: %w", err)
	}
	return &resp, nil
}

// RegenerateAPIKey regenerates the authenticated user's API key.
func (c *Client) RegenerateAPIKey(ctx context.Context) (*APIKeyResponse, error) {
	var resp APIKeyResponse
	if err := c.do(ctx, http.MethodPost, "/api/user/api-key/regenerate", nil, &resp); err != nil {
		return nil, fmt.Errorf("regenerating API key: %w", err)
	}
	return &resp, nil
}
