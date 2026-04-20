package api

import (
	"context"
	"fmt"
	"net/http"
)

// MigrateResult is the response from a migration export request.
type MigrateResult struct {
	Imported MigrateImported  `json:"imported"`
	Skipped  []MigrateSkipped `json:"skipped"`
}

// MigrateImported contains counts of successfully imported entities.
type MigrateImported struct {
	Secrets      int `json:"secrets"`
	Agents       int `json:"agents"`
	AgentSecrets int `json:"agentSecrets"`
	Rules        int `json:"rules"`
}

// MigrateSkipped describes an entity that was skipped during import.
type MigrateSkipped struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// MigrateToCloud triggers a data export from the current instance to OneCLI Cloud.
// The server decrypts secrets and sends them directly to cloud over HTTPS.
func (c *Client) MigrateToCloud(ctx context.Context, cloudKey string) (*MigrateResult, error) {
	body := map[string]string{
		"cloudApiKey": cloudKey,
	}
	var result MigrateResult
	if err := c.do(ctx, http.MethodPost, "/api/migrate/export", body, &result); err != nil {
		return nil, fmt.Errorf("migrating to cloud: %w", err)
	}
	return &result, nil
}
