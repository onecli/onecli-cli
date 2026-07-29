package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The grants surface replaces the retired equipment writes, so the properties
// that matter are the PATH (the old twins are 410 now — a wrong path fails at
// runtime, not compile time) and the exact wire BODY (the server's schema
// requires both arrays on a custom grant and rejects null lists).

func TestGrantsCallTheCanonicalPaths(t *testing.T) {
	tests := []struct {
		name       string
		wantMethod string
		wantPath   string
		wantQuery  string
		body       string
		call       func(*Client) error
	}{
		{
			name:       "get agent grants",
			wantMethod: http.MethodGet,
			wantPath:   "/v1/agents/a1/grants",
			body:       `{"agentId":"a1","mode":"grants","connections":[],"secrets":[]}`,
			call: func(c *Client) error {
				_, err := c.GetAgentGrants(context.Background(), "", "a1")
				return err
			},
		},
		{
			name:       "attach connection",
			wantMethod: http.MethodPut,
			wantPath:   "/v1/agents/a1/grants/connections/conn-1",
			body:       `{"agentId":"a1","mode":"grants","connections":[],"secrets":[]}`,
			call: func(c *Client) error {
				_, err := c.SetAgentConnectionGrant(context.Background(), "", "a1", "conn-1", ConnectionGrantInput{Access: "full"})
				return err
			},
		},
		{
			name:       "detach connection",
			wantMethod: http.MethodDelete,
			wantPath:   "/v1/agents/a1/grants/connections/conn-1",
			call: func(c *Client) error {
				return c.RemoveAgentConnectionGrant(context.Background(), "", "a1", "conn-1")
			},
		},
		{
			name:       "attach secret",
			wantMethod: http.MethodPut,
			wantPath:   "/v1/agents/a1/grants/secrets/sec-1",
			body:       `{"agentId":"a1","mode":"grants","connections":[],"secrets":[]}`,
			call: func(c *Client) error {
				_, err := c.SetAgentSecretGrant(context.Background(), "", "a1", "sec-1")
				return err
			},
		},
		{
			name:       "detach secret",
			wantMethod: http.MethodDelete,
			wantPath:   "/v1/agents/a1/grants/secrets/sec-1",
			call: func(c *Client) error {
				return c.RemoveAgentSecretGrant(context.Background(), "", "a1", "sec-1")
			},
		},
		{
			name:       "get connection grants",
			wantMethod: http.MethodGet,
			wantPath:   "/v1/connections/conn-1/grants",
			body:       `{"connectionId":"conn-1","agents":[]}`,
			call: func(c *Client) error {
				_, err := c.GetConnectionGrants(context.Background(), "", "conn-1")
				return err
			},
		},
		{
			name:       "agents with grants summary",
			wantMethod: http.MethodGet,
			wantPath:   "/v1/agents",
			wantQuery:  "include=grants-summary",
			body:       `[]`,
			call: func(c *Client) error {
				_, err := c.ListAgentsWithGrantsSummary(context.Background(), "")
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod, gotPath, gotQuery string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				gotQuery = r.URL.RawQuery
				if tt.body == "" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c := newWithPrefix(srv.URL, "oc_test", "")
			if err := tt.call(c); err != nil {
				t.Fatalf("call failed: %v", err)
			}
			if gotMethod != tt.wantMethod {
				t.Errorf("method = %q, want %q", gotMethod, tt.wantMethod)
			}
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
			if tt.wantQuery != "" && gotQuery != tt.wantQuery {
				t.Errorf("query = %q, want %q", gotQuery, tt.wantQuery)
			}
		})
	}
}

func TestSetConnectionGrantBodies(t *testing.T) {
	tests := []struct {
		name     string
		input    ConnectionGrantInput
		wantBody string
	}{
		{
			name:     "full sends exactly the access field",
			input:    ConnectionGrantInput{Access: "full"},
			wantBody: `{"access":"full"}`,
		},
		{
			name: "custom sends both arrays",
			input: ConnectionGrantInput{
				Access: "custom",
				Allow:  []string{"search_messages"},
				Ask:    []string{"send_email"},
			},
			wantBody: `{"access":"custom","allow":["search_messages"],"ask":["send_email"]}`,
		},
		{
			name:  "custom with an empty list sends [] never null or a missing key",
			input: ConnectionGrantInput{Access: "custom", Allow: []string{"get_message"}},
			// Ask was nil; the wire must still carry "ask":[] — the server's
			// schema requires the key and rejects null.
			wantBody: `{"access":"custom","allow":["get_message"],"ask":[]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				gotBody = string(data)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"agentId":"a1","mode":"grants","connections":[],"secrets":[]}`))
			}))
			defer srv.Close()

			c := newWithPrefix(srv.URL, "oc_test", "")
			if _, err := c.SetAgentConnectionGrant(context.Background(), "", "a1", "c1", tt.input); err != nil {
				t.Fatalf("call failed: %v", err)
			}
			if gotBody != tt.wantBody {
				t.Errorf("body = %s, want %s", gotBody, tt.wantBody)
			}
		})
	}
}

func TestSetSecretGrantSendsNoBody(t *testing.T) {
	var gotLength int64
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLength = r.ContentLength
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agentId":"a1","mode":"grants","connections":[],"secrets":[]}`))
	}))
	defer srv.Close()

	c := newWithPrefix(srv.URL, "oc_test", "")
	if _, err := c.SetAgentSecretGrant(context.Background(), "", "a1", "s1"); err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if gotLength != 0 {
		t.Errorf("content length = %d, want 0 (the secret PUT takes no body)", gotLength)
	}
	if gotContentType != "" {
		t.Errorf("content type = %q, want none", gotContentType)
	}
}

func TestGrantDetachHandles204(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newWithPrefix(srv.URL, "oc_test", "")
	if err := c.RemoveAgentConnectionGrant(context.Background(), "", "a1", "c1"); err != nil {
		t.Fatalf("detach connection on 204: %v", err)
	}
	if err := c.RemoveAgentSecretGrant(context.Background(), "", "a1", "s1"); err != nil {
		t.Fatalf("detach secret on 204: %v", err)
	}
}

func TestAgentGrantsDecodesBothArms(t *testing.T) {
	label := "Work"
	payload, _ := json.Marshal(AgentGrants{
		AgentID: "a1",
		Mode:    "grants",
		Connections: []AgentGrantConnection{{
			ConnectionID: "c1",
			Provider:     "gmail",
			Label:        &label,
			Scope:        "organization",
			Access:       "custom",
			Allow:        []string{"search_messages"},
			Ask:          []string{"send_email"},
		}},
		Secrets: []AgentGrantSecret{{
			SecretID: "s1", Name: "STRIPE_KEY", Type: "generic", Scope: "project",
		}},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	c := newWithPrefix(srv.URL, "oc_test", "")
	got, err := c.GetAgentGrants(context.Background(), "", "a1")
	if err != nil {
		t.Fatalf("get grants: %v", err)
	}
	conn := got.Connections[0]
	if conn.Access != "custom" || conn.Scope != "organization" || *conn.Label != "Work" {
		t.Errorf("connection arm mangled: %+v", conn)
	}
	if len(conn.Allow) != 1 || len(conn.Ask) != 1 {
		t.Errorf("tool lists mangled: %+v", conn)
	}
	if got.Secrets[0].Name != "STRIPE_KEY" || got.Secrets[0].Type != "generic" {
		t.Errorf("secret arm mangled: %+v", got.Secrets[0])
	}
}

func TestGrantsSummaryDecodesEntryKinds(t *testing.T) {
	body := `[{
		"id": "a1", "name": "Cody", "identifier": "cody", "isDefault": true,
		"createdAt": "2026-07-01T00:00:00.000Z",
		"grantsSummary": {
			"mode": "grants",
			"entries": [
				{"kind": "app", "provider": "gmail", "connectionId": "c1", "label": "Work"},
				{"kind": "secret", "id": "s1", "name": "STRIPE_KEY"},
				{"kind": "llm", "id": "s2", "name": "ANTHROPIC_KEY"}
			],
			"total": 3
		}
	}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := newWithPrefix(srv.URL, "oc_test", "")
	got, err := c.ListAgentsWithGrantsSummary(context.Background(), "")
	if err != nil {
		t.Fatalf("list with summary: %v", err)
	}
	a := got[0]
	if a.Name != "Cody" || !a.IsDefault {
		t.Errorf("embedded Agent mangled: %+v", a.Agent)
	}
	s := a.GrantsSummary
	if s.Total != 3 || len(s.Entries) != 3 {
		t.Fatalf("summary mangled: %+v", s)
	}
	if s.Entries[0].Kind != "app" || s.Entries[0].Provider != "gmail" || *s.Entries[0].Label != "Work" {
		t.Errorf("app arm mangled: %+v", s.Entries[0])
	}
	if s.Entries[1].Kind != "secret" || s.Entries[1].ID != "s1" {
		t.Errorf("secret arm mangled: %+v", s.Entries[1])
	}
	if s.Entries[2].Kind != "llm" || s.Entries[2].Name != "ANTHROPIC_KEY" {
		t.Errorf("llm arm mangled: %+v", s.Entries[2])
	}
}

func TestGrantsAreProjectScoped(t *testing.T) {
	// A slug resolves to an id via /v1/projects and rides X-Project-Id — the
	// header /v1 servers scope grants requests by.
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/projects" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"proj_123","slug":"payments","name":"Payments"}]`))
			return
		}
		gotHeader = r.Header.Get("X-Project-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agentId":"a1","mode":"grants","connections":[],"secrets":[]}`))
	}))
	defer srv.Close()

	c := newWithPrefix(srv.URL, "oc_test", "")
	if _, err := c.GetAgentGrants(context.Background(), "payments", "a1"); err != nil {
		t.Fatalf("get grants: %v", err)
	}
	if gotHeader != "proj_123" {
		t.Errorf("X-Project-Id = %q, want %q", gotHeader, "proj_123")
	}
}

func TestGrantErrorPassthrough(t *testing.T) {
	const msg = "A tool can't be both always-allowed and require approval."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"message":"` + msg + `","type":"validation_error"}}`))
	}))
	defer srv.Close()

	c := newWithPrefix(srv.URL, "oc_test", "")
	_, err := c.SetAgentConnectionGrant(context.Background(), "", "a1", "c1", ConnectionGrantInput{
		Access: "custom",
		Allow:  []string{"send_email"},
		Ask:    []string{"send_email"},
	})
	if err == nil {
		t.Fatal("expected the 422 to surface")
	}
	if !strings.Contains(err.Error(), msg) {
		t.Errorf("error %q does not carry the server message", err)
	}
}
