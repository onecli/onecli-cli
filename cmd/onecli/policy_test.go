package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/pkg/output"
)

// Command-layer helper units for the policy family: the --json XOR field-flag
// contract, the required trio, the legacy-action guidance, and JSON parsing.

func TestBuildCreatePolicyInputJSONPayload(t *testing.T) {
	input, err := buildCreatePolicyInput(
		`{"name":"n","action":"block","targets":[{"kind":"network","hostPattern":"a.com"}]}`,
		"", "", "", "", "", "", nil, nil, "", nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.Name != "n" || input.Action != "block" || len(input.Targets) != 1 {
		t.Errorf("input = %+v", input)
	}
}

func TestBuildCreatePolicyInputRejectsCombining(t *testing.T) {
	_, err := buildCreatePolicyInput(
		`{"name":"n","action":"block"}`,
		"also-a-name", "", "", "", "", "", nil, nil, "", nil,
	)
	if err == nil || !strings.Contains(err.Error(), "do not combine") {
		t.Errorf("want the combine rejection, got %v", err)
	}
}

func TestBuildCreatePolicyInputRequiredTrio(t *testing.T) {
	_, err := buildCreatePolicyInput(
		"", "n", "block", "", "", "", "", nil, nil, "", nil,
	)
	if err == nil || !strings.Contains(err.Error(), "--targets") {
		t.Errorf("want the required-trio error naming --targets, got %v", err)
	}
}

func TestBuildCreatePolicyInputLegacyActionGuidance(t *testing.T) {
	for _, action := range []string{"rate_limit", "manual_approval"} {
		_, err := buildCreatePolicyInput(
			"", "n", action, "", `[{"kind":"network","hostPattern":"a.com"}]`, "", "", nil, nil, "", nil,
		)
		if err == nil || !strings.Contains(err.Error(), "legacy action") {
			t.Errorf("action %q: want the legacy-action guidance, got %v", action, err)
		}
	}
}

func TestBuildCreatePolicyInputFieldFlags(t *testing.T) {
	limit := 10
	approval := true
	input, err := buildCreatePolicyInput(
		"", "n", "allow", "d",
		`[{"kind":"app","provider":"gmail","tools":["send_email"]}]`,
		`[{"type":"agent","id":"a1"}]`,
		`[{"target":"body","operator":"contains","value":"x"}]`,
		nil, &limit, "minute", &approval,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.Targets[0].Provider != "gmail" || input.Identities[0].ID != "a1" {
		t.Errorf("input = %+v", input)
	}
	if input.RateLimit == nil || *input.RateLimit != 10 || input.RateLimitWindow != "minute" {
		t.Errorf("rate fields = %+v %q", input.RateLimit, input.RateLimitWindow)
	}
	if input.Conditions == nil {
		t.Error("conditions must be set")
	}
}

func TestParsePolicyHelpersRejectBadJSON(t *testing.T) {
	if _, err := parsePolicyTargets(`{"not":"an array"}`); err == nil {
		t.Error("targets: want error for non-array")
	}
	if _, err := parsePolicyIdentities(`"nope"`); err == nil {
		t.Error("identities: want error for non-array")
	}
	if _, err := parsePolicyConditions(`{broken`); err == nil {
		t.Error("conditions: want error for invalid JSON")
	}
	if raw, err := parsePolicyConditions(`null`); err != nil || raw == nil || string(*raw) != "null" {
		t.Errorf("conditions 'null' must pass through as a raw null, got %v %v", raw, err)
	}
}

func TestUpdateInputRequiresSomething(t *testing.T) {
	cmd := PolicyRulesUpdateCmd{ID: "r1"}
	_, err := cmd.buildInput()
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Errorf("want the at-least-one error, got %v", err)
	}
}

func TestUpdateBuildInputRawJSONVerbatim(t *testing.T) {
	// --json must pass through as json.RawMessage: explicit nulls are the only
	// way to CLEAR nullable fields, and a typed round-trip would drop them.
	payload := `{"name":"x","rateLimit":null,"conditions":null}`
	cmd := PolicyRulesUpdateCmd{ID: "r1", Json: payload}
	v, err := cmd.buildInput()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, ok := v.(json.RawMessage)
	if !ok {
		t.Fatalf("buildInput --json must return json.RawMessage, got %T", v)
	}
	if string(raw) != payload {
		t.Errorf("payload = %s, want verbatim %s", raw, payload)
	}
}

func TestUpdateBuildInputRawJSONRejections(t *testing.T) {
	cases := []struct{ name, payload, want string }{
		{"non-object", `[1]`, "expected a JSON object"},
		{"empty-object", `{}`, "at least one field"},
		{"null", `null`, "at least one field"},
		{"legacy-action", `{"action":"manual_approval"}`, "legacy action"},
		{"invalid-action", `{"action":"warn"}`, "invalid action"},
		{"action-wrong-type", `{"action":5}`, "invalid --json payload"},
	}
	for _, tc := range cases {
		cmd := PolicyRulesUpdateCmd{ID: "r1", Json: tc.payload}
		if _, err := cmd.buildInput(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.name, err, tc.want)
		}
	}
}

// TestStagedPolicyDiffCountComposition pins the guard's 4-GET feed: draft +
// published rules, draft + published defaults — and that the composed count
// honors the diff lib's laws (derived rows excluded, default flip counted).
func TestStagedPolicyDiffCountComposition(t *testing.T) {
	custom := func(id, logical, name string, priority int, status string) string {
		return fmt.Sprintf(
			`{"id":%q,"logicalId":%q,"source":"custom","status":%q,"priority":%d,"enabled":true,"isDefault":false,"name":%q,"action":"allow","requireApproval":false,"description":null,"rateLimit":null,"rateLimitWindow":null,"identities":[],"targets":[{"kind":"network","hostPattern":"a.com"}],"createdAt":"2026-01-01"}`,
			id, logical, status, priority, name,
		)
	}
	blocklist := func(id, logical, status string, priority int) string {
		return fmt.Sprintf(
			`{"id":%q,"logicalId":%q,"source":"blocklist","status":%q,"priority":%d,"enabled":true,"isDefault":false,"name":"bl","action":"block","requireApproval":false,"description":null,"rateLimit":null,"rateLimitWindow":null,"identities":[],"targets":[],"createdAt":"2026-01-01"}`,
			id, logical, status, priority,
		)
	}
	defaultRule := func(id, action, status string) string {
		return fmt.Sprintf(
			`{"id":%q,"logicalId":"l-def","source":"default","status":%q,"priority":99,"enabled":true,"isDefault":true,"name":"Default Rule","action":%q,"requireApproval":false,"description":null,"rateLimit":null,"rateLimitWindow":null,"identities":[],"targets":[],"createdAt":"2026-01-01"}`,
			id, status, action,
		)
	}

	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health" {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		requests = append(requests, r.Method+" "+r.URL.Path+"?status="+r.URL.Query().Get("status"))
		switch {
		case r.URL.Path == "/v1/org/policy/rules" && r.URL.Query().Get("status") == "draft":
			// One kept custom, one ADDED custom, blocklist noise (fresh logicalId).
			_, _ = fmt.Fprintf(w, "[%s,%s,%s]",
				custom("d1", "l-keep", "keep", 0, "draft"),
				custom("d2", "l-new", "new", 1, "draft"),
				blocklist("d3", "bl-fresh", "draft", 2),
			)
		case r.URL.Path == "/v1/org/policy/rules" && r.URL.Query().Get("status") == "published":
			_, _ = fmt.Fprintf(w, "[%s,%s]",
				custom("p1", "l-keep", "keep", 0, "published"),
				blocklist("p2", "bl-old", "published", 1),
			)
		case r.URL.Path == "/v1/org/policy/default" && r.URL.Query().Get("status") == "draft":
			_, _ = fmt.Fprint(w, defaultRule("dd", "block", "draft"))
		case r.URL.Path == "/v1/org/policy/default" && r.URL.Query().Get("status") == "published":
			_, _ = fmt.Fprint(w, defaultRule("pd", "allow", "published"))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	count, err := stagedPolicyDiffCount(api.New(srv.URL, "oc_key"), "", true)
	if err != nil {
		t.Fatalf("stagedPolicyDiffCount: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (one added custom + the default flip; blocklist noise excluded)", count)
	}
	want := []string{
		"GET /v1/org/policy/rules?status=draft",
		"GET /v1/org/policy/rules?status=published",
		"GET /v1/org/policy/default?status=draft",
		"GET /v1/org/policy/default?status=published",
	}
	if len(requests) != len(want) {
		t.Fatalf("requests = %v, want %v", requests, want)
	}
	for i := range want {
		if requests[i] != want[i] {
			t.Errorf("request[%d] = %q, want %q", i, requests[i], want[i])
		}
	}
}

// ── The publish guard flow (pure, via injected ops) ─────────────────────────

func newTestWriter(t *testing.T) *output.Writer {
	t.Helper()
	return output.NewWithWriters(io.Discard, io.Discard)
}

func guardOps(pending int, publishErr error, published *int) policyScopeOps {
	return policyScopeOps{
		diffCount: func() (int, error) { return pending, nil },
		publish: func() (*api.PolicyPublishResult, error) {
			if publishErr != nil {
				return nil, publishErr
			}
			*published++
			return &api.PolicyPublishResult{Generation: 9, RuleCount: 3}, nil
		},
		statusCmd:   "onecli policy status",
		publishCmd:  "onecli policy publish",
		publishFlag: "--publish-all",
	}
}

func TestPublishPlanContradictionRejected(t *testing.T) {
	_, err := prePublishPending(
		policyPublishPlan{NoPublish: true, PublishAll: true},
		policyScopeOps{},
	)
	if err == nil || !strings.Contains(err.Error(), "contradictory") {
		t.Errorf("want the contradiction rejection, got %v", err)
	}
}

func TestGuardWithholdsPublishWhenDraftDirty(t *testing.T) {
	published := 0
	ops := guardOps(2, nil, &published)
	plan := policyPublishPlan{}
	pending, err := prePublishPending(plan, ops)
	if err != nil || pending != 2 {
		t.Fatalf("pending = %d, err = %v", pending, err)
	}
	out := newTestWriter(t)
	if err := finishPolicyWrite(out, policyWriteOutcome{DeletedID: "r1"}, plan, pending, ops); err != nil {
		t.Fatalf("withheld publish must not error: %v", err)
	}
	if published != 0 {
		t.Error("publish must be withheld when the draft has staged changes")
	}
}

func TestGuardPublishesCleanDraft(t *testing.T) {
	published := 0
	ops := guardOps(0, nil, &published)
	plan := policyPublishPlan{}
	pending, _ := prePublishPending(plan, ops)
	out := newTestWriter(t)
	if err := finishPolicyWrite(out, policyWriteOutcome{DeletedID: "r1"}, plan, pending, ops); err != nil {
		t.Fatalf("clean publish must not error: %v", err)
	}
	if published != 1 {
		t.Errorf("published = %d, want 1", published)
	}
}

func TestGuardPublishAllOverridesDirtyDraft(t *testing.T) {
	published := 0
	ops := guardOps(5, nil, &published)
	plan := policyPublishPlan{PublishAll: true}
	pending, _ := prePublishPending(plan, ops) // -1: check skipped
	out := newTestWriter(t)
	if err := finishPolicyWrite(out, policyWriteOutcome{DeletedID: "r1"}, plan, pending, ops); err != nil {
		t.Fatalf("publish-all must not error: %v", err)
	}
	if published != 1 {
		t.Errorf("published = %d, want 1", published)
	}
}

func TestGuardPublishFailureReturnsErrorAfterWrite(t *testing.T) {
	published := 0
	ops := guardOps(0, fmt.Errorf("plan gate rejected a staged rule"), &published)
	plan := policyPublishPlan{}
	pending, _ := prePublishPending(plan, ops)
	out := newTestWriter(t)
	err := finishPolicyWrite(out, policyWriteOutcome{DeletedID: "r1"}, plan, pending, ops)
	if err == nil || !strings.Contains(err.Error(), "the change IS staged") {
		t.Errorf("want the staged-but-unpublished error, got %v", err)
	}
}
