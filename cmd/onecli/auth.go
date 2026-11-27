package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

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
	Update           AuthUpdateCmd           `cmd:"" help:"Update your profile (display name)."`
	ApiKey           AuthApiKeyCmd           `cmd:"api-key" help:"List your API keys (never their values)."`
	RegenerateApiKey AuthRegenerateApiKeyCmd `cmd:"regenerate-api-key" help:"Revoke a key with --revoke <id>. Servers before 3.0: replace your key."`
}

// AuthUpdateCmd is `onecli auth update`.
type AuthUpdateCmd struct {
	Name   string `required:"" help:"New display name."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

func (c *AuthUpdateCmd) Run(out *output.Writer) error {
	if c.Name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if c.DryRun {
		return out.WriteDryRun("Would update profile", map[string]string{"name": c.Name})
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	user, err := client.UpdateProfile(newContext(), c.Name)
	if err != nil {
		return err
	}
	return out.Write(user)
}

// AuthLoginCmd is `onecli auth login`.
type AuthLoginCmd struct {
	APIKey  string        `optional:"" name:"api-key" help:"API key to store (oc_... format)."`
	Browser bool          `optional:"" help:"Sign in in a browser; the server makes a key for this terminal and hands it over once."`
	NoOpen  bool          `optional:"" name:"no-open" help:"With --browser: print the link only, do not open a browser."`
	Timeout time.Duration `optional:"" default:"5m" help:"With --browser: how long to wait for the sign-in."`
}

// AuthLoginResponse is the JSON output of a successful login.
type AuthLoginResponse struct {
	Status string `json:"status"`
	Email  string `json:"email,omitempty"`
	Name   string `json:"name,omitempty"`
}

func (c *AuthLoginCmd) Run(out *output.Writer) error {
	if c.Browser {
		if c.APIKey != "" {
			return errors.New("use --browser or --api-key, not both")
		}
		if c.Timeout <= 0 {
			return errors.New("--timeout must be positive")
		}
		return c.loginWithBrowser(out)
	}
	if c.NoOpen {
		return errors.New("--no-open needs --browser")
	}
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

	return storeVerifiedKey(out, config.APIHost(), apiKey)
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
