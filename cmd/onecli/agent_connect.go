package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/onecli/onecli-cli/internal/agentapi"
	"github.com/onecli/onecli-cli/internal/agentenv"
	"github.com/onecli/onecli-cli/pkg/exitcode"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// stdin is where a connect token given as "-" is read from.
var stdin io.Reader = os.Stdin

// ConnectCmd is `onecli connect <connect-token>`: the connect script's
// trade, in Go. The token is sent once and never written to disk.
type ConnectCmd struct {
	Token      string `arg:"" optional:"" name:"connect-token" help:"The vault's connect token (oc_connect_...). Use - to read it from standard input."`
	Credential string `optional:"" name:"credential" help:"Write agent.env for an agent credential you already hold (oc_agent_...), with no trade."`
	Name       string `optional:"" name:"name" help:"The agent's name. Unset: the server picks one."`
	Replace    bool   `optional:"" name:"replace" help:"This place holds an agent on another vault: disconnect it and connect here."`
	NoProfile  bool   `optional:"" name:"no-profile" help:"Do not add the line that reads agent.env to your shell profile."`
	DryRun     bool   `optional:"" name:"dry-run" help:"Show what would be sent and written, without doing it."`
	Fields     string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet      string `optional:"" name:"quiet" help:"Output only the specified field."`
}

func validationErr(msg string) error {
	return &agentError{code: exitcode.CodeValidation, exit: exitcode.Error, msg: msg}
}

// checkAgentName applies the server's rule early: 1 to 64 characters, no
// control character, and none that breaks out of a JSON string.
func checkAgentName(name string) error {
	if name == "" {
		return nil
	}
	if len([]rune(name)) > 64 {
		return validationErr("invalid name: at most 64 characters")
	}
	if strings.ContainsAny(name, "\"\\\t\n\r") {
		return validationErr("invalid name: it cannot hold a quote, a backslash or a control character")
	}
	if err := validate.NoControlChars(name); err != nil {
		return validationErr("invalid name: " + err.Error())
	}
	return nil
}

func (c *ConnectCmd) Run(out *output.Writer) error {
	token := c.Token
	if token == "-" {
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("reading the connect token: %w", err)
		}
		token = strings.TrimSpace(line)
	}
	if token != "" && c.Credential != "" {
		return validationErr("give a connect token or --credential, not both")
	}
	if token == "" && c.Credential == "" {
		return &agentError{code: exitcode.CodeError, exit: exitcode.Error,
			msg:    "a connect token is needed",
			action: "copy the connect command from the vault's page, or run 'onecli signup'"}
	}
	// The value is never echoed: it may be a mistyped secret.
	if token != "" && !agentenv.ValidConnectToken(token) {
		return validationErr("that is not a connect token")
	}
	if c.Credential != "" && !agentenv.ValidCredential(c.Credential) {
		return validationErr("that is not an agent credential")
	}
	if err := checkAgentName(c.Name); err != nil {
		return err
	}
	dir, err := agentenv.Dir()
	if err != nil {
		return err
	}
	client := newAgentAPI()
	in := agentapi.ConnectInput{Runtime: detectRuntime(), Replace: c.Replace, Name: c.Name}

	if c.DryRun {
		payload := map[string]any{
			"api":       client.BaseURL(),
			"agent_env": dir + "/" + agentenv.FileName,
			"ca_file":   dir + "/" + agentenv.CAFileName,
		}
		if token != "" {
			payload["request"] = map[string]any{"method": "POST", "path": "/v2/agents", "body": in,
				"sends_held_credential": agentenv.HeldCredential(dir) != ""}
			return out.WriteDryRun("Would trade the connect token once for an agent credential and write agent.env (mode 0600)", payload)
		}
		payload["request"] = map[string]any{"method": "GET", "path": "/v2/agents/me"}
		return out.WriteDryRun("Would check the credential and write agent.env (mode 0600)", payload)
	}

	ctx := newContext()
	// The certificate first: with none, nothing is made.
	pem, err := client.CA(ctx)
	if err != nil {
		return fmt.Errorf("could not fetch the gateway's certificate from %s: %w", client.BaseURL(), err)
	}
	caPath, err := writeCA(dir, pem)
	if err != nil {
		return err
	}

	var agent agentapi.Agent
	if token != "" {
		held := agentenv.HeldCredential(dir)
		// The token is sent once, here, and kept nowhere after.
		a, created, err := client.Connect(ctx, token, held, in)
		if err != nil {
			return connectFail(err)
		}
		if !created {
			return writeShaped(out, connectedResult{
				Connected: true,
				Created:   false,
				AgentID:   a.ID,
				Name:      a.Name,
				VaultID:   a.VaultID,
				ExpiresAt: a.ExpiresAt,
				Gateway:   a.Gateway.Scheme + "://" + a.Gateway.Address,
				AgentEnv:  dir + "/" + agentenv.FileName,
				CAFile:    caPath,
				Next:      "already connected to this vault; nothing was changed",
			}, c.Fields, c.Quiet)
		}
		agent = a
	} else {
		self, err := client.Me(ctx, c.Credential)
		if err != nil {
			return agentFail(err)
		}
		agent = agentapi.Agent{
			ID: self.ID, Name: self.Name, VaultID: self.Vault.ID, ExpiresAt: self.ExpiresAt,
			Gateway: self.Gateway, Credential: c.Credential,
		}
	}
	res, err := saveAgent(dir, caPath, agent, c.NoProfile)
	if err != nil {
		return err
	}
	return writeShaped(out, res, c.Fields, c.Quiet)
}

// connectFail explains the connect trade's refusals.
func connectFail(err error) error {
	var e *agentapi.Error
	if errors.As(err, &e) {
		switch {
		case e.StatusCode == http.StatusUnauthorized:
			return &agentError{code: exitcode.CodeAuthRequired, exit: exitcode.AuthRequired,
				msg:    "the connect token was not accepted",
				action: "copy a new connect command from the vault's page"}
		case e.StatusCode == http.StatusConflict && e.HasIssue("agent_on_other_vault"):
			return &agentError{code: exitcode.CodeConflict, exit: exitcode.Conflict,
				msg:    e.Message,
				action: "run again with --replace to move this agent to this vault"}
		}
	}
	return agentFail(err)
}

// SignupCmd is `onecli signup`: start a sign-up, show the link and code,
// and wait for the person to approve it.
type SignupCmd struct {
	Name      string        `optional:"" name:"name" help:"The agent's name. Unset: the server picks one."`
	Timeout   time.Duration `optional:"" name:"timeout" default:"15m" help:"Stop waiting after this long."`
	NoProfile bool          `optional:"" name:"no-profile" help:"Do not add the line that reads agent.env to your shell profile."`
	DryRun    bool          `optional:"" name:"dry-run" help:"Show what would be sent, without starting a sign-up."`
	Fields    string        `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet     string        `optional:"" name:"quiet" help:"Output only the specified field."`
}

// sleep waits between polls; tests replace it.
var sleep = time.Sleep

// minPollInterval is the floor between two polls, whatever the server says.
const minPollInterval = time.Second

type signupWaiting struct {
	Status          string `json:"status"`
	VerificationURL string `json:"verification_url"`
	UserCode        string `json:"user_code"`
	ExpiresAt       string `json:"expires_at"`
	Message         string `json:"message"`
}

func (c *SignupCmd) Run(out *output.Writer) error {
	if err := checkAgentName(c.Name); err != nil {
		return err
	}
	dir, err := agentenv.Dir()
	if err != nil {
		return err
	}
	client := newAgentAPI()
	in := agentapi.SignupInput{Name: c.Name, Runtime: detectRuntime()}
	if c.DryRun {
		return out.WriteDryRun("Would start a sign-up, wait for your person to approve it, then write agent.env (mode 0600)", map[string]any{
			"api":       client.BaseURL(),
			"request":   map[string]any{"method": "POST", "path": "/v2/agent-signups", "body": in},
			"agent_env": dir + "/" + agentenv.FileName,
		})
	}
	ctx := newContext()
	if held := agentenv.HeldCredential(dir); held != "" {
		if self, err := client.Me(ctx, held); err == nil {
			return &agentError{code: exitcode.CodeConflict, exit: exitcode.Conflict,
				msg:    fmt.Sprintf("this place is connected already, as %s on vault %s", self.Name, self.Vault.ID),
				action: "run 'onecli disconnect' first to connect it somewhere else"}
		}
	}
	pem, err := client.CA(ctx)
	if err != nil {
		return fmt.Errorf("could not fetch the gateway's certificate from %s: %w", client.BaseURL(), err)
	}
	caPath, err := writeCA(dir, pem)
	if err != nil {
		return err
	}
	started, err := client.StartSignup(ctx, in)
	if err != nil {
		return agentFail(err)
	}
	if started.ID == "" || started.PollToken == "" {
		return errors.New("the server's sign-up answer had no id or poll token")
	}
	if err := validate.ResourceID(started.ID); err != nil {
		return fmt.Errorf("the server's sign-up id is not safe to use: %w", err)
	}
	// The person needs the link and the code now, while this waits. They go
	// to stderr as one JSON line; stdout keeps the one final answer.
	waiting, _ := jsonLine(signupWaiting{
		Status:          "waiting_for_approval",
		VerificationURL: started.VerificationURL,
		UserCode:        started.UserCode,
		ExpiresAt:       started.ExpiresAt,
		Message:         "Show your person this link and code. They sign in, type the code, pick a vault and approve.",
	})
	out.Stderr(waiting)

	agent, err := pollUntilDone(ctx, client, started, c.Timeout)
	if err != nil {
		return err
	}
	res, err := saveAgent(dir, caPath, agent, c.NoProfile)
	if err != nil {
		return err
	}
	return writeShaped(out, res, c.Fields, c.Quiet)
}

func pollUntilDone(ctx context.Context, client *agentapi.Client, s agentapi.Signup, timeout time.Duration) (agentapi.Agent, error) {
	interval := time.Duration(s.Interval) * time.Second
	if interval < minPollInterval {
		interval = 5 * time.Second
	}
	limit := timeout
	if exp, err := time.Parse(time.RFC3339, s.ExpiresAt); err == nil {
		if left := time.Until(exp) + interval; left < limit {
			limit = left
		}
	}
	// Time waited is counted from the sleeps, so a test's fake sleep ends too.
	var waited time.Duration
	for {
		if waited >= limit {
			return agentapi.Agent{}, &agentError{code: "EXPIRED", exit: exitcode.Error,
				msg: "stopped waiting: the sign-up was not approved in time", action: "run 'onecli signup' again"}
		}
		sleep(interval)
		waited += interval
		agent, err := client.PollSignup(ctx, s.ID, s.PollToken)
		if err == nil {
			return agent, nil
		}
		var e *agentapi.Error
		if !errors.As(err, &e) {
			return agentapi.Agent{}, err
		}
		switch e.Code {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			if ra := time.Duration(e.RetryAfter) * time.Second; ra > interval {
				interval = ra
			}
			continue
		case "access_denied":
			return agentapi.Agent{}, &agentError{code: "ACCESS_DENIED", exit: exitcode.Forbidden,
				msg: "your person did not approve the sign-up", action: "start a new one only if your person wants to connect you"}
		case "expired_token":
			return agentapi.Agent{}, &agentError{code: "EXPIRED", exit: exitcode.Error,
				msg: "the sign-up is over", action: "run 'onecli signup' again"}
		}
		if e.StatusCode == http.StatusTooManyRequests {
			if ra := time.Duration(e.RetryAfter) * time.Second; ra > interval {
				interval = ra
			}
			continue
		}
		return agentapi.Agent{}, agentFail(err)
	}
}

// jsonLine encodes v as one compact JSON line.
func jsonLine(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
