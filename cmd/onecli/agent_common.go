package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/onecli/onecli-cli/internal/agentapi"
	"github.com/onecli/onecli-cli/internal/agentenv"
	"github.com/onecli/onecli-cli/internal/config"
	"github.com/onecli/onecli-cli/pkg/exitcode"
	"github.com/onecli/onecli-cli/pkg/output"
)

// agentError is a command error that carries its own string code, exit
// code and next step, for when the generic status mapping in handleError
// would give the wrong one. handleError writes it as it is.
type agentError struct {
	code   string
	exit   int
	msg    string
	action string
}

func (e *agentError) Error() string { return e.msg }

// CodeUnavailable marks an answer the server could not give (503).
const codeUnavailable = "UNAVAILABLE"

// codeRateLimited marks a 429.
const codeRateLimited = "RATE_LIMITED"

const actionConnect = "connect first: 'onecli connect <connect-token>' or 'onecli signup'"

func notConnected(msg string) error {
	return &agentError{code: exitcode.CodeAuthRequired, exit: exitcode.AuthRequired, msg: msg, action: actionConnect}
}

// agentFail maps an error from the agent routes onto a CLI error.
func agentFail(err error) error {
	var e *agentapi.Error
	if !errors.As(err, &e) {
		return err
	}
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return notConnected(e.Message)
	case http.StatusForbidden:
		return &agentError{code: exitcode.CodeForbidden, exit: exitcode.Forbidden, msg: e.Message}
	case http.StatusNotFound:
		return &agentError{code: exitcode.CodeNotFound, exit: exitcode.NotFound, msg: e.Message}
	case http.StatusConflict:
		return &agentError{code: exitcode.CodeConflict, exit: exitcode.Conflict, msg: e.Message}
	case http.StatusUnprocessableEntity:
		return &agentError{code: exitcode.CodeValidation, exit: exitcode.Error, msg: e.Message}
	case http.StatusTooManyRequests:
		action := "wait, then try again"
		if e.RetryAfter > 0 {
			action = fmt.Sprintf("wait %d seconds, then try again", e.RetryAfter)
		}
		return &agentError{code: codeRateLimited, exit: exitcode.Error, msg: e.Message, action: action}
	case http.StatusServiceUnavailable:
		return &agentError{code: codeUnavailable, exit: exitcode.Error, msg: e.Message}
	}
	return &agentError{code: exitcode.CodeError, exit: exitcode.Error, msg: e.Message}
}

// writeAgentError writes err to stderr when it is an agentError and
// returns its exit code. ok is false for any other error.
func writeAgentError(out *output.Writer, err error) (exit int, ok bool) {
	var e *agentError
	if !errors.As(err, &e) {
		return 0, false
	}
	if e.action != "" {
		_ = out.ErrorWithAction(e.code, e.msg, e.action)
	} else {
		_ = out.Error(e.code, e.msg)
	}
	return e.exit, true
}

// newAgentAPI makes the /v2 client for the resolved API host.
var newAgentAPI = func() *agentapi.Client {
	return agentapi.New(config.APIHost())
}

// agentCredential resolves the agent's own credential and the folder of
// agent.env. It fails with AUTH_REQUIRED when there is none.
func agentCredential() (string, string, error) {
	cred, dir, _, err := agentCredentialFrom()
	return cred, dir, err
}

// agentCredentialFrom is agentCredential that also names where it was read.
func agentCredentialFrom() (string, string, agentenv.Source, error) {
	dir, err := agentenv.Dir()
	if err != nil {
		return "", "", agentenv.SourceNone, err
	}
	cred, src := agentenv.Credential(dir)
	if cred == "" {
		return "", dir, src, notConnected("no agent credential: ONECLI_AGENT_CREDENTIAL is not set and no agent.env was found")
	}
	return cred, dir, src, nil
}

// detectRuntime tells docker from plain linux, as the connect script does.
func detectRuntime() string {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker"
	}
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(data)
		if strings.Contains(s, "docker") || strings.Contains(s, "containerd") || strings.Contains(s, "kubepods") {
			return "docker"
		}
	}
	return "linux"
}

// writeCA writes the gateway's certificate next to agent.env, mode 0644,
// and the bundle of system roots plus that certificate beside it, in a dir
// only this user can write (the same rule agent.env follows): commands run
// through the gateway trust these files. With no system roots found it
// fails before anything is traded, rather than write a bundle that holds
// the gateway's certificate alone.
func writeCA(dir string, pem []byte) (string, error) {
	if err := agentenv.EnsurePrivateDir(dir); err != nil {
		return "", err
	}
	if _, err := agentenv.WriteCABundle(dir, string(pem)); err != nil {
		return "", err
	}
	path := filepath.Join(dir, agentenv.CAFileName)
	if err := agentenv.WriteFileFresh(path, pem, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// connectedResult is what connect and signup print.
type connectedResult struct {
	Connected bool    `json:"connected"`
	Created   bool    `json:"created"`
	AgentID   string  `json:"agent_id"`
	Name      string  `json:"name"`
	VaultID   string  `json:"vault_id"`
	ExpiresAt *string `json:"expires_at"`
	Gateway   string  `json:"gateway"`
	AgentEnv  string  `json:"agent_env"`
	CAFile    string  `json:"ca_file"`
	Profile   string  `json:"profile,omitempty"`
	Next      string  `json:"next"`
}

// saveAgent writes agent.env for a and the profile line. It never writes
// a connect token or a poll token: only the agent's own credential.
func saveAgent(dir, caPath string, a agentapi.Agent, noProfile bool) (connectedResult, error) {
	s := agentenv.Settings{
		AgentID:    a.ID,
		VaultID:    a.VaultID,
		Credential: a.Credential,
		Scheme:     a.Gateway.Scheme,
		Address:    a.Gateway.Address,
		CAFile:     caPath,
		BundleFile: filepath.Join(filepath.Dir(caPath), agentenv.CABundleFileName),
	}
	envPath, err := agentenv.Write(dir, s)
	if err != nil {
		return connectedResult{}, fmt.Errorf("%w; nothing was written", err)
	}
	res := connectedResult{
		Connected: true,
		Created:   true,
		AgentID:   a.ID,
		Name:      a.Name,
		VaultID:   a.VaultID,
		ExpiresAt: a.ExpiresAt,
		Gateway:   a.Gateway.Scheme + "://" + a.Gateway.Address,
		AgentEnv:  envPath,
		CAFile:    caPath,
		Next:      "open a new shell, or run: . " + envPath,
	}
	if !noProfile {
		profile, err := agentenv.ProfilePath()
		if err == nil {
			if _, err := agentenv.EnsureProfileLine(profile, envPath); err == nil {
				res.Profile = profile
			}
		}
	}
	return res, nil
}
