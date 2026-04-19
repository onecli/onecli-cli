package main

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/internal/auth"
	"github.com/onecli/onecli-cli/internal/config"
	"github.com/onecli/onecli-cli/pkg/exitcode"
	"github.com/onecli/onecli-cli/pkg/output"
)

// version is set at build time via ldflags.
var version = "dev"

// CLI is the root command. Subcommands are added as fields.
type CLI struct {
	Version VersionCmd `cmd:"" help:"Print version information."`
	Help    HelpCmd    `cmd:"" help:"Show available commands."`
	Agents  AgentsCmd  `cmd:"" help:"Manage agents."`
	Secrets SecretsCmd `cmd:"" help:"Manage secrets."`
	Apps    AppsCmd    `cmd:"" help:"Manage app connections."`
	Rules   RulesCmd   `cmd:"" help:"Manage policy rules."`
	Auth    AuthCmd    `cmd:"" help:"Manage authentication."`
	Config  ConfigCmd  `cmd:"" help:"Manage configuration settings."`
	Migrate MigrateCmd `cmd:"" help:"Migrate data to OneCLI Cloud."`
}

func main() {
	out := output.New()

	// When invoked with no args, --help, or -h, output structured JSON
	// so agents always get machine-readable output.
	if len(os.Args) <= 1 || os.Args[1] == "--help" || os.Args[1] == "-h" {
		cmd := &HelpCmd{}
		if err := cmd.Run(out); err != nil {
			_ = out.Error(exitcode.CodeError, err.Error())
			os.Exit(exitcode.Error)
		}
		return
	}

	cli := &CLI{}
	k, err := kong.New(cli,
		kong.Name("onecli"),
		kong.Description("CLI for managing OneCLI agents, secrets, rules, and configuration."),
		kong.Help(jsonHelpPrinter(out)),
		kong.Bind(out),
	)
	if err != nil {
		_ = out.Error(exitcode.CodeError, err.Error())
		os.Exit(exitcode.Error)
	}

	kCtx, err := k.Parse(os.Args[1:])
	if err != nil {
		_ = out.Error(exitcode.CodeError, err.Error())
		os.Exit(exitcode.Error)
	}

	cmd := kCtx.Command()
	out.SetHintFunc(func() string {
		return hintForCommand(cmd, config.APIHost())
	})
	err = kCtx.Run(out)
	if err != nil {
		handleError(out, err)
	}
}

// handleError maps errors to appropriate exit codes and structured output.
func handleError(out *output.Writer, err error) {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 401:
			_ = out.ErrorWithAction(exitcode.CodeAuthRequired, apiErr.Message, "onecli auth login")
			os.Exit(exitcode.AuthRequired)
		case 404:
			_ = out.Error(exitcode.CodeNotFound, apiErr.Message)
			os.Exit(exitcode.NotFound)
		case 409:
			_ = out.Error(exitcode.CodeConflict, apiErr.Message)
			os.Exit(exitcode.Conflict)
		}
	}

	_ = out.Error(exitcode.CodeError, err.Error())
	os.Exit(exitcode.Error)
}

// newClient creates an API client using the resolved API key and host.
// If no API key is stored, the client is created without one — the server
// decides whether authentication is required (local mode doesn't need it).
func newClient() (*api.Client, error) {
	var key string
	credDir, err := config.CredentialsDir()
	if err == nil {
		store := auth.NewStore(nil, credDir)
		key, _ = store.Load()
	}
	return api.New(config.APIHost(), key), nil
}

// newContext returns a background context for API calls.
func newContext() context.Context {
	return context.Background()
}

// hintForCommand returns a contextual hint message based on the active command group.
func hintForCommand(cmd, host string) string {
	group := strings.SplitN(cmd, " ", 2)[0]
	switch group {
	case "secrets":
		return "Manage your secrets \u2192 " + host
	case "agents":
		return "Manage your agents \u2192 " + host
	case "apps":
		return "Manage your app connections \u2192 " + host
	case "rules":
		return "Manage your policy rules \u2192 " + host
	case "auth":
		return "Manage authentication \u2192 " + host
	case "config":
		return "Manage configuration \u2192 " + host
	case "migrate":
		return "Migrate data to OneCLI Cloud"
	default:
		return ""
	}
}
