package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/internal/auth"
	"github.com/onecli/onecli-cli/internal/config"
	"github.com/onecli/onecli-cli/pkg/exitcode"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// version is set at build time via ldflags.
var version = "dev"

// CLI is the root command. Subcommands are added as fields.
type CLI struct {
	Run      RunCmd      `cmd:"" help:"Run a command with OneCLI gateway access."`
	Version  VersionCmd  `cmd:"" help:"Print version information."`
	Help     HelpCmd     `cmd:"" help:"Show available commands."`
	Agents   AgentsCmd   `cmd:"" help:"Manage agents."`
	Secrets  SecretsCmd  `cmd:"" help:"Manage secrets."`
	Apps     AppsCmd     `cmd:"" help:"Manage app connections."`
	Rules    RulesCmd    `cmd:"" help:"Manage legacy policy rules (cloud deployments reject writes — see 'onecli policy')."`
	Policy   PolicyCmd   `cmd:"" help:"Manage policy rules on the policy engine (draft → publish)."`
	Projects ProjectsCmd `cmd:"" help:"Manage projects."`
	Org      OrgCmd      `cmd:"" help:"Organization-scoped management (secrets, rules, policy, connections, apps, settings)."`
	Vaults   VaultsCmd   `cmd:"" help:"List external vault connections."`
	Counts   CountsCmd   `cmd:"" help:"Show the project's resource counts."`
	Auth     AuthCmd     `cmd:"" help:"Manage authentication."`
	Config   ConfigCmd   `cmd:"" help:"Manage configuration settings."`
	Sandbox  SandboxCmd  `cmd:"" help:"Inspect and audit the enforce-mode sandbox."`
	Migrate  MigrateCmd  `cmd:"" help:"Migrate data to OneCLI Cloud."`
}

func main() {
	out := output.New()

	// Hidden sidecar mode: the enforce-mode auth forwarder forked by
	// `onecli run --enforce` re-invokes this binary. Handled before kong
	// so the flag never appears in help or completion.
	if pid, ok := parseEnforceForwarderArgs(os.Args[1:]); ok {
		runEnforceForwarder(pid)
		return
	}
	// Same pattern for the transparent-redirect listener, which must also
	// outlive the syscall.Exec that replaces this process with the agent.
	if pid, ok := parseTransparentSidecarArgs(os.Args[1:]); ok {
		runTransparentSidecar(pid)
		return
	}

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
		kong.Description("CLI for managing OneCLI agents, secrets, rules, projects, and configuration."),
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
		// A 400/401 demanding a project header is a scoping problem, not an
		// auth one — "onecli auth login" would be misleading advice.
		if (apiErr.StatusCode == 400 || apiErr.StatusCode == 401) &&
			strings.Contains(apiErr.Message, "X-Project-Id") {
			_ = out.ErrorWithAction(
				exitcode.CodeError,
				apiErr.Message,
				"pass --project <slug> or run 'onecli config set project <slug>'",
			)
			os.Exit(exitcode.Error)
		}
		switch apiErr.StatusCode {
		case 401:
			_ = out.ErrorWithAction(exitcode.CodeAuthRequired, apiErr.Message, "onecli auth login")
			os.Exit(exitcode.AuthRequired)
		case 403:
			_ = out.Error(exitcode.CodeForbidden, apiErr.Message)
			os.Exit(exitcode.Forbidden)
		case 404:
			_ = out.Error(exitcode.CodeNotFound, apiErr.Message)
			os.Exit(exitcode.NotFound)
		case 409:
			_ = out.Error(exitcode.CodeConflict, apiErr.Message)
			os.Exit(exitcode.Conflict)
		case 410:
			// A retired endpoint: the server message names the replacement.
			_ = out.ErrorWithAction(
				exitcode.CodeGone,
				apiErr.Message,
				"project access: 'onecli agents grants --help' — org rules: 'onecli org policy --help'",
			)
			os.Exit(exitcode.Error)
		case 422:
			_ = out.Error(exitcode.CodeValidation, apiErr.Message)
			os.Exit(exitcode.Error)
		}
	}

	_ = out.Error(exitcode.CodeError, err.Error())
	os.Exit(exitcode.Error)
}

// loadStoredAPIKey returns the resolved API key (env or credential file),
// or "" — used for fail-fast key-shape checks; the client loads it itself.
func loadStoredAPIKey() string {
	credDir, err := config.CredentialsDir()
	if err != nil {
		return ""
	}
	key, _ := auth.NewStore(nil, credDir).Load()
	return key
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

// resolveProject returns the project from the flag value, falling back to config.
// Returns an error if the resolved value fails input validation.
func resolveProject(flag string) (string, error) {
	v := flag
	if v == "" {
		v = config.Project()
	}
	if v == "" {
		return "", nil
	}
	if err := validate.ResourceID(v); err != nil {
		return "", fmt.Errorf("invalid project slug: %w", err)
	}
	return v, nil
}

// resolveAgent returns the agent identifier from the flag value, falling back
// to config (ONECLI_AGENT env var > config file). Empty means the project's
// server-side default agent. Returns an error if the resolved value fails
// input validation.
func resolveAgent(flag string) (string, error) {
	v := flag
	if v == "" {
		v = config.Agent()
	}
	if v == "" {
		return "", nil
	}
	if err := validate.ResourceID(v); err != nil {
		return "", fmt.Errorf("invalid agent identifier: %w", err)
	}
	return v, nil
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
	case "projects":
		return "Manage your projects \u2192 " + host
	case "org":
		return "Manage organization-level resources \u2192 " + host
	case "auth":
		return "Manage authentication \u2192 " + host
	case "config":
		return "Manage configuration \u2192 " + host
	case "run":
		return "OneCLI gateway docs \u2192 " + host
	case "migrate":
		return "Migrate data to OneCLI Cloud"
	default:
		return ""
	}
}
