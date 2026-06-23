package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// GetGatewaySkill fetches the gateway skill markdown from the API.
// When agentFramework is non-empty, the server may return framework-specific content.
func (c *Client) GetGatewaySkill(ctx context.Context, agentFramework string) (string, error) {
	c.resolvePrefix(ctx)
	path := c.applyPrefix("/v1/skill/gateway")
	if agentFramework != "" {
		path += "?" + url.Values{"agent_framework": {agentFramework}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching gateway skill: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("skill endpoint returned %d", resp.StatusCode)}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading skill response: %w", err)
	}
	return string(body), nil
}
