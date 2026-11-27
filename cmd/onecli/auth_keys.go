package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/exitcode"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

// makeKeyAction is the next step wherever the CLI cannot make a key itself.
// The browser login comes first: its key lands on your default workspace,
// so every command works. A key from Settings > API keys names no
// workspace, so it needs a project setting, and `onecli run` refuses it.
const makeKeyAction = "run `onecli auth login --browser` (a key for this terminal, works with every command). " +
	"Or make a key in Settings > API keys, `onecli auth login --api-key <key>` and `onecli config set project <workspace>`; " +
	"that key cannot use `onecli run`"

// revokeHint is how to cut off a key, for example a leaked one.
const revokeHint = "To cut off a key: `onecli auth regenerate-api-key --revoke <id>` (ids from `onecli auth api-key`)"

// keysClient is the part of the API client the key commands use.
type keysClient interface {
	ListKeys(ctx context.Context) ([]api.Key, error)
	RevokeKey(ctx context.Context, id string) error
	GetAPIKey(ctx context.Context) (*api.APIKeyResponse, error)
	RegenerateAPIKey(ctx context.Context) (*api.APIKeyResponse, error)
}

// goneError turns a 410 from a retired key route into a GONE error that
// keeps the server's text and says where keys are made now. Other errors
// pass through.
func goneError(err error) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusGone {
		return &actionError{
			code:   exitcode.CodeGone,
			msg:    apiErr.Message,
			action: makeKeyAction,
			exit:   exitcode.Error,
		}
	}
	return err
}

// AuthApiKeyCmd is `onecli auth api-key`.
type AuthApiKeyCmd struct {
	Fields string `optional:"" help:"Comma-separated list of fields to include in output."`
}

// AuthKeysResponse is the JSON output of `auth api-key` on a server with
// /v2/keys. A listing never holds a key's value. The field is not named
// "hint": pkg/output adds its own "hint" to every object.
type AuthKeysResponse struct {
	Keys    []api.Key `json:"keys"`
	MakeKey string    `json:"makeKey"`
}

func (c *AuthApiKeyCmd) Run(out *output.Writer) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	v, err := showKeys(newContext(), client)
	if err != nil {
		return err
	}
	return out.WriteFiltered(v, c.Fields)
}

// showKeys lists the caller's keys from /v2/keys, or reads the one stored
// key from the old route on a server older than 3.0.
func showKeys(ctx context.Context, client keysClient) (any, error) {
	keys, err := client.ListKeys(ctx)
	if errors.Is(err, api.ErrKeysUnsupported) {
		resp, oldErr := client.GetAPIKey(ctx)
		if oldErr != nil {
			return nil, goneError(oldErr)
		}
		return resp, nil
	}
	if err != nil {
		return nil, err
	}
	return AuthKeysResponse{
		Keys:    keys,
		MakeKey: "A key's value is shown once, when it is made. To use a new key: " + makeKeyAction + ". " + revokeHint + ".",
	}, nil
}

// AuthRegenerateApiKeyCmd is `onecli auth regenerate-api-key`.
type AuthRegenerateApiKeyCmd struct {
	Revoke string `optional:"" help:"Revoke this key now (an id from 'onecli auth api-key'). Cuts off a leaked key."`
	DryRun bool   `optional:"" name:"dry-run" help:"Validate the request without executing it."`
}

// AuthRevokeResponse is the JSON output of `auth regenerate-api-key --revoke`.
type AuthRevokeResponse struct {
	Revoked string `json:"revoked"`
}

func (c *AuthRegenerateApiKeyCmd) Run(out *output.Writer) error {
	if c.Revoke != "" {
		if err := validate.ResourceID(c.Revoke); err != nil {
			return fmt.Errorf("invalid --revoke: %w", err)
		}
	}
	if c.DryRun {
		if c.Revoke != "" {
			return out.WriteDryRun("Would revoke key "+c.Revoke, map[string]string{"revoke": c.Revoke})
		}
		return out.WriteDryRun("Would regenerate the API key (servers before 3.0 only)", nil)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	v, err := regenerate(newContext(), client, c.Revoke)
	if err != nil {
		return err
	}
	return out.Write(v)
}

// regenerate revokes the given key, or, with none given, replaces the
// stored key on a server older than 3.0. A 3.0 server makes a key only for
// a signed-in person, never for a key, so the CLI never asks it to.
func regenerate(ctx context.Context, client keysClient, revoke string) (any, error) {
	if revoke != "" {
		err := client.RevokeKey(ctx, revoke)
		if api.IsStatus(err, http.StatusNotFound) {
			// No such key of the caller's, or no /v2/keys at all.
			if _, listErr := client.ListKeys(ctx); errors.Is(listErr, api.ErrKeysUnsupported) {
				return nil, &actionError{
					code:   exitcode.CodeError,
					msg:    "this server has no /v2/keys, so a key cannot be revoked by id",
					action: "run `onecli auth regenerate-api-key` without --revoke: on this server it replaces your key and the old one stops working",
					exit:   exitcode.Error,
				}
			}
		}
		if err != nil {
			return nil, err
		}
		return AuthRevokeResponse{Revoked: revoke}, nil
	}

	_, err := client.ListKeys(ctx)
	if errors.Is(err, api.ErrKeysUnsupported) {
		resp, oldErr := client.RegenerateAPIKey(ctx)
		if oldErr != nil {
			return nil, goneError(oldErr)
		}
		return resp, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, &actionError{
		code:   exitcode.CodeForbidden,
		msg:    "this server makes a key only for a signed-in person, never for a key, so the CLI cannot make one",
		action: makeKeyAction + ". " + revokeHint,
		exit:   exitcode.Forbidden,
	}
}
