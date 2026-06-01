package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// CredentialStub is the response from the credential-stubs API.
type CredentialStub struct {
	Agent       string `json:"agent"`
	FilePath    string `json:"filePath"`
	Content     string `json:"content"`
	Permissions string `json:"permissions"`
}

// GetCredentialStub fetches a credential stub for the given agent from the API.
func (c *Client) GetCredentialStub(ctx context.Context, agent string) (*CredentialStub, error) {
	c.resolvePrefix(ctx)
	path := c.applyPrefix("/v1/credential-stubs/" + agent)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching credential stub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("credential-stubs endpoint returned %d", resp.StatusCode)}
	}

	var stub CredentialStub
	if err := json.NewDecoder(resp.Body).Decode(&stub); err != nil {
		return nil, fmt.Errorf("decoding credential stub: %w", err)
	}
	return &stub, nil
}
