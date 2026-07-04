package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConnectOrgApp(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody ConnectAppInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_org_key", "/v1")
	input := ConnectAppInput{
		Fields:       ConfigFields{"apiKey": "sk-1"},
		ConnectionID: "conn-9",
		Label:        "shared",
		Method:       "api_key",
	}
	if err := client.ConnectOrgApp(context.Background(), "fireflies", input); err != nil {
		t.Fatalf("ConnectOrgApp: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/v1/org/apps/fireflies/connect" {
		t.Errorf("request = %s %s, want POST /v1/org/apps/fireflies/connect", gotMethod, gotPath)
	}
	if gotAuth != "Bearer oc_org_key" {
		t.Errorf("auth = %q, want Bearer oc_org_key", gotAuth)
	}
	if gotBody.Fields["apiKey"] != "sk-1" || gotBody.Label != "shared" ||
		gotBody.ConnectionID != "conn-9" || gotBody.Method != "api_key" {
		t.Errorf("body = %+v, want all contract fields marshaled", gotBody)
	}
}

func TestConnectOrgAppError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"API Key is required"}`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_org_key", "/v1")
	err := client.ConnectOrgApp(context.Background(), "fireflies", ConnectAppInput{
		Fields: ConfigFields{"apiKey": ""},
	})
	if err == nil {
		t.Fatal("ConnectOrgApp: expected error, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "API Key is required" {
		t.Errorf("error = %v, want APIError with server message", err)
	}
}

func TestOrgAppAuthorizeURLCapturesRedirect(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("connectionId")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Location", "https://provider.example/oauth?state=abc")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_org_key", "/v1")
	url, err := client.OrgAppAuthorizeURL(context.Background(), "github", "conn-1")
	if err != nil {
		t.Fatalf("OrgAppAuthorizeURL: %v", err)
	}

	if url != "https://provider.example/oauth?state=abc" {
		t.Errorf("url = %q, want the raw Location target", url)
	}
	if gotPath != "/v1/org/apps/github/authorize" || gotQuery != "conn-1" {
		t.Errorf("request = %s?connectionId=%s, want /v1/org/apps/github/authorize?connectionId=conn-1", gotPath, gotQuery)
	}
	if gotAuth != "Bearer oc_org_key" {
		t.Errorf("auth = %q, want Bearer oc_org_key", gotAuth)
	}
}

func TestOrgAppAuthorizeURLServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"OAuth App is not configured. Missing required credentials."}`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_org_key", "/v1")
	_, err := client.OrgAppAuthorizeURL(context.Background(), "github", "")
	if err == nil {
		t.Fatal("OrgAppAuthorizeURL: expected error, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("error = %v, want 400 APIError", err)
	}
}

func TestOrgAppAuthorizeURLNonRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_org_key", "/v1")
	_, err := client.OrgAppAuthorizeURL(context.Background(), "github", "")
	if err == nil {
		t.Fatal("OrgAppAuthorizeURL: expected error for non-redirect response")
	}
}

func TestOrgAppAuthorizeURLOmitsEmptyConnectionID(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Location", "https://provider.example/oauth")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_org_key", "/v1")
	if _, err := client.OrgAppAuthorizeURL(context.Background(), "github", ""); err != nil {
		t.Fatalf("OrgAppAuthorizeURL: %v", err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty when no connection id is given", gotQuery)
	}
}

func TestOrgAppAuthorizeURLLegacyPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Location", "https://provider.example/oauth")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_org_key", "/api")
	url, err := client.OrgAppAuthorizeURL(context.Background(), "github", "")
	if err != nil {
		t.Fatalf("OrgAppAuthorizeURL: %v", err)
	}
	if gotPath != "/api/org/apps/github/authorize" {
		t.Errorf("path = %q, want the /api-prefixed rewrite", gotPath)
	}
	if url != "https://provider.example/oauth" {
		t.Errorf("url = %q, want the Location target", url)
	}
}
