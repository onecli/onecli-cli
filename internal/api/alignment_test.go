package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseAPIError(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantMsg  string
		wantType string
	}{
		{
			name:     "envelope shape",
			status:   401,
			body:     `{"error":{"message":"X-Project-Id header is required","type":"authentication_error"}}`,
			wantMsg:  "X-Project-Id header is required",
			wantType: "authentication_error",
		},
		{
			name:    "flat shape",
			status:  400,
			body:    `{"error":"Label is required"}`,
			wantMsg: "Label is required",
		},
		{
			name:    "unparseable body falls back to status text",
			status:  502,
			body:    `<html>bad gateway</html>`,
			wantMsg: "Bad Gateway",
		},
		{
			name:    "empty error falls back to status text",
			status:  404,
			body:    `{"error":""}`,
			wantMsg: "Not Found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAPIError(tt.status, []byte(tt.body))
			if got.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantMsg)
			}
			if got.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tt.wantType)
			}
			if got.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", got.StatusCode, tt.status)
			}
		})
	}
}

func TestDoProjectResolvesSlugToHeader(t *testing.T) {
	var gotHeader, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/projects":
			_, _ = w.Write([]byte(`[{"id":"cproj123","name":"My Project","slug":"my-project","createdAt":"2026-01-01"}]`))
		case "/v1/agents":
			gotHeader = r.Header.Get("X-Project-Id")
			gotQuery = r.URL.Query().Get("projectId")
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"Not found"}`))
		}
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_key", "/v1")
	if _, err := client.ListAgents(context.Background(), "my-project"); err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if gotHeader != "cproj123" {
		t.Errorf("X-Project-Id = %q, want resolved id %q", gotHeader, "cproj123")
	}
	if gotQuery != "my-project" {
		t.Errorf("projectId query = %q, want original value %q (legacy compat)", gotQuery, "my-project")
	}
}

func TestDoProjectFallsBackToRawValueWhenProjectsUnavailable(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/projects":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"Not found"}`))
		case "/v1/agents":
			gotHeader = r.Header.Get("X-Project-Id")
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_key", "/v1")
	if _, err := client.ListAgents(context.Background(), "cprojraw"); err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if gotHeader != "cprojraw" {
		t.Errorf("X-Project-Id = %q, want pass-through %q", gotHeader, "cprojraw")
	}
}

func TestListConnectionsNewServer(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/connections" {
			gotPath = r.URL.Path
			gotQuery = r.URL.Query().Get("provider")
			_, _ = w.Write([]byte(`[{"id":"c1","provider":"gmail","status":"connected"}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Not found"}`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_key", "/v1")
	conns, err := client.ListConnections(context.Background(), "gmail")
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	if len(conns) != 1 || conns[0].ID != "c1" {
		t.Errorf("connections = %+v, want one row c1", conns)
	}
	if gotPath != "/v1/connections" || gotQuery != "gmail" {
		t.Errorf("request = %s?provider=%s, want /v1/connections?provider=gmail", gotPath, gotQuery)
	}
}

func TestListConnectionsLegacyFallback(t *testing.T) {
	var gotLegacyPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/connections":
			// Pre-promotion server: route doesn't exist.
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"Not found","type":"not_found_error"}}`))
		case "/v1/apps/connections/gmail":
			gotLegacyPath = r.URL.Path
			_, _ = w.Write([]byte(`{"connections":[{"id":"c1","provider":"gmail","status":"connected"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_key", "/v1")
	conns, err := client.ListConnections(context.Background(), "gmail")
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	if len(conns) != 1 || conns[0].ID != "c1" {
		t.Errorf("connections = %+v, want one row c1 from legacy envelope", conns)
	}
	if gotLegacyPath != "/v1/apps/connections/gmail" {
		t.Errorf("legacy path = %q, want /v1/apps/connections/gmail", gotLegacyPath)
	}
}

func TestGetAppPermissionsDecodesLayeredShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/org/rules/permissions/gmail" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{
			"defaults": {"send_message": {"permission": "block", "conditions": []}},
			"byAgent": {"agt1": {"get_message": {"permission": "allow", "conditions": []}}}
		}`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_key", "/v1")
	states, err := client.GetAppPermissions(context.Background(), "gmail")
	if err != nil {
		t.Fatalf("GetAppPermissions: %v", err)
	}
	if got := states.Defaults["send_message"].Permission; got != "block" {
		t.Errorf("Defaults[send_message] = %q, want block", got)
	}
	if got := states.ByAgent["agt1"]["get_message"].Permission; got != "allow" {
		t.Errorf("ByAgent[agt1][get_message] = %q, want allow", got)
	}
}

func TestConfigureAppSendsArbitraryFields(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_key", "/v1")
	fields := ConfigFields{"appId": "123", "appSlug": "my-app", "privateKey": "pem"}
	if err := client.ConfigureApp(context.Background(), "github-app", fields); err != nil {
		t.Fatalf("ConfigureApp: %v", err)
	}
	if gotBody["appId"] != "123" || gotBody["appSlug"] != "my-app" || gotBody["privateKey"] != "pem" {
		t.Errorf("body = %v, want all three github-app fields passed through", gotBody)
	}
}

func TestUpdateRuleRefetchesAfterSuccess(t *testing.T) {
	var patched bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/rules/r1":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, ok := body["conditions"]; !ok {
				t.Errorf("PATCH body missing conditions: %v", body)
			}
			patched = true
			_, _ = w.Write([]byte(`{"success":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/rules/r1":
			_, _ = w.Write([]byte(`{"id":"r1","name":"Updated","action":"manual_approval","enabled":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_key", "/v1")
	conditions := []RuleCondition{{Target: "body", Operator: "contains", Value: "x"}}
	rule, err := client.UpdateRule(context.Background(), "r1", UpdateRuleInput{Conditions: &conditions})
	if err != nil {
		t.Fatalf("UpdateRule: %v", err)
	}
	if !patched {
		t.Fatal("PATCH was never sent")
	}
	if rule.Name != "Updated" || rule.Action != "manual_approval" {
		t.Errorf("rule = %+v, want the re-fetched rule", rule)
	}
}
