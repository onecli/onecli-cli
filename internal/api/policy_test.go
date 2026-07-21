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

// Round-trips for the /v1/policy client: payload encoding for every target
// kind, scope/status routing, the reorder body, and the error-shape mapping
// external callers depend on (410 legacy / 422 validation / 403 flag-off /
// the route-miss 404 upgrade).

func TestCreatePolicyRuleEncodesAllTargetKinds(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/policy/rules" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decoding body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"r1","logicalId":"l1","name":"n","action":"block"}`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_key", "/v1")
	enabled := true
	rule, err := client.CreatePolicyRule(context.Background(), "", CreatePolicyRuleInput{
		Name:    "n",
		Action:  "block",
		Enabled: &enabled,
		Identities: []PolicyIdentity{
			{Type: "agent", ID: "a1"},
		},
		Targets: []PolicyTarget{
			{Kind: "app", Provider: "gmail", Tools: []string{"send_email"}},
			{Kind: "connection", ConnectionID: "c1", Tools: []string{"git_push"}},
			{Kind: "secret", SecretScope: "project"},
			{Kind: "network", HostPattern: "api.example.com", PathPattern: "/v1/*", Method: "POST"},
		},
	})
	if err != nil {
		t.Fatalf("CreatePolicyRule: %v", err)
	}
	if rule.ID != "r1" {
		t.Errorf("rule.ID = %q, want r1", rule.ID)
	}

	targets, ok := captured["targets"].([]any)
	if !ok || len(targets) != 4 {
		t.Fatalf("targets = %#v, want 4 entries", captured["targets"])
	}
	conn := targets[1].(map[string]any)
	if conn["connectionId"] != "c1" {
		t.Errorf("connection target field = %#v, want connectionId=c1 (the contract field name)", conn)
	}
	if _, present := conn["secretId"]; present {
		t.Errorf("connection target must omit unrelated fields, got %#v", conn)
	}
	secret := targets[2].(map[string]any)
	if secret["secretScope"] != "project" {
		t.Errorf("secret target = %#v, want secretScope=project", secret)
	}
	if _, present := captured["rateLimit"]; present {
		t.Errorf("absent rateLimit must be omitted, got %#v", captured["rateLimit"])
	}
}

func TestListPolicyRulesStatusQueryAndOrgPath(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.String())
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_key", "/v1")
	if _, err := client.ListPolicyRules(context.Background(), "", "published"); err != nil {
		t.Fatalf("ListPolicyRules: %v", err)
	}
	if _, err := client.ListOrgPolicyRules(context.Background(), "draft"); err != nil {
		t.Fatalf("ListOrgPolicyRules: %v", err)
	}
	if paths[0] != "/v1/policy/rules?status=published" {
		t.Errorf("project path = %q", paths[0])
	}
	if paths[1] != "/v1/org/policy/rules?status=draft" {
		t.Errorf("org path = %q", paths[1])
	}
}

func TestReorderPolicyRulesBody(t *testing.T) {
	var captured struct {
		OrderedIDs []string `json:"orderedIds"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/policy/rules/order" || r.Method != http.MethodPut {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_key", "/v1")
	if _, err := client.ReorderPolicyRules(context.Background(), "", []string{"a", "b"}); err != nil {
		t.Fatalf("ReorderPolicyRules: %v", err)
	}
	if len(captured.OrderedIDs) != 2 || captured.OrderedIDs[0] != "a" {
		t.Errorf("orderedIds = %#v", captured.OrderedIDs)
	}
}

func TestPolicyErrorShapesPassThrough(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantStatus int
		wantSub    string
	}{
		{
			name:       "410 legacy-gone carries the server message",
			status:     http.StatusGone,
			body:       `{"error":{"message":"managed through the policy API","type":"invalid_request_error"}}`,
			wantStatus: 410,
			wantSub:    "policy API",
		},
		{
			name:       "422 validation carries the Zod issue",
			status:     http.StatusUnprocessableEntity,
			body:       `{"error":{"message":"A rule must name at least one target.","type":"validation_error"}}`,
			wantStatus: 422,
			wantSub:    "at least one target",
		},
		{
			name:       "403 editing-off carries the flag message",
			status:     http.StatusForbidden,
			body:       `{"error":{"message":"Policy editing is not enabled for this deployment yet.","type":"authentication_error"}}`,
			wantStatus: 403,
			wantSub:    "not enabled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			client := newWithPrefix(srv.URL, "oc_key", "/v1")
			_, err := client.CreatePolicyRule(context.Background(), "", CreatePolicyRuleInput{Name: "n", Action: "block"})
			apiErr, ok := err.(*APIError)
			if !ok {
				t.Fatalf("want *APIError, got %T: %v", err, err)
			}
			if apiErr.StatusCode != tt.wantStatus {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tt.wantStatus)
			}
			if !strings.Contains(apiErr.Message, tt.wantSub) {
				t.Errorf("Message = %q, want substring %q", apiErr.Message, tt.wantSub)
			}
		})
	}
}

func TestPolicyRouteMiss404IsUpgraded(t *testing.T) {
	// A 404 WITHOUT type "not_found_error" is a route miss (old server, or
	// community edition for the org surface) — upgraded to guidance.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Unrecognized request URL (GET: /v1/org/policy/rules).","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_key", "/v1")
	_, err := client.ListOrgPolicyRules(context.Background(), "")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "does not support the org policy API") {
		t.Errorf("err = %q, want the org guidance", err.Error())
	}

	// A RESOURCE 404 (type not_found_error) passes through untouched.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Rule not found","type":"not_found_error"}}`))
	}))
	defer srv2.Close()
	client2 := newWithPrefix(srv2.URL, "oc_key", "/v1")
	_, err2 := client2.GetPolicyRule(context.Background(), "", "missing")
	apiErr, ok := err2.(*APIError)
	if !ok || apiErr.StatusCode != 404 || apiErr.Message != "Rule not found" {
		t.Errorf("resource 404 must pass through, got %T %v", err2, err2)
	}
}

func TestGetPolicyLastPublishNull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`null`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_key", "/v1")
	last, err := client.GetPolicyLastPublish(context.Background(), "")
	if err != nil {
		t.Fatalf("GetPolicyLastPublish: %v", err)
	}
	if last != nil {
		t.Errorf("never-published must yield nil, got %#v", last)
	}
}

func TestUpdatePolicyRuleSendsRawBodyVerbatim(t *testing.T) {
	// The --json path passes json.RawMessage: explicit nulls (the only way to
	// CLEAR rateLimit/conditions server-side) must reach the wire — a typed
	// pointer round-trip would drop them as omitempty absences.
	var rawBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/policy/rules/r1" || r.Method != http.MethodPatch {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		rawBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":"r1","logicalId":"l1","name":"x","action":"allow"}`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_key", "/v1")
	payload := `{"conditions":null,"name":"x","rateLimit":null}`
	if _, err := client.UpdatePolicyRule(context.Background(), "", "r1", json.RawMessage(payload)); err != nil {
		t.Fatalf("UpdatePolicyRule: %v", err)
	}
	if string(rawBody) != payload {
		t.Errorf("body = %s, want the verbatim payload (explicit nulls must survive)", rawBody)
	}
}
