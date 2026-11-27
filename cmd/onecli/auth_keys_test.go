package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/exitcode"
	"github.com/onecli/onecli-cli/pkg/output"
)

// fakeKeyServer stands in for the API: v3 has /v2/keys, a pre-3.0 server
// answers 404 there and serves the old /v1/user/api-key routes, and gone
// makes those old routes answer 410.
type fakeKeyServer struct {
	v3         bool
	gone       bool
	revokeCode int // DELETE status; 0 = 204
	mu         sync.Mutex
	calls      []string
}

const goneText = "GET /v1/user/api-key was removed: the server no longer shows a stored key. A key is made by name and shown once."

func (f *fakeKeyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	v2Err := func(status int, code, msg, issue string) {
		w.WriteHeader(status)
		details := "[]"
		if issue != "" {
			details = `[{"field":"","issue":"` + issue + `","message":"m"}]`
		}
		_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"` + msg + `","details":` + details + `,"requestId":null}}`))
	}
	switch {
	case r.URL.Path == "/v1/health":
		_, _ = w.Write([]byte(`{}`))
	case strings.HasPrefix(r.URL.Path, "/v2/") && !f.v3:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Not found","type":"not_found"}}`))
	case r.URL.Path == "/v2/keys" && r.Method == http.MethodGet:
		_, _ = w.Write([]byte(`{"data":[{"id":"k_old","name":"laptop","permissions":["manage"],"createdVia":"cli_login","createdAt":"2026-11-01T00:00:00.000Z","expiresAt":null,"lastUsedAt":null}],"nextCursor":null}`))
	case strings.HasPrefix(r.URL.Path, "/v2/keys/") && r.Method == http.MethodDelete:
		switch f.revokeCode {
		case 0:
			w.WriteHeader(http.StatusNoContent)
		default:
			v2Err(f.revokeCode, "not_found", "No such key of yours.", "")
		}
	case r.URL.Path == "/v1/user/api-key" && r.Method == http.MethodGet:
		if f.gone {
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"error":{"message":"` + goneText + `","type":"gone"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"apiKey":"oc_legacy_value"}`))
	case r.URL.Path == "/v1/user/api-key/regenerate" && r.Method == http.MethodPost:
		if f.gone {
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"error":{"message":"POST /v1/user/api-key/regenerate was removed.","type":"gone"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"apiKey":"oc_legacy_regenerated"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeKeyServer) callList() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if c != "GET /v1/health" {
			out = append(out, c)
		}
	}
	return strings.Join(out, ", ")
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	// Encode as pkg/output does: no HTML escaping.
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(buf.String())
}

func wantActionError(t *testing.T, err error, code string, exit int, actionHas string) *actionError {
	t.Helper()
	var ae *actionError
	if !errors.As(err, &ae) {
		t.Fatalf("want actionError, got %T %v", err, err)
	}
	if ae.code != code || ae.exit != exit || !strings.Contains(ae.action, actionHas) {
		t.Fatalf("got code=%s exit=%d action=%q", ae.code, ae.exit, ae.action)
	}
	return ae
}

func TestShowKeys(t *testing.T) {
	tests := []struct {
		name      string
		srv       fakeKeyServer
		wantOut   []string
		notOut    []string
		wantCode  string
		wantCalls string
	}{
		{
			name:      "3.0 lists keys, never a value",
			srv:       fakeKeyServer{v3: true},
			wantOut:   []string{`"id":"k_old"`, `"makeKey"`, "Settings > API keys"},
			notOut:    []string{"oc_", `"apiKey"`},
			wantCalls: "GET /v2/keys",
		},
		{
			name:      "pre-3.0 falls back to the old route",
			srv:       fakeKeyServer{},
			wantOut:   []string{`{"apiKey":"oc_legacy_value"}`},
			wantCalls: "GET /v2/keys, GET /v1/user/api-key",
		},
		{
			name:      "old route 410 is a GONE error with its pointer",
			srv:       fakeKeyServer{gone: true},
			wantCode:  exitcode.CodeGone,
			wantCalls: "GET /v2/keys, GET /v1/user/api-key",
		},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(&tt.srv)
			defer srv.Close()
			v, err := showKeys(context.Background(), api.New(srv.URL, "oc_test"))
			if tt.wantCode != "" {
				ae := wantActionError(t, err, tt.wantCode, exitcode.Error, "Settings > API keys")
				if ae.msg != goneText {
					t.Errorf("msg = %q, want the server's text", ae.msg)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			got := ""
			if v != nil {
				got = toJSON(t, v)
			}
			for _, s := range tt.wantOut {
				if !strings.Contains(got, s) {
					t.Errorf("output %s lacks %s", got, s)
				}
			}
			for _, s := range tt.notOut {
				if strings.Contains(got, s) {
					t.Errorf("output %s must not hold %s", got, s)
				}
			}
			if c := tt.srv.callList(); c != tt.wantCalls {
				t.Errorf("calls = %q, want %q", c, tt.wantCalls)
			}
		})
	}
}

func TestRegenerate(t *testing.T) {
	tests := []struct {
		name      string
		srv       fakeKeyServer
		revoke    string
		want      string
		wantCode  string
		wantExit  int
		wantHas   string // in the error message or action
		wantCalls string
	}{
		{
			name: "3.0 never asks the server to make a key", srv: fakeKeyServer{v3: true},
			wantCode: exitcode.CodeForbidden, wantExit: exitcode.Forbidden, wantHas: "Settings > API keys",
			wantCalls: "GET /v2/keys",
		},
		{
			name: "3.0 --revoke cuts off a key", srv: fakeKeyServer{v3: true},
			revoke: "k_old", want: `{"revoked":"k_old"}`,
			wantCalls: "DELETE /v2/keys/k_old",
		},
		{
			name: "3.0 --revoke, no such key", srv: fakeKeyServer{v3: true, revokeCode: 404},
			revoke: "k_gone", wantHas: "No such key of yours.",
			wantCalls: "DELETE /v2/keys/k_gone, GET /v2/keys",
		},
		{
			name: "pre-3.0 falls back to the old regenerate", srv: fakeKeyServer{},
			want:      `{"apiKey":"oc_legacy_regenerated"}`,
			wantCalls: "GET /v2/keys, POST /v1/user/api-key/regenerate",
		},
		{
			name: "pre-3.0 --revoke explains and touches nothing", srv: fakeKeyServer{},
			revoke: "k_old", wantCode: exitcode.CodeError, wantExit: exitcode.Error, wantHas: "without --revoke",
			wantCalls: "DELETE /v2/keys/k_old, GET /v2/keys",
		},
		{
			name: "old regenerate 410 is GONE with the server's text", srv: fakeKeyServer{gone: true},
			wantCode: exitcode.CodeGone, wantExit: exitcode.Error, wantHas: "regenerate was removed.",
			wantCalls: "GET /v2/keys, POST /v1/user/api-key/regenerate",
		},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(&tt.srv)
			defer srv.Close()
			v, err := regenerate(context.Background(), api.New(srv.URL, "oc_test"), tt.revoke)
			switch {
			case tt.wantCode != "":
				ae := wantActionError(t, err, tt.wantCode, tt.wantExit, "")
				if !strings.Contains(ae.msg+" "+ae.action, tt.wantHas) {
					t.Errorf("error %q / %q lacks %q", ae.msg, ae.action, tt.wantHas)
				}
			case tt.wantHas != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantHas) {
					t.Errorf("err = %v, want %q", err, tt.wantHas)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				if got := toJSON(t, v); got != tt.want {
					t.Errorf("output %s, want %s", got, tt.want)
				}
			}
			if c := tt.srv.callList(); c != tt.wantCalls {
				t.Errorf("calls = %q, want %q", c, tt.wantCalls)
			}
		})
	}
}

func TestRegenerateRejectsBadRevokeID(t *testing.T) {
	for _, id := range []string{"../x", "k?a=1", "k#f", "k%2F", "a b", "k\x01"} {
		var stdout, stderr bytes.Buffer
		err := (&AuthRegenerateApiKeyCmd{Revoke: id, DryRun: true}).Run(output.NewWithWriters(&stdout, &stderr))
		if err == nil || !strings.Contains(err.Error(), "invalid --revoke") || stdout.Len() != 0 {
			t.Errorf("id %q: err=%v stdout=%s", id, err, stdout.String())
		}
	}
}

func TestRegenerateDryRun(t *testing.T) {
	for _, tt := range []struct{ revoke, want string }{
		{"k_old", `"revoke": "k_old"`},
		{"", "servers before 3.0 only"},
	} {
		var stdout, stderr bytes.Buffer
		if err := (&AuthRegenerateApiKeyCmd{Revoke: tt.revoke, DryRun: true}).Run(output.NewWithWriters(&stdout, &stderr)); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), `"dry_run": true`) || !strings.Contains(stdout.String(), tt.want) {
			t.Errorf("stdout = %s", stdout.String())
		}
	}
}

func TestWriteActionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantOK   bool
		wantExit int
		wantErr  string
	}{
		{name: "action error with action", err: fmt.Errorf("wrapped: %w", &actionError{code: exitcode.CodeGone, msg: "gone", action: "do x", exit: exitcode.Error}),
			wantOK: true, wantExit: exitcode.Error, wantErr: `{"error":"gone","code":"GONE","action":"do x"}`},
		{name: "action error without action", err: &actionError{code: exitcode.CodeForbidden, msg: "no", exit: exitcode.Forbidden},
			wantOK: true, wantExit: exitcode.Forbidden, wantErr: `{"error":"no","code":"FORBIDDEN"}`},
		{name: "plain error is left to handleError", err: errors.New("x")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit, ok := writeActionError(output.NewWithWriters(&stdout, &stderr), tt.err)
			if ok != tt.wantOK || exit != tt.wantExit {
				t.Fatalf("ok=%v exit=%d", ok, exit)
			}
			var compact bytes.Buffer
			if stderr.Len() > 0 {
				if err := json.Compact(&compact, stderr.Bytes()); err != nil {
					t.Fatal(err)
				}
			}
			if compact.String() != tt.wantErr || stdout.Len() != 0 {
				t.Errorf("stderr=%s stdout=%s", compact.String(), stdout.String())
			}
		})
	}
}

// pkg/output adds a "hint" key to every object, so the listing must not
// carry one of its own: a duplicate key is invalid for many JSON readers.
func TestShowKeysOutputHasOneHint(t *testing.T) {
	srv := httptest.NewServer(&fakeKeyServer{v3: true})
	defer srv.Close()
	v, err := showKeys(context.Background(), api.New(srv.URL, "oc_test"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	out := output.NewWithWriters(&stdout, &stderr)
	out.SetHint("Manage authentication")
	if err := out.Write(v); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(stdout.String(), `"hint"`); n != 1 {
		t.Errorf("want one \"hint\" key, got %d in %s", n, stdout.String())
	}
}
