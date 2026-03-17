package main

import "github.com/onecli/onecli-cli/pkg/output"

// VersionCmd prints version information as JSON.
type VersionCmd struct{}

// VersionResponse is the JSON output of the version command.
type VersionResponse struct {
	Version string `json:"version"`
}

func (cmd *VersionCmd) Run(out *output.Writer) error {
	return out.Write(VersionResponse{
		Version: version,
	})
}
