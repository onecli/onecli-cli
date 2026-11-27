package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/onecli/onecli-cli/internal/agentapi"
	"github.com/onecli/onecli-cli/internal/agentenv"
	"github.com/onecli/onecli-cli/pkg/exitcode"
	"github.com/onecli/onecli-cli/pkg/output"
)

// execStdout is the child's standard output; tests replace it.
var execStdout io.Writer = os.Stdout

// exitStatus ends the CLI with a child's exit code and writes nothing.
type exitStatus struct{ code int }

func (e *exitStatus) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// ExecCmd is `onecli exec -- <command>`: one command with the proxy and the
// OneCLI certificate set for it only. Unlike `run`, it uses this agent's
// own credential and touches no agent config.
type ExecCmd struct {
	NoCA   bool     `optional:"" name:"no-ca" help:"Do not set the OneCLI certificate for the command."`
	DryRun bool     `optional:"" name:"dry-run" help:"Show the command and the names of the variables it would get, without running it."`
	Args   []string `arg:"" passthrough:"" name:"command" help:"The command and its arguments, after --."`
}

// agentSettings resolves this agent's settings: agent.env when it holds the
// credential in use, else the credential and GET /v2/agents/me.
func agentSettings(client *agentapi.Client) (agentenv.Settings, string, error) {
	cred, dir, err := agentCredential()
	if err != nil {
		return agentenv.Settings{}, "", err
	}
	s := agentenv.Settings{Credential: cred, CAFile: filepath.Join(dir, agentenv.CAFileName)}
	vals, _ := agentenv.Read(filepath.Join(dir, agentenv.FileName))
	if vals[agentenv.CredentialVar] == cred {
		if u, perr := url.Parse(vals["HTTPS_PROXY"]); perr == nil && u.Host != "" {
			s.Scheme, s.Address = u.Scheme, u.Host
			s.AgentID, s.VaultID = vals["ONECLI_AGENT_ID"], vals["ONECLI_VAULT_ID"]
		}
	}
	if s.Address == "" {
		self, err := client.Me(newContext(), cred)
		if err != nil {
			return agentenv.Settings{}, "", agentFail(err)
		}
		s.Scheme, s.Address = self.Gateway.Scheme, self.Gateway.Address
		s.AgentID, s.VaultID = self.ID, self.Vault.ID
	}
	if err := s.Check(); err != nil {
		return agentenv.Settings{}, "", err
	}
	return s, dir, nil
}

// gatewayPEM reads the gateway's certificate next to agent.env, or fetches it.
func gatewayPEM(client *agentapi.Client, dir string) ([]byte, error) {
	if data, err := os.ReadFile(filepath.Join(dir, agentenv.CAFileName)); err == nil &&
		bytes.Contains(data, []byte("-----BEGIN CERTIFICATE-----")) {
		return data, nil
	}
	return client.CA(newContext())
}

func (c *ExecCmd) Run(out *output.Writer) error {
	args := c.Args
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return validationErr("no command given: use 'onecli exec -- <command> [args...]'")
	}
	binary, err := exec.LookPath(args[0])
	if err != nil {
		return &agentError{code: exitcode.CodeNotFound, exit: exitcode.NotFound,
			msg: fmt.Sprintf("command not found: %s", args[0])}
	}
	client := newAgentAPI()
	s, dir, err := agentSettings(client)
	if err != nil {
		return err
	}
	serverEnv := map[string]string{}
	for _, kv := range s.Pairs() {
		serverEnv[kv[0]] = kv[1]
	}
	if c.DryRun {
		names := make([]string, 0, len(serverEnv)+len(caTrustKeys))
		for k := range serverEnv {
			if c.NoCA && (slices.Contains(caTrustKeys, k) || k == "PGSSLROOTCERT") {
				continue
			}
			names = append(names, k)
		}
		if !c.NoCA {
			for _, k := range caTrustKeys {
				if _, ok := serverEnv[k]; !ok {
					names = append(names, k)
				}
			}
		}
		slices.Sort(names)
		return out.WriteDryRun("Would run the command with this agent's proxy settings", map[string]any{
			"binary": binary, "args": args, "env_set": names,
		})
	}
	caPath := ""
	if c.NoCA {
		for _, k := range caTrustKeys {
			delete(serverEnv, k)
		}
		delete(serverEnv, "PGSSLROOTCERT")
	} else {
		pem, err := gatewayPEM(client, dir)
		if err != nil {
			return fmt.Errorf("could not get the gateway's certificate: %w", err)
		}
		// System roots plus the gateway's: SSL_CERT_FILE replaces a trust
		// store, and the command may also reach hosts past the proxy.
		caPath, err = writeGatewayCACert(string(pem))
		if err != nil {
			return err
		}
		serverEnv["PGSSLROOTCERT"] = caPath
	}
	env := buildChildEnv(os.Environ(), serverEnv, caPath)
	env = suppressUndiciProxyWarning(env)

	cmd := exec.Command(binary, args[1:]...)
	cmd.Args[0] = args[0]
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, execStdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return &exitStatus{code: ee.ExitCode()}
		}
		return fmt.Errorf("could not start %s: %w", args[0], err)
	}
	return nil
}

// AttachCmd is `onecli attach [agent]`: the OneCLI skill into each agent's
// skills folder, and for Claude Code the proxy settings into the env block
// of its settings.json. They apply from the agent's next start.
type AttachCmd struct {
	Agent  string `arg:"" optional:"" help:"One agent: claude, cursor, codex, hermes, opencode or openclaw. Unset: every one found in your home folder."`
	DryRun bool   `optional:"" name:"dry-run" help:"Show what would be written, without writing it."`
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
}

type attachedAgent struct {
	Agent    string `json:"agent"`
	Skill    string `json:"skill"`
	Settings string `json:"settings,omitempty"`
	Note     string `json:"note,omitempty"`
}

type attachResult struct {
	Attached []attachedAgent `json:"attached"`
	Next     string          `json:"next"`
}

// attachSkillName is the folder of the skill attach writes.
const attachSkillName = "onecli"

// attachSkill wraps the agent page in the front matter agents load skills by.
func attachSkill(page string) string {
	return "---\nname: onecli\ndescription: >-\n  OneCLI: call APIs through the OneCLI gateway with no key of your own.\n" +
		"  Load when an API call needs a credential, a call is refused or held,\n  or you need to know what you can use.\n---\n\n" + page
}

// attachTargets returns the agents to attach to.
func attachTargets(home, name string) ([]agentSpec, error) {
	if name != "" {
		spec, ok := agentSkillDir(name)
		if !ok {
			return nil, validationErr("unknown agent: use claude, cursor, codex, hermes, opencode or openclaw")
		}
		return []agentSpec{spec}, nil
	}
	var found []agentSpec
	for _, a := range supportedAgents {
		if st, err := os.Stat(filepath.Join(home, a.spec.baseDir)); err == nil && st.IsDir() {
			found = append(found, a.spec)
		}
	}
	if len(found) == 0 {
		return nil, &agentError{code: exitcode.CodeNotFound, exit: exitcode.NotFound,
			msg: "no agent config found in your home folder", action: "name one: 'onecli attach claude'"}
	}
	return found, nil
}

func (c *AttachCmd) Run(out *output.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home dir: %w", err)
	}
	targets, err := attachTargets(home, c.Agent)
	if err != nil {
		return err
	}
	client := newAgentAPI()
	s, _, err := agentSettings(client)
	if err != nil {
		return err
	}
	page, err := client.Skill(newContext())
	if err != nil {
		return fmt.Errorf("could not fetch the OneCLI skill: %w", err)
	}
	skill := attachSkill(page)

	res := attachResult{Next: "the settings apply from the agent's next start"}
	for _, spec := range targets {
		a := attachedAgent{
			Agent: spec.agentName,
			Skill: filepath.Join(home, spec.baseDir, "skills", attachSkillName, "SKILL.md"),
		}
		if spec.baseDir == ".claude" {
			a.Settings = filepath.Join(home, ".claude", "settings.json")
		} else {
			a.Note = "settings come from the shell: agent.env is read by your profile"
		}
		if !c.DryRun {
			if err := os.MkdirAll(filepath.Dir(a.Skill), 0o750); err != nil {
				return fmt.Errorf("creating %s: %w", filepath.Dir(a.Skill), err)
			}
			if err := os.WriteFile(a.Skill, []byte(skill), 0o600); err != nil {
				return fmt.Errorf("writing %s: %w", a.Skill, err)
			}
			if a.Settings != "" {
				if err := setSettingsEnv(a.Settings, s.Pairs()); err != nil {
					return fmt.Errorf("writing %s: %w", a.Settings, err)
				}
			}
		}
		res.Attached = append(res.Attached, a)
	}
	if c.DryRun {
		return out.WriteDryRun("Would write the OneCLI skill and settings into these agents' config", res)
	}
	return out.WriteFiltered(res, c.Fields)
}

// readSettings reads a JSON settings file, or an empty one when missing.
func readSettings(path string) (map[string]any, error) {
	settings := map[string]any{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, errors.New("the file is not plain JSON; it was left as it is")
	}
	return settings, nil
}

func writeSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return agentenv.WriteFileFresh(path, append(data, '\n'), 0o600)
}

// setSettingsEnv puts pairs into the "env" object of a settings file,
// keeping every other key.
func setSettingsEnv(path string, pairs [][2]string) error {
	settings, err := readSettings(path)
	if err != nil {
		return err
	}
	env, _ := settings["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	for _, kv := range pairs {
		env[kv[0]] = kv[1]
	}
	settings["env"] = env
	return writeSettings(path, settings)
}

// unsetSettingsEnv removes from the "env" object each key that still holds
// the value attach wrote. A value someone changed since is left. Reports
// whether the file changed.
func unsetSettingsEnv(path string, pairs [][2]string) (bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	settings, err := readSettings(path)
	if err != nil {
		return false, err
	}
	env, _ := settings["env"].(map[string]any)
	changed := false
	for _, kv := range pairs {
		if v, ok := env[kv[0]].(string); ok && v == kv[1] {
			delete(env, kv[0])
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	if len(env) == 0 {
		delete(settings, "env")
	}
	return true, writeSettings(path, settings)
}

// detachAll undoes attach for every known agent: the settings that still
// hold this agent's values, and the skill file. Returns what changed.
func detachAll(home string, pairs [][2]string) []string {
	var changed []string
	for _, a := range supportedAgents {
		skill := filepath.Join(home, a.spec.baseDir, "skills", attachSkillName, "SKILL.md")
		if data, err := os.ReadFile(skill); err == nil && strings.HasPrefix(string(data), "---\nname: onecli\n") {
			if os.Remove(skill) == nil {
				_ = os.Remove(filepath.Dir(skill))
				changed = append(changed, skill)
			}
		}
	}
	if len(pairs) > 0 {
		settings := filepath.Join(home, ".claude", "settings.json")
		if ok, err := unsetSettingsEnv(settings, pairs); err == nil && ok {
			changed = append(changed, settings)
		}
	}
	return changed
}
