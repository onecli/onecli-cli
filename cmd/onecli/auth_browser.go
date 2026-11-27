package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/internal/auth"
	"github.com/onecli/onecli-cli/internal/config"
	"github.com/onecli/onecli-cli/pkg/exitcode"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// cliAuthClient is the part of the API client the browser login uses.
type cliAuthClient interface {
	StartCLIAuth(ctx context.Context) (*api.CLIAuthSession, error)
	PollCLIAuth(ctx context.Context, code string) (*api.CLIAuthPoll, error)
}

// browserLogin is one browser login: start a session, show or open its
// page, then poll until the person confirms it, it expires, or time runs
// out. It returns the key the server hands out once.
type browserLogin struct {
	client   cliAuthClient
	open     func(url string) error // nil: only print the link
	notify   func(msg string)       // where the link and code are shown (stderr)
	interval time.Duration
	timeout  time.Duration
	sleep    func(time.Duration)
}

func (b *browserLogin) run(ctx context.Context) (string, error) {
	s, err := b.client.StartCLIAuth(ctx)
	if err != nil {
		return "", err
	}
	if err := validate.URL(s.AuthURL); err != nil {
		return "", fmt.Errorf("server sent a bad sign-in link: %w", err)
	}
	if s.Code == "" {
		return "", errors.New("server sent no sign-in code")
	}
	b.notify(fmt.Sprintf("Open this link, sign in and confirm the code %s: %s", shortCode(s.Code), s.AuthURL))
	if b.open != nil {
		if err := b.open(s.AuthURL); err != nil {
			b.notify("Could not open a browser; open the link above yourself.")
		}
	}
	deadline := time.Now().Add(b.timeout)
	for {
		p, err := b.client.PollCLIAuth(ctx, s.Code)
		if err != nil {
			return "", err
		}
		switch p.Status {
		case "ok":
			if err := validate.APIKey(p.APIKey); err != nil {
				return "", fmt.Errorf("server sent a bad key: %w", err)
			}
			return p.APIKey, nil
		case "expired":
			return "", &actionError{
				code:   exitcode.CodeAuthRequired,
				msg:    "the sign-in link expired or was already used",
				action: "run `onecli auth login --browser` again",
				exit:   exitcode.AuthRequired,
			}
		case "pending":
		default:
			return "", fmt.Errorf("unexpected sign-in status %q", p.Status)
		}
		if !time.Now().Before(deadline) {
			return "", &actionError{
				code:   exitcode.CodeAuthRequired,
				msg:    "timed out waiting for the browser sign-in",
				action: "run `onecli auth login --browser` again and confirm in the browser",
				exit:   exitcode.AuthRequired,
			}
		}
		b.sleep(b.interval)
	}
}

// shortCode is the part of the code shown to the person, to match with the
// page. The full code stays in the link.
func shortCode(code string) string {
	if len(code) <= 8 {
		return code
	}
	return code[:8]
}

// openBrowser opens url in the system browser. The URL was checked to be
// http(s) and is passed as one argument, never through a shell.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// loginWithBrowser runs the browser login and stores the key it gets.
func (c *AuthLoginCmd) loginWithBrowser(out *output.Writer) error {
	host := config.APIHost()
	b := &browserLogin{
		client:   api.New(host, ""),
		notify:   out.Stderr,
		interval: 2 * time.Second,
		timeout:  c.Timeout,
		sleep:    time.Sleep,
	}
	if !c.NoOpen {
		b.open = openBrowser
	}
	key, err := b.run(newContext())
	if err != nil {
		return err
	}
	return storeVerifiedKey(out, host, key)
}

// storeVerifiedKey checks key against the API, then stores it.
func storeVerifiedKey(out *output.Writer, host, apiKey string) error {
	client := api.New(host, apiKey)
	user, err := client.GetUser(newContext())
	if err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 401 {
			return errors.New("invalid API key: the server rejected this key")
		}
		return fmt.Errorf("could not verify API key: %w", err)
	}
	credDir, err := config.CredentialsDir()
	if err != nil {
		return err
	}
	if err := auth.NewStore(nil, credDir).Save(apiKey); err != nil {
		return err
	}
	return out.Write(AuthLoginResponse{Status: "ok", Email: user.Email, Name: user.Name})
}
