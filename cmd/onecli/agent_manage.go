package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/onecli/onecli-cli/internal/agentapi"
	"github.com/onecli/onecli-cli/internal/agentenv"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// DisconnectCmd is `onecli disconnect`: remove this agent, then delete
// agent.env. An agent's own credential cannot remove an agent: that call
// takes a person's key (DELETE /v2/agents/{id}). With no key, agent.env is
// still deleted, and the answer says the agent was not removed.
type DisconnectCmd struct {
	KeepRemote bool   `optional:"" name:"local-only" help:"Only delete agent.env and the profile line; do not ask the server to remove the agent."`
	DryRun     bool   `optional:"" name:"dry-run" help:"Show what would be removed, without doing it."`
	Fields     string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet      string `optional:"" name:"quiet" help:"Output only the specified field."`
}

type disconnectResult struct {
	AgentID         string   `json:"agent_id"`
	RemoteRemoved   bool     `json:"remote_removed"`
	RemoteReason    string   `json:"remote_reason,omitempty"`
	AgentEnvDeleted bool     `json:"agent_env_deleted"`
	ProfileUpdated  bool     `json:"profile_updated"`
	Detached        []string `json:"detached,omitempty"`
	Next            string   `json:"next,omitempty"`
}

const disconnectByPerson = "a person disconnects this agent on the vault's Agents tab, or run 'onecli auth login' and then 'onecli disconnect' again"

// loadPersonKey returns a person's stored API key, or "". Tests replace it.
var loadPersonKey = loadStoredAPIKey

func (c *DisconnectCmd) Run(out *output.Writer) error {
	dir, err := agentenv.Dir()
	if err != nil {
		return err
	}
	envPath := filepath.Join(dir, agentenv.FileName)
	vals, readErr := agentenv.Read(envPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", envPath, readErr)
	}
	cred, _ := agentenv.Credential(dir)
	if readErr != nil && cred == "" {
		return notConnected("nothing to disconnect: no agent.env and no ONECLI_AGENT_CREDENTIAL")
	}
	client := newAgentAPI()
	ctx := newContext()

	agentID := vals["ONECLI_AGENT_ID"]
	if agentID == "" && cred != "" && !c.KeepRemote {
		if self, err := client.Me(ctx, cred); err == nil {
			agentID = self.ID
		}
	}
	if agentID != "" {
		if err := validate.ResourceID(agentID); err != nil {
			return fmt.Errorf("agent.env holds an agent id that is not safe to use: %w", err)
		}
	}
	key := ""
	if !c.KeepRemote {
		key = loadPersonKey()
	}

	if c.DryRun {
		payload := map[string]any{"agent_env": envPath, "agent_id": agentID}
		if !c.KeepRemote && agentID != "" && key != "" {
			payload["request"] = map[string]any{"method": "DELETE", "path": "/v2/agents/" + agentID}
		}
		return out.WriteDryRun("Would remove this agent and delete agent.env", payload)
	}

	res := disconnectResult{AgentID: agentID}
	// A person's key when one is stored; else the agent's own credential.
	// Today the server takes only a person's key for this call, so the
	// agent's credential is refused, and agent.env is deleted all the same.
	bearer, byAgent := key, false
	if bearer == "" {
		bearer, byAgent = cred, true
	}
	switch {
	case c.KeepRemote:
		res.RemoteReason = "--local-only"
	case agentID == "":
		res.RemoteReason = "the agent's id is not known: agent.env has none and the server did not say"
	case bearer == "":
		res.RemoteReason = "no credential to ask the server with"
		res.Next = disconnectByPerson
	default:
		err := client.DeleteAgent(ctx, bearer, agentID)
		var e *agentapi.Error
		switch {
		case err == nil:
			res.RemoteRemoved = true
		case errors.As(err, &e) && e.StatusCode == http.StatusNotFound:
			// Gone already: nothing is left to remove.
			res.RemoteRemoved = true
			res.RemoteReason = "the agent was gone already"
		case byAgent && errors.As(err, &e) && (e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden):
			res.RemoteReason = "the server did not let this agent remove itself: " + e.Message
			res.Next = disconnectByPerson
		default:
			// Keep agent.env: the agent still exists, and its file is how
			// a retry finds it.
			return agentFail(err)
		}
	}

	// Undo attach first, while agent.env still names the values it wrote.
	if home, err := os.UserHomeDir(); err == nil {
		pairs := make([][2]string, 0, len(vals))
		for k, v := range vals {
			pairs = append(pairs, [2]string{k, v})
		}
		res.Detached = detachAll(home, pairs)
	}
	if err := os.Remove(envPath); err == nil {
		res.AgentEnvDeleted = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting %s: %w", envPath, err)
	}
	if profile, err := agentenv.ProfilePath(); err == nil {
		if changed, err := agentenv.RemoveProfileLine(profile, envPath); err == nil {
			res.ProfileUpdated = changed
		}
	}
	if res.Next == "" && os.Getenv(agentenv.CredentialVar) != "" {
		res.Next = "ONECLI_AGENT_CREDENTIAL is still set in this shell: unset it, or open a new shell"
	}
	return writeShaped(out, res, c.Fields, c.Quiet)
}

// RequestCmd is `onecli request <service>`: the link a person opens to add
// an item this agent lacks. It sends nothing and changes nothing.
type RequestCmd struct {
	Service string `arg:"" help:"The service you need, for example github or api.example.com."`
	Fields  string `optional:"" help:"Comma-separated list of fields to include in output."`
	Quiet   string `optional:"" name:"quiet" help:"Output only the specified field."`
}

var serviceRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,99}$`)

type requestResult struct {
	Service string `json:"service"`
	VaultID string `json:"vault_id"`
	URL     string `json:"url,omitempty"`
	Path    string `json:"path"`
	Message string `json:"message"`
	Note    string `json:"note,omitempty"`
}

func (c *RequestCmd) Run(out *output.Writer) error {
	service := strings.ToLower(strings.TrimSpace(c.Service))
	if !serviceRE.MatchString(service) {
		return validationErr("invalid service: use a name like github or a host like api.example.com")
	}
	cred, _, err := agentCredential()
	if err != nil {
		return err
	}
	client := newAgentAPI()
	ctx := newContext()
	self, err := client.Me(ctx, cred)
	if err != nil {
		return agentFail(err)
	}
	if err := validate.ResourceID(self.Vault.ID); err != nil {
		return fmt.Errorf("the server's vault id is not safe to use: %w", err)
	}
	// The vault's own page is /o/<org>/v/<vault>, and an agent cannot read
	// its organization. /w/<vault> opens the same vault for a person who can
	// use it.
	path := "/w/" + url.PathEscape(self.Vault.ID)
	res := requestResult{
		Service: service,
		VaultID: self.Vault.ID,
		Path:    path,
		Message: fmt.Sprintf("Ask your person to add an item for %s to the vault %s. Do not ask for a key.", service, self.Vault.Name),
	}
	if origin, err := client.AppOrigin(ctx); err == nil {
		res.URL = origin + path
	} else {
		res.Note = "the dashboard's address is not known: open this path on your OneCLI dashboard"
	}
	return writeShaped(out, res, c.Fields, c.Quiet)
}
