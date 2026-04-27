package main

import (
	"context"
	"time"

	"github.com/onecli/onecli-cli/pkg/output"
)

// VersionCmd prints version information as JSON.
type VersionCmd struct{}

// VersionResponse is the JSON output of the version command.
type VersionResponse struct {
	Version       string `json:"version"`
	ServerVersion string `json:"server_version"`
	ServerStatus  string `json:"server_status"`
}

func (cmd *VersionCmd) Run(out *output.Writer) error {
	resp := VersionResponse{Version: version}

	client, err := newClient()
	if err != nil {
		resp.ServerVersion = "unknown"
		resp.ServerStatus = "not_configured"
		return out.Write(resp)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	health, err := client.GetHealth(ctx)
	if err != nil {
		resp.ServerVersion = "unknown"
		resp.ServerStatus = "unreachable"
		return out.Write(resp)
	}

	resp.ServerVersion = health.Version
	resp.ServerStatus = health.Status
	return out.Write(resp)
}
