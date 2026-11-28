package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/onecli/onecli-cli/internal/agentapi"
	"github.com/onecli/onecli-cli/pkg/exitcode"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// StatusCmd is `onecli status`: GET /v2/agents/me with the agent's credential.
type StatusCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet  string `optional:"" name:"quiet" help:"Output only the specified field."`
}

type statusResult struct {
	Connected  bool    `json:"connected"`
	AgentID    string  `json:"agent_id"`
	Name       string  `json:"name"`
	VaultID    string  `json:"vault_id"`
	VaultName  string  `json:"vault_name"`
	Runtime    *string `json:"runtime"`
	ExpiresAt  *string `json:"expires_at"`
	Gateway    string  `json:"gateway"`
	Credential string  `json:"credential_source"`
}

func (c *StatusCmd) Run(out *output.Writer) error {
	cred, _, src, err := agentCredentialFrom()
	if err != nil {
		return err
	}
	self, err := newAgentAPI().Me(newContext(), cred)
	if err != nil {
		return agentFail(err)
	}
	res := statusResult{
		Connected:  true,
		AgentID:    self.ID,
		Name:       self.Name,
		VaultID:    self.Vault.ID,
		VaultName:  self.Vault.Name,
		Runtime:    self.Runtime,
		ExpiresAt:  self.ExpiresAt,
		Gateway:    self.Gateway.Scheme + "://" + self.Gateway.Address,
		Credential: string(src),
	}
	return writeShaped(out, res, c.Fields, c.Quiet)
}

// writeShaped writes v honoring --quiet, then --fields.
func writeShaped(out *output.Writer, v any, fields, quiet string) error {
	if quiet != "" {
		return out.WriteQuiet(v, quiet)
	}
	return out.WriteFiltered(v, fields)
}

// ItemsCmd is `onecli items`: GET /v2/agents/me/items, every page.
type ItemsCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet  string `optional:"" name:"quiet" help:"Output only the specified field, one per line."`
}

func (c *ItemsCmd) Run(out *output.Writer) error {
	cred, _, err := agentCredential()
	if err != nil {
		return err
	}
	items, err := newAgentAPI().Items(newContext(), cred)
	if err != nil {
		return agentFail(err)
	}
	return writeShaped(out, items, c.Fields, c.Quiet)
}

// CheckCmd is `onecli check <METHOD> <URL>`: POST /v2/agents/me/check.
type CheckCmd struct {
	Method string `arg:"" help:"The HTTP method of the call, for example GET."`
	URL    string `arg:"" name:"url" help:"The full http or https address of the call."`
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet  string `optional:"" name:"quiet" help:"Output only the specified field."`
}

// checkMethodOK mirrors the server's rule: 1 to 16 letters.
func checkMethodOK(m string) bool {
	if m == "" || len(m) > 16 {
		return false
	}
	for _, r := range m {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func (c *CheckCmd) Run(out *output.Writer) error {
	if !checkMethodOK(c.Method) {
		return &agentError{code: exitcode.CodeValidation, exit: exitcode.Error, msg: "invalid method: use an HTTP method, for example GET"}
	}
	if err := validate.URL(c.URL); err != nil {
		return &agentError{code: exitcode.CodeValidation, exit: exitcode.Error, msg: "invalid url: " + err.Error()}
	}
	if len(c.URL) > 8192 {
		return &agentError{code: exitcode.CodeValidation, exit: exitcode.Error, msg: "invalid url: longer than 8192 characters"}
	}
	cred, _, err := agentCredential()
	if err != nil {
		return err
	}
	res, err := newAgentAPI().CheckCall(newContext(), cred, strings.ToUpper(c.Method), c.URL)
	if errors.Is(err, agentapi.ErrUnknown) {
		// Print the answer, and still fail: unknown is never allowed.
		_ = writeShaped(out, res, c.Fields, c.Quiet)
		return &agentError{code: codeUnavailable, exit: exitcode.Error, msg: res.Reason, action: "the real call still decides; try the check again later"}
	}
	if err != nil {
		var e *agentapi.Error
		if errors.As(err, &e) && e.StatusCode == http.StatusUnprocessableEntity {
			return &agentError{code: exitcode.CodeValidation, exit: exitcode.Error, msg: e.Message}
		}
		return agentFail(err)
	}
	return writeShaped(out, res, c.Fields, c.Quiet)
}
