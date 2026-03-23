package main

import (
	"bufio"
	"errors"
	"os"
	"strings"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/internal/auth"
	"github.com/onecli/onecli-cli/internal/config"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
	"golang.org/x/term"
)

// AuthCmd is the `onecli auth` command group.
type AuthCmd struct {
	Login            AuthLoginCmd            `cmd:"" help:"Store API key for authentication."`
	Logout           AuthLogoutCmd           `cmd:"" help:"Remove stored API key."`
	Status           AuthStatusCmd           `cmd:"" help:"Show authentication status."`
	ApiKey           AuthApiKeyCmd           `cmd:"api-key" help:"Show your current API key."`
	RegenerateApiKey AuthRegenerateApiKeyCmd `cmd:"regenerate-api-key" help:"Regenerate your API key."`
}

// AuthLoginCmd is `onecli auth login`.
type AuthLoginCmd struct {
	APIKey string `optional:"" name:"api-key" help:"API key to store (oc_... format)."`
}

// AuthLoginResponse is the JSON output of a successful login.
type AuthLoginResponse struct {
	Status string `json:"status"`
	Email  string `json:"email,omitempty"`
	Name   string `json:"name,omitempty"`
}

func (c *AuthLoginCmd) Run(out *output.Writer) error {
	apiKey := c.APIKey

	// If no key provided, read from stdin.
	if apiKey == "" {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			out.Stderr("Paste your API key:")
		}
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			apiKey = strings.TrimSpace(scanner.Text())
		}
		if apiKey == "" {
			return errors.New("no API key provided")
		}
	}

	if err := validate.APIKey(apiKey); err != nil {
		return err
	}

	// Verify the key by calling the API.
	client := api.New(config.APIHost(), apiKey)
	user, err := client.GetUser(newContext())
	if err != nil {
		return errors.New("invalid API key: authentication failed")
	}

	// Store the key.
	credDir, err := config.CredentialsDir()
	if err != nil {
		return err
	}
	store := auth.NewStore(nil, credDir)
	if err := store.Save(apiKey); err != nil {
		return err
	}

	return out.Write(AuthLoginResponse{
		Status: "ok",
		Email:  user.Email,
		Name:   user.Name,
	})
}

// AuthLogoutCmd is `onecli auth logout`.
type AuthLogoutCmd struct{}

// AuthLogoutResponse is the JSON output of a successful logout.
type AuthLogoutResponse struct {
	Status string `json:"status"`
}

func (c *AuthLogoutCmd) Run(out *output.Writer) error {
	credDir, err := config.CredentialsDir()
	if err != nil {
		return err
	}
	store := auth.NewStore(nil, credDir)
	if err := store.Delete(); err != nil {
		if errors.Is(err, auth.ErrAPIKeyNotFound) {
			return out.Write(AuthLogoutResponse{Status: "ok"})
		}
		return err
	}
	return out.Write(AuthLogoutResponse{Status: "ok"})
}

// AuthStatusCmd is `onecli auth status`.
type AuthStatusCmd struct{}

// AuthStatusResponse is the JSON output of auth status.
type AuthStatusResponse struct {
	Authenticated bool   `json:"authenticated"`
	Email         string `json:"email,omitempty"`
	Name          string `json:"name,omitempty"`
}

func (c *AuthStatusCmd) Run(out *output.Writer) error {
	// Try the API with whatever key we have (or none).
	// The server decides whether auth is required.
	client, err := newClient()
	if err != nil {
		return err
	}
	user, err := client.GetUser(newContext())
	if err != nil {
		// handleError in main.go maps 401 → AUTH_REQUIRED with action hint,
		// so just return the error as-is.
		return err
	}

	return out.Write(AuthStatusResponse{
		Authenticated: true,
		Email:         user.Email,
		Name:          user.Name,
	})
}

// AuthApiKeyCmd is `onecli auth api-key`.
type AuthApiKeyCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
}

func (c *AuthApiKeyCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	resp, err := client.GetAPIKey(newContext())
	if err != nil {
		return err
	}
	return out.WriteFiltered(resp, c.Fields)
}

// AuthRegenerateApiKeyCmd is `onecli auth regenerate-api-key`.
type AuthRegenerateApiKeyCmd struct {
	DryRun bool `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AuthRegenerateApiKeyCmd) Run(out *output.Writer) error {
	if c.DryRun {
		return out.WriteDryRun("Would regenerate API key", nil)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	resp, err := client.RegenerateAPIKey(newContext())
	if err != nil {
		return err
	}
	return out.Write(resp)
}
