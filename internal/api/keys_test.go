package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListKeys(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantIDs   []string
		wantErr   error
		wantCalls int
	}{
		{
			name: "one page",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"data":[{"id":"k1","name":"a","permissions":["manage"],"createdVia":"dashboard","createdAt":"2026-11-27T00:00:00.000Z","expiresAt":null,"lastUsedAt":null}],"nextCursor":null}`))
			},
			wantIDs:   []string{"k1"},
			wantCalls: 1,
		},
		{
			name: "follows the cursor",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("cursor") == "c+1" {
					_, _ = w.Write([]byte(`{"data":[{"id":"k2"}],"nextCursor":null}`))
					return
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"k1"}],"nextCursor":"c+1"}`))
			},
			wantIDs:   []string{"k1", "k2"},
			wantCalls: 2,
		},
		{
			name: "404 means no /v2/keys",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"message":"Not found"}}`))
			},
			wantErr:   ErrKeysUnsupported,
			wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/v2/keys" || r.Method != http.MethodGet {
					t.Errorf("got %s %s", r.Method, r.URL.Path)
				}
				tt.handler(w, r)
			}))
			defer srv.Close()
			keys, err := newWithPrefix(srv.URL, "oc_k", "/v1").ListKeys(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			var ids []string
			for _, k := range keys {
				ids = append(ids, k.ID)
			}
			if strings.Join(ids, ",") != strings.Join(tt.wantIDs, ",") {
				t.Errorf("ids = %v, want %v", ids, tt.wantIDs)
			}
			if calls != tt.wantCalls {
				t.Errorf("calls = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestListKeysStopsOnEndlessCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[],"nextCursor":"again"}`))
	}))
	defer srv.Close()
	_, err := newWithPrefix(srv.URL, "oc_k", "/v1").ListKeys(context.Background())
	if err == nil || !strings.Contains(err.Error(), "pages") {
		t.Fatalf("want the page bound error, got %v", err)
	}
}

func TestListKeysOtherErrorIsNotUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated","message":"Sign in","details":[]}}`))
	}))
	defer srv.Close()
	_, err := newWithPrefix(srv.URL, "oc_k", "/v1").ListKeys(context.Background())
	if errors.Is(err, ErrKeysUnsupported) || !IsStatus(err, 401) {
		t.Fatalf("want a 401 APIError, got %v", err)
	}
}

func TestRevokeKey(t *testing.T) {
	var path, method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.EscapedPath(), r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	if err := newWithPrefix(srv.URL, "oc_k", "/v1").RevokeKey(context.Background(), "k/1"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodDelete || path != "/v2/keys/k%2F1" {
		t.Errorf("got %s %s", method, path)
	}
}

func TestV2PathKeepsItsPrefixOnLegacyServers(t *testing.T) {
	c := newWithPrefix("http://x", "", "/api")
	if got := c.applyPrefix("/v2/keys"); got != "/v2/keys" {
		t.Errorf("applyPrefix(/v2/keys) = %q", got)
	}
}
