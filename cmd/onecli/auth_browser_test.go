package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/exitcode"
)

// fakeCLIAuth stands in for /v1/auth/cli: polls answer from a script, the
// last entry repeating.
type fakeCLIAuth struct {
	session string   // body of POST /session
	polls   []string // bodies of GET /poll, in order
	mu      sync.Mutex
	n       int
	codes   []string
}

func (f *fakeCLIAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/v1/health":
		_, _ = w.Write([]byte(`{}`))
	case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/cli/session":
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusBadRequest) // must start with no key
			return
		}
		_, _ = w.Write([]byte(f.session))
	case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/cli/poll":
		f.mu.Lock()
		f.codes = append(f.codes, r.URL.Query().Get("code"))
		i := min(f.n, len(f.polls)-1)
		f.n++
		f.mu.Unlock()
		_, _ = w.Write([]byte(f.polls[i]))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

const goodSession = `{"code":"abcdef0123456789","auth_url":"http://localhost:1/auth/cli?code=abcdef0123456789"}`

func TestBrowserLogin(t *testing.T) {
	tests := []struct {
		name      string
		session   string
		polls     []string
		openErr   error
		timeout   time.Duration
		wantKey   string
		wantCode  string // actionError code
		wantErr   string // plain error substring
		wantPolls int
		wantNote  string
	}{
		{name: "pending then ok", session: goodSession,
			polls:   []string{`{"status":"pending"}`, `{"status":"pending"}`, `{"status":"ok","api_key":"oc_key_abc","workspace_id":"w1"}`},
			timeout: time.Minute, wantKey: "oc_key_abc", wantPolls: 3, wantNote: "abcdef01"},
		{name: "expired", session: goodSession, polls: []string{`{"status":"expired"}`},
			timeout: time.Minute, wantCode: exitcode.CodeAuthRequired, wantPolls: 1},
		{name: "times out", session: goodSession, polls: []string{`{"status":"pending"}`},
			timeout: 0, wantCode: exitcode.CodeAuthRequired, wantPolls: 1},
		{name: "unknown status", session: goodSession, polls: []string{`{"status":"weird"}`},
			timeout: time.Minute, wantErr: "unexpected sign-in status", wantPolls: 1},
		{name: "ok with a bad key", session: goodSession, polls: []string{`{"status":"ok","api_key":"nope"}`},
			timeout: time.Minute, wantErr: "bad key", wantPolls: 1},
		{name: "link that is not http", session: `{"code":"c","auth_url":"file:///etc/passwd"}`,
			polls: []string{`{"status":"ok","api_key":"oc_key_abc"}`}, timeout: time.Minute, wantErr: "bad sign-in link"},
		{name: "no code", session: `{"code":"","auth_url":"http://x/auth/cli"}`,
			polls: []string{`{"status":"pending"}`}, timeout: time.Minute, wantErr: "no sign-in code"},
		{name: "browser fails to open: still works", session: goodSession, openErr: errors.New("no display"),
			polls:   []string{`{"status":"ok","api_key":"oc_key_abc"}`},
			timeout: time.Minute, wantKey: "oc_key_abc", wantPolls: 1, wantNote: "Could not open a browser"},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeCLIAuth{session: tt.session, polls: tt.polls}
			srv := httptest.NewServer(f)
			defer srv.Close()
			var notes []string
			var opened string
			sleeps := 0
			b := &browserLogin{
				client:   api.New(srv.URL, ""),
				open:     func(u string) error { opened = u; return tt.openErr },
				notify:   func(m string) { notes = append(notes, m) },
				interval: time.Second,
				timeout:  tt.timeout,
				sleep:    func(time.Duration) { sleeps++ },
			}
			key, err := b.run(context.Background())
			switch {
			case tt.wantCode != "":
				wantActionError(t, err, tt.wantCode, exitcode.AuthRequired, "auth login --browser")
			case tt.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
			default:
				if err != nil || key != tt.wantKey {
					t.Fatalf("key=%q err=%v", key, err)
				}
				if opened == "" || !strings.HasPrefix(opened, "http://localhost:1/auth/cli") {
					t.Errorf("opened %q", opened)
				}
			}
			if f.n != tt.wantPolls {
				t.Errorf("polls = %d, want %d", f.n, tt.wantPolls)
			}
			if tt.wantPolls > 1 && sleeps != tt.wantPolls-1 {
				t.Errorf("sleeps = %d, want %d", sleeps, tt.wantPolls-1)
			}
			for _, c := range f.codes {
				if c != "abcdef0123456789" {
					t.Errorf("polled with code %q", c)
				}
			}
			if tt.wantNote != "" && !strings.Contains(strings.Join(notes, "\n"), tt.wantNote) {
				t.Errorf("notes %q lack %q", notes, tt.wantNote)
			}
			if strings.Contains(strings.Join(notes, "\n"), "oc_key_") {
				t.Error("the key must never be shown on stderr")
			}
		})
	}
}

func TestBrowserLoginNoOpenOnlyPrints(t *testing.T) {
	f := &fakeCLIAuth{session: goodSession, polls: []string{`{"status":"ok","api_key":"oc_key_abc"}`}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	var notes []string
	b := &browserLogin{client: api.New(srv.URL, ""), notify: func(m string) { notes = append(notes, m) },
		timeout: time.Minute, sleep: func(time.Duration) {}}
	if _, err := b.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "http://localhost:1/auth/cli?code=abcdef0123456789") {
		t.Errorf("notes = %q", notes)
	}
}

func TestAuthLoginFlagRules(t *testing.T) {
	tests := []struct {
		cmd  AuthLoginCmd
		want string
	}{
		{AuthLoginCmd{Browser: true, APIKey: "oc_x", Timeout: time.Minute}, "not both"},
		{AuthLoginCmd{Browser: true, Timeout: 0}, "--timeout must be positive"},
		{AuthLoginCmd{NoOpen: true, APIKey: "oc_x"}, "--no-open needs --browser"},
	}
	for _, tt := range tests {
		err := tt.cmd.Run(nil)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%+v: err = %v, want %q", tt.cmd, err, tt.want)
		}
	}
}
