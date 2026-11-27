package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// Key is one of the caller's own managing keys, as /v2/keys lists it.
// The key itself is never part of a listing.
type Key struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	CreatedVia  string   `json:"createdVia"`
	CreatedAt   string   `json:"createdAt"`
	ExpiresAt   *string  `json:"expiresAt"`
	LastUsedAt  *string  `json:"lastUsedAt"`
}

type keyPage struct {
	Data       []Key   `json:"data"`
	NextCursor *string `json:"nextCursor"`
}

// maxKeyPages bounds the listing loop so a server that keeps answering
// with a cursor cannot keep the CLI spinning.
const maxKeyPages = 50

// ErrKeysUnsupported means the server has no /v2/keys (an API older than
// 3.0): it answered 404 for the collection itself.
var ErrKeysUnsupported = errors.New("this server has no /v2/keys")

// IsStatus reports whether err carries an API answer with this status.
func IsStatus(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}

// ListKeys returns all of the caller's live keys, following every page.
// It returns ErrKeysUnsupported when the server has no /v2/keys.
func (c *Client) ListKeys(ctx context.Context) ([]Key, error) {
	keys := []Key{}
	cursor := ""
	for range maxKeyPages {
		path := "/v2/keys"
		if cursor != "" {
			path += "?cursor=" + url.QueryEscape(cursor)
		}
		var page keyPage
		if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
			if cursor == "" && IsStatus(err, http.StatusNotFound) {
				return nil, ErrKeysUnsupported
			}
			return nil, fmt.Errorf("listing keys: %w", err)
		}
		keys = append(keys, page.Data...)
		if page.NextCursor == nil || *page.NextCursor == "" {
			return keys, nil
		}
		cursor = *page.NextCursor
	}
	return nil, fmt.Errorf("listing keys: more than %d pages", maxKeyPages)
}

// RevokeKey turns one of the caller's own keys off for good.
func (c *Client) RevokeKey(ctx context.Context, id string) error {
	if err := c.do(ctx, http.MethodDelete, "/v2/keys/"+url.PathEscape(id), nil, nil); err != nil {
		return fmt.Errorf("revoking key %s: %w", id, err)
	}
	return nil
}
