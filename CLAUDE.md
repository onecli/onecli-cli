# OneCLI CLI

Agent-first CLI for managing OneCLI agents, secrets, and configuration.

## Commands

```bash
task build         # Build the onecli binary
task test          # Run all tests
task test:race     # Run tests with race detector
task lint          # Run golangci-lint
task fmt           # Format all Go files
task tidy          # Clean up dependencies
task clean         # Remove build artifacts
```

## Project Structure

```
cmd/onecli/          # CLI entry point and commands (Kong framework)
internal/api/        # HTTP client for OneCLI REST API
internal/config/     # Config file + env resolution
internal/auth/       # API key storage (keychain + file fallback)
pkg/output/          # JSON writer (structured stdout/stderr)
pkg/exitcode/        # Exit codes and string codes
pkg/validate/        # Input hardening at command boundaries
```

## Agent-First Output Rules

1. **All stdout is valid JSON** — never print plain text to stdout
2. **Errors go to stderr as JSON** — `{"error": "...", "code": "...", "action": "..."}`
3. **Exit codes are semantic** — 0 success, 1 error, 2 auth, 3 not found, 4 conflict
4. **`--fields` filters output** — agents can request only the fields they need
5. **`--quiet` extracts single field** — one value per line for piping
6. **`--dry-run` previews mutations** — `{"dry_run": true, "description": "...", "payload": {...}}`

## Shared Packages

- `pkg/output` — all output goes through `output.Writer`. Never use `fmt.Print` or `os.Stdout`.
- `pkg/exitcode` — exit codes and string codes. Map API 401 → AuthRequired, 404 → NotFound, 409 → Conflict.
- `pkg/validate` — validate resource IDs, URLs, API keys at command boundaries.

## Go Conventions

- Use `context.Background()` for all API calls
- Accept interfaces, return structs (API client pattern)
- Wrap errors with `fmt.Errorf("doing X: %w", err)`
- Use const arrow functions pattern: command structs with `Run(out *output.Writer) error` methods
- Kong struct tags for CLI parsing — `required`, `optional`, `name`, `help`
