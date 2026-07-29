package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alecthomas/kong"

	"github.com/onecli/onecli-cli/pkg/output"
)

// help.go's command list is hand-maintained while subcommand --help walks the
// live Kong tree — two independent sources of truth that have historically
// drifted (commands retired in one and neutral in the other). This pins them
// together in both directions, plus RETIRED-marker parity.

// helpListEntries runs the hand-maintained `onecli help` and returns its
// entries, with positional suffixes trimmed ("config get <key>" → "config get").
func helpListEntries(t *testing.T) map[string]string {
	t.Helper()
	var buf bytes.Buffer
	out := output.NewWithWriters(&buf, &bytes.Buffer{})
	if err := (&HelpCmd{}).Run(out); err != nil {
		t.Fatalf("running help: %v", err)
	}
	var resp HelpResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("help output is not JSON: %v", err)
	}
	entries := make(map[string]string, len(resp.Commands))
	for _, cmd := range resp.Commands {
		name := cmd.Name
		if i := strings.Index(name, " <"); i > 0 {
			name = name[:i]
		}
		entries[name] = cmd.Description
	}
	return entries
}

// kongLeafEntries builds the live Kong tree and returns every visible leaf
// command with its help string.
func kongLeafEntries(t *testing.T) map[string]string {
	t.Helper()
	k, err := kong.New(&CLI{}, kong.Name("onecli"))
	if err != nil {
		t.Fatalf("building the kong tree: %v", err)
	}
	var commands []CommandInfo
	for _, node := range k.Model.Children {
		collectKongLeafCommands(node, "", &commands)
	}
	entries := make(map[string]string, len(commands))
	for _, cmd := range commands {
		entries[cmd.Name] = cmd.Description
	}
	return entries
}

// Commands that appear in exactly one surface on purpose.
var helpDriftAllowlist = map[string]bool{
	// The help meta-command deliberately doesn't list itself.
	"help": true,
}

func TestHelpListMatchesTheKongTree(t *testing.T) {
	helpEntries := helpListEntries(t)
	kongEntries := kongLeafEntries(t)

	for name := range kongEntries {
		if helpDriftAllowlist[name] {
			continue
		}
		if _, ok := helpEntries[name]; !ok {
			t.Errorf("kong command %q is missing from the hand-maintained help list (help.go)", name)
		}
	}
	for name := range helpEntries {
		if helpDriftAllowlist[name] {
			continue
		}
		if _, ok := kongEntries[name]; !ok {
			t.Errorf("help.go lists %q, which is not a command in the kong tree", name)
		}
	}
}

func TestRetiredMarkersAgreeAcrossHelpSurfaces(t *testing.T) {
	helpEntries := helpListEntries(t)
	kongEntries := kongLeafEntries(t)

	for name, kongDesc := range kongEntries {
		helpDesc, ok := helpEntries[name]
		if !ok || helpDriftAllowlist[name] {
			continue // the presence test above owns missing entries
		}
		kongRetired := strings.HasPrefix(kongDesc, "RETIRED")
		helpRetired := strings.HasPrefix(helpDesc, "RETIRED")
		if kongRetired != helpRetired {
			t.Errorf(
				"%q disagrees on retirement: kong help says retired=%v, help.go says retired=%v",
				name, kongRetired, helpRetired,
			)
		}
	}
}
