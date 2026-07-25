package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The reflections replace the retired equipment/permission reads. The property
// that matters most is the PATH: the old ones are 410 now, so a wrong path here
// fails at runtime rather than at compile time.

func newTestClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, apiKey: "oc_test", httpClient: http.DefaultClient}
}

func TestReflectionsCallTheCanonicalPaths(t *testing.T) {
	tests := []struct {
		name      string
		wantPath  string
		wantQuery string
		// The catalog endpoint returns an array; the rest return an object.
		body string
		call func(*Client) error
	}{
		{
			name:      "project effective app permissions",
			wantPath:  "/v1/policy/effective-app-permissions",
			wantQuery: "provider=gmail",
			call: func(c *Client) error {
				_, err := c.GetEffectiveAppPermissions(context.Background(), "", "gmail", "")
				return err
			},
		},
		{
			name:      "narrowed to an agent",
			wantPath:  "/v1/policy/effective-app-permissions",
			wantQuery: "agentId=a1&provider=gmail",
			call: func(c *Client) error {
				_, err := c.GetEffectiveAppPermissions(context.Background(), "", "gmail", "a1")
				return err
			},
		},
		{
			name:      "org effective app permissions",
			wantPath:  "/v1/org/policy/effective-app-permissions",
			wantQuery: "provider=github",
			call: func(c *Client) error {
				_, err := c.GetOrgEffectiveAppPermissions(context.Background(), "github")
				return err
			},
		},
		{
			name:     "agent effective credentials",
			wantPath: "/v1/agents/a1/effective-credentials",
			call: func(c *Client) error {
				_, err := c.GetEffectiveCredentials(context.Background(), "", "a1")
				return err
			},
		},
		{
			name:     "connection agent access",
			wantPath: "/v1/connections/c1/effective-agents",
			call: func(c *Client) error {
				_, err := c.GetConnectionAgentAccess(context.Background(), "", "c1")
				return err
			},
		},
		{
			name:     "app permission definitions",
			wantPath: "/v1/apps/permission-definitions",
			body:     "[]",
			call: func(c *Client) error {
				_, err := c.ListAppPermissionDefinitions(context.Background())
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath, gotQuery, gotMethod string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotQuery, gotMethod = r.URL.Path, r.URL.Query().Encode(), r.Method
				w.Header().Set("Content-Type", "application/json")
				body := tt.body
				if body == "" {
					body = "{}"
				}
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			if err := tt.call(newTestClient(srv.URL)); err != nil {
				t.Fatalf("call: %v", err)
			}
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
			if tt.wantQuery != "" && gotQuery != tt.wantQuery {
				t.Errorf("query = %q, want %q", gotQuery, tt.wantQuery)
			}
			if gotMethod != http.MethodGet {
				t.Errorf("method = %q, want GET (reflections never mutate)", gotMethod)
			}
		})
	}
}

func TestEffectiveCredentialsDecodesBothArms(t *testing.T) {
	// The response is a discriminated union — a secret carries name/host, a
	// connection carries label/provider. Decoding one as the other loses fields
	// silently, so both arms are asserted.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"agentId":"a1","mode":"selective",
			"secrets":[{"kind":"secret","id":"s1","name":"Anthropic key","host":"api.anthropic.com",
				"status":"usable","provenance":[{"kind":"rule","scope":"project",
				"rule":{"logicalId":"l1","name":"Anthropic key"}}]}],
			"connections":[{"kind":"connection","id":"c1","label":"Acme repos","provider":"github",
				"status":"limited","provenance":[{"kind":"rule","scope":"organization","redacted":true}]}]
		}`))
	}))
	defer srv.Close()

	got, err := newTestClient(srv.URL).GetEffectiveCredentials(context.Background(), "", "a1")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if got.Mode != "selective" {
		t.Errorf("mode = %q, want selective", got.Mode)
	}
	if len(got.Secrets) != 1 || got.Secrets[0].Host != "api.anthropic.com" {
		t.Errorf("secret arm lost its host: %+v", got.Secrets)
	}
	if got.Secrets[0].Provenance[0].Rule == nil || got.Secrets[0].Provenance[0].Rule.Name != "Anthropic key" {
		t.Errorf("named provenance lost: %+v", got.Secrets[0].Provenance)
	}
	if len(got.Connections) != 1 || got.Connections[0].Provider != "github" {
		t.Errorf("connection arm lost its provider: %+v", got.Connections)
	}
	if !got.Connections[0].Provenance[0].Redacted {
		t.Error("redacted org provenance must survive decoding — it is a visibility signal")
	}
}

func TestReflectionSurfacesAServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "Agent not found", "type": "not_found_error"},
		})
	}))
	defer srv.Close()

	if _, err := newTestClient(srv.URL).GetEffectiveCredentials(context.Background(), "", "nope"); err == nil {
		t.Fatal("expected an error for a 404")
	}
}
