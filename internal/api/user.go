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

// GetUser returns the authenticated user's profile.
func (c *Client) GetUser(ctx context.Context) (*User, error) {
	var user User
	if err := c.do(ctx, http.MethodGet, "/api/user", nil, &user); err != nil {
		return nil, fmt.Errorf("getting user: %w", err)
	}
	return &user, nil
}
