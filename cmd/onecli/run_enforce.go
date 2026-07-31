package main

// Enforce mode (`onecli run --enforce`): make gateway governance
// unbypassable for agents whose runtime has an OS-level sandbox with a
// custom-proxy setting. The env-var proxy (HTTPS_PROXY) is cooperative —
// an agent can unset it or pass --noproxy and dial hosts directly. In
// enforce mode the agent's own sandbox (Seatbelt on macOS, bubblewrap on
// Linux) becomes the enforcement layer: sandboxed Bash egress has exactly
// one network path, a loopback forwarder that injects the gateway
// credentials (see run_enforce_forwarder.go). Direct dials fail at the OS.
//
// This file is the NATIVE enforce path, used for agents whose runtime has
// an OS-level sandbox with a custom-proxy setting (currently Claude Code).
// Its sandbox honors --settings for the
// sandbox.* keys, denies writes to every settings.json scope from inside
// the sandbox, and (with allowUnsandboxedCommands=false) ignores the
// dangerouslyDisableSandbox escape hatch — the exact three properties
// enforcement needs.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// enforceSupportedAgents names the agent frameworks enforce mode knows how
// to configure, keyed by agentSpec.agentName.
var enforceSupportedAgents = map[string]bool{
	"Claude Code": true,
}

// claudeSandboxSettings is the sandbox block injected via --settings.
type claudeSandboxSettings struct {
	Sandbox claudeSandbox `json:"sandbox"`
}

type claudeSandbox struct {
	Enabled bool `json:"enabled"`
	// FailIfUnavailable: missing sandbox deps abort startup instead of
	// silently degrading to unsandboxed (= ungoverned) execution.
	FailIfUnavailable bool `json:"failIfUnavailable"`
	// AllowUnsandboxedCommands=false disables the dangerouslyDisableSandbox
	// retry path — the documented escape hatch agents reach for when a
	// command fails inside the sandbox.
	AllowUnsandboxedCommands bool                 `json:"allowUnsandboxedCommands"`
	Network                  claudeSandboxNetwork `json:"network"`
}

type claudeSandboxNetwork struct {
	// HTTPProxyPort points sandboxed egress at the loopback auth
	// forwarder. The sandbox allows only this path out.
	HTTPProxyPort uint16 `json:"httpProxyPort"`
	// AllowedDomains is deliberately wide open: WHICH hosts an agent may
	// reach (and which need human approval) is the gateway's policy
	// decision, where rules live and activity is logged. The sandbox's
	// only job in enforce mode is making the proxy unbypassable.
	AllowedDomains []string `json:"allowedDomains"`
}

// writeEnforceSettings renders the sandbox settings file for Claude Code
// and returns its path. A fresh file per run (the forwarder port is
// per-run), under ~/.onecli so the sandbox's own settings-write denial
// protects it alongside Claude's settings scopes.
func writeEnforceSettings(forwarderPort uint16) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	dir := filepath.Join(home, ".onecli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating settings dir: %w", err)
	}

	settings := claudeSandboxSettings{
		Sandbox: claudeSandbox{
			Enabled:                  true,
			FailIfUnavailable:        true,
			AllowUnsandboxedCommands: false,
			Network: claudeSandboxNetwork{
				HTTPProxyPort:  forwarderPort,
				AllowedDomains: []string{"*"},
			},
		},
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding sandbox settings: %w", err)
	}

	path := filepath.Join(dir, "enforce-sandbox-settings.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("writing sandbox settings: %w", err)
	}
	return path, nil
}

// enforceAgentArgs returns the extra CLI args that activate the sandbox
// settings for the agent invocation. Claude Code merges --settings at
// startup with higher precedence than project settings.
func enforceAgentArgs(settingsPath string) []string {
	return []string{"--settings", settingsPath}
}
