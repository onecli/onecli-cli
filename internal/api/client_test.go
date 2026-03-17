package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDoSetsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := New(srv.URL, "oc_testkey123")
	var result map[string]any
	_ = client.do(context.Background(), http.MethodGet, "/test", nil, &result)

	if gotAuth != "Bearer oc_testkey123" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer oc_testkey123")
	}
}

func TestDoOmitsAuthHeaderWhenNoKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := New(srv.URL, "")
	var result map[string]any
	_ = client.do(context.Background(), http.MethodGet, "/test", nil, &result)

	if gotAuth != "" {
		t.Errorf("Authorization should be empty when no key, got %q", gotAuth)
	}
}

func TestDoSendsJSONBody(t *testing.T) {
	var gotContentType string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := New(srv.URL, "")
	body := map[string]string{"name": "test"}
	var result map[string]any
	_ = client.do(context.Background(), http.MethodPost, "/test", body, &result)

	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["name"] != "test" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestDoNoContentTypeWithoutBody(t *testing.T) {
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := New(srv.URL, "")
	var result map[string]any
	_ = client.do(context.Background(), http.MethodGet, "/test", nil, &result)

	if gotContentType != "" {
		t.Errorf("Content-Type should be empty for GET, got %q", gotContentType)
	}
}

func TestDoHandles204(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := New(srv.URL, "")
	err := client.do(context.Background(), http.MethodDelete, "/test", nil, nil)
	if err != nil {
		t.Errorf("204 should not return error, got %v", err)
	}
}

func TestDoDecodesSuccessResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"abc","name":"Test Agent"}`))
	}))
	defer srv.Close()

	client := New(srv.URL, "")
	var agent Agent
	err := client.do(context.Background(), http.MethodGet, "/test", nil, &agent)
	if err != nil {
		t.Fatal(err)
	}
	if agent.ID != "abc" || agent.Name != "Test Agent" {
		t.Errorf("got %+v", agent)
	}
}

func TestDoReturnsAPIErrorWithMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"identifier already exists"}`))
	}))
	defer srv.Close()

	client := New(srv.URL, "")
	err := client.do(context.Background(), http.MethodPost, "/test", nil, nil)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Message != "identifier already exists" {
		t.Errorf("Message = %q", apiErr.Message)
	}
}

func TestDoReturnsAPIErrorFallbackMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	client := New(srv.URL, "")
	err := client.do(context.Background(), http.MethodGet, "/test", nil, nil)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
	if apiErr.Message != "Internal Server Error" {
		t.Errorf("Message = %q, want fallback status text", apiErr.Message)
	}
}

func TestDoReturnsAPIError401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
	}))
	defer srv.Close()

	client := New(srv.URL, "")
	err := client.do(context.Background(), http.MethodGet, "/test", nil, nil)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
}

func TestDoReturnsAPIError404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"agent not found"}`))
	}))
	defer srv.Close()

	client := New(srv.URL, "")
	err := client.do(context.Background(), http.MethodGet, "/test", nil, nil)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
	if apiErr.Message != "agent not found" {
		t.Errorf("Message = %q", apiErr.Message)
	}
}

func TestDoSendsCorrectMethod(t *testing.T) {
	methods := []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			var gotMethod string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				if method == http.MethodDelete {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()

			client := New(srv.URL, "")
			_ = client.do(context.Background(), method, "/test", nil, nil)

			if gotMethod != method {
				t.Errorf("method = %q, want %q", gotMethod, method)
			}
		})
	}
}

func TestDoSendsCorrectPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := New(srv.URL, "")
	_ = client.do(context.Background(), http.MethodGet, "/api/agents", nil, nil)

	if gotPath != "/api/agents" {
		t.Errorf("path = %q, want /api/agents", gotPath)
	}
}
