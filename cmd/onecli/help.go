package main

import (
	"strings"

	"github.com/alecthomas/kong"
	"github.com/onecli/onecli-cli/pkg/output"
)

// HelpCmd shows available commands as JSON.
type HelpCmd struct{}

// HelpResponse is the JSON output of the help command.
type HelpResponse struct {
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Description string        `json:"description"`
	Commands    []CommandInfo `json:"commands"`
	Hint        string        `json:"hint"`
}

// CommandInfo describes a single available command.
type CommandInfo struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Args        []ArgInfo `json:"args,omitempty"`
}

// ArgInfo describes a command argument or flag.
type ArgInfo struct {
	Name        string `json:"name"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

func (cmd *HelpCmd) Run(out *output.Writer) error {
	return out.Write(HelpResponse{
		Name:        "onecli",
		Version:     version,
		Description: "CLI for managing OneCLI agents, secrets, rules, and configuration.",
		Commands: []CommandInfo{
			{Name: "run", Description: "Run a command with OneCLI gateway access.", Args: []ArgInfo{
				{Name: "<command>", Required: true, Description: "Command to execute (e.g. claude, cursor, codex)."},
				{Name: "--agent", Description: "OneCLI agent identifier (uses default if omitted)."},
				{Name: "--gateway", Description: "Gateway host:port override (default: derived from API host)."},
				{Name: "--no-ca", Description: "Skip CA cert write and CA trust env injection."},
				{Name: "--dry-run", Description: "Print resolved env and command without executing."},
			}},
			{Name: "agents list", Description: "List all agents."},
			{Name: "agents get-default", Description: "Get the default agent."},
			{Name: "agents create", Description: "Create a new agent.", Args: []ArgInfo{
				{Name: "--name", Required: true, Description: "Display name for the agent."},
				{Name: "--identifier", Required: true, Description: "Unique identifier (lowercase letters, numbers, hyphens)."},
			}},
			{Name: "agents delete", Description: "Delete an agent.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent to delete."},
			}},
			{Name: "agents rename", Description: "Rename an agent.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent to rename."},
				{Name: "--name", Required: true, Description: "New display name."},
			}},
			{Name: "agents regenerate-token", Description: "Regenerate an agent's access token.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
			}},
			{Name: "agents secrets", Description: "List secrets assigned to an agent.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
			}},
			{Name: "agents set-secrets", Description: "Set secrets assigned to an agent.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
				{Name: "--secret-ids", Required: true, Description: "Comma-separated list of secret IDs."},
			}},
			{Name: "agents set-secret-mode", Description: "Set an agent's secret mode.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the agent."},
				{Name: "--mode", Required: true, Description: "Secret mode: 'all' or 'selective'."},
			}},
			{Name: "secrets list", Description: "List all secrets."},
			{Name: "secrets create", Description: "Create a new secret.", Args: []ArgInfo{
				{Name: "--name", Required: true, Description: "Display name for the secret."},
				{Name: "--type", Required: true, Description: "Secret type: 'anthropic' or 'generic'."},
				{Name: "--value", Required: true, Description: "Secret value (e.g. API key)."},
				{Name: "--host-pattern", Required: true, Description: "Host pattern to match."},
			}},
			{Name: "secrets update", Description: "Update an existing secret.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the secret to update."},
			}},
			{Name: "secrets delete", Description: "Delete a secret.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the secret to delete."},
			}},
			{Name: "apps list", Description: "List all apps with config and connection status."},
			{Name: "apps get", Description: "Get a single app with setup guidance.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
			}},
			{Name: "apps configure", Description: "Save OAuth credentials (BYOC) for a provider.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
				{Name: "--client-id", Required: true, Description: "OAuth client ID."},
				{Name: "--client-secret", Required: true, Description: "OAuth client secret."},
			}},
			{Name: "apps remove", Description: "Remove OAuth credentials for a provider.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
			}},
			{Name: "apps disconnect", Description: "Disconnect an app connection.", Args: []ArgInfo{
				{Name: "--provider", Required: true, Description: "Provider name (e.g. 'github', 'gmail')."},
			}},
			{Name: "rules list", Description: "List all policy rules."},
			{Name: "rules create", Description: "Create a new policy rule.", Args: []ArgInfo{
				{Name: "--name", Required: true, Description: "Display name for the rule."},
				{Name: "--host-pattern", Required: true, Description: "Host pattern to match."},
				{Name: "--action", Required: true, Description: "Action: 'block' or 'rate_limit'."},
			}},
			{Name: "rules update", Description: "Update an existing policy rule.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the rule to update."},
			}},
			{Name: "rules delete", Description: "Delete a policy rule.", Args: []ArgInfo{
				{Name: "--id", Required: true, Description: "ID of the rule to delete."},
			}},
			{Name: "auth login", Description: "Store API key for authentication."},
			{Name: "auth logout", Description: "Remove stored API key."},
			{Name: "auth status", Description: "Show authentication status."},
			{Name: "auth api-key", Description: "Show your current API key."},
			{Name: "auth regenerate-api-key", Description: "Regenerate your API key."},
			{Name: "config get <key>", Description: "Get a config value."},
			{Name: "config set <key> <value>", Description: "Set a config value."},
			{Name: "migrate", Description: "Migrate data to OneCLI Cloud.", Args: []ArgInfo{
				{Name: "--cloud-key", Required: true, Description: "OneCLI Cloud API key."},
			}},
			{Name: "version", Description: "Print version information."},
		},
		Hint: "run 'onecli <command> --help' to see available subcommands and flags",
	})
}

// subcommandHelpResponse is the JSON output for subcommand-level --help.
type subcommandHelpResponse struct {
	Commands []CommandInfo `json:"commands"`
}

// jsonHelpPrinter returns a kong.HelpPrinter that outputs JSON.
func jsonHelpPrinter(out *output.Writer) kong.HelpPrinter {
	return func(options kong.HelpOptions, ctx *kong.Context) error {
		selected := ctx.Selected()

		// Root level -> full help response.
		if selected == nil || selected.Type == kong.ApplicationNode {
			cmd := &HelpCmd{}
			return cmd.Run(out)
		}

		// Subcommand level -> collect leaf commands under this node.
		var commands []CommandInfo
		prefix := kongParentPrefix(selected)
		collectKongLeafCommands(selected, prefix, &commands)
		return out.Write(subcommandHelpResponse{Commands: commands})
	}
}

// collectKongLeafCommands walks a Kong node tree and collects leaf commands.
func collectKongLeafCommands(node *kong.Node, prefix string, commands *[]CommandInfo) {
	if node.Hidden {
		return
	}

	path := node.Name
	if prefix != "" {
		path = prefix + " " + node.Name
	}

	// Intermediate node -> recurse into children.
	if len(node.Children) > 0 {
		for _, child := range node.Children {
			collectKongLeafCommands(child, path, commands)
		}
		return
	}

	// Leaf command -> collect positional args and flags.
	cmd := CommandInfo{
		Name:        path,
		Description: node.Help,
	}
	for _, pos := range node.Positional {
		cmd.Args = append(cmd.Args, ArgInfo{
			Name:        "<" + pos.Name + ">",
			Required:    pos.Required,
			Description: pos.Help,
		})
	}
	for _, flag := range node.Flags {
		if flag.Name == "help" || flag.Hidden {
			continue
		}
		cmd.Args = append(cmd.Args, ArgInfo{
			Name:        "--" + flag.Name,
			Required:    flag.Required,
			Description: flag.Help,
		})
	}
	*commands = append(*commands, cmd)
}

// kongParentPrefix builds the command path prefix from a node's parent chain,
// excluding the application root.
func kongParentPrefix(node *kong.Node) string {
	var parts []string
	for n := node.Parent; n != nil && n.Type != kong.ApplicationNode; n = n.Parent {
		parts = append([]string{n.Name}, parts...)
	}
	return strings.Join(parts, " ")
}
