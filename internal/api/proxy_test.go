package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestIsGatewayProxy(t *testing.T) {
	cases := map[string]bool{
		"http://x:oc_agent_abc_DEF@gw.example.com:10255": true,
		"https://x:oc_agent_abc@gw.example.com:443":      true,
		"http://aoc_old:@gw.example.com:10255":           true,
		"http://x:aoc_old@gw.example.com:10255":          true,
		"http://corp-proxy:3128":                         false,
		"http://user:pass@corp-proxy:3128":               false,
		"http://oc_key_x:@corp-proxy:3128":               false,
	}
	for raw, want := range cases {
		u, _ := url.Parse(raw)
		if got := isGatewayProxy(u); got != want {
			t.Errorf("isGatewayProxy(%s) = %v, want %v", raw, got, want)
		}
	}
}

// The API key never rides through the gateway, on either transport; a
// plain proxy from the environment is still used (the control).
func TestAPIClientSkipsTheGatewayProxyOnly(t *testing.T) {
	for _, withCA := range []bool{false, true} {
		for _, gateway := range []bool{true, false} {
			name := map[bool]string{true: "gateway", false: "plain"}[gateway]
			if withCA {
				name += "_with_SSL_CERT_FILE"
			}
			t.Run(name, func(t *testing.T) {
				var keySeen string
				var direct, proxied int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// A proxied request carries the absolute URL.
					if r.URL.IsAbs() {
						atomic.AddInt32(&proxied, 1)
					} else {
						atomic.AddInt32(&direct, 1)
					}
					keySeen = r.Header.Get("Authorization")
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"data":[],"nextCursor":null}`))
				}))
				defer srv.Close()
				// The environment names srv as the proxy, with or without an
				// agent credential in it.
				pu, _ := url.Parse(srv.URL)
				if gateway {
					pu.User = url.UserPassword("x", "oc_agent_abc_DEF123")
				}
				orig := envProxy
				envProxy = func(*http.Request) (*url.URL, error) { return pu, nil }
				t.Cleanup(func() { envProxy = orig })
				t.Setenv("SSL_CERT_FILE", "")
				if withCA {
					f := filepath.Join(t.TempDir(), "ca.pem")
					_ = os.WriteFile(f, []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"), 0o600)
					t.Setenv("SSL_CERT_FILE", f)
				}
				c := newWithPrefix(srv.URL, "oc_key_secret", "/v1")
				if _, err := c.ListKeys(context.Background()); err != nil {
					t.Fatal(err)
				}
				if keySeen != "Bearer oc_key_secret" {
					t.Fatalf("key seen = %q", keySeen)
				}
				if gateway && (proxied != 0 || direct != 1) {
					t.Fatalf("the API key went through the gateway proxy (proxied=%d direct=%d)", proxied, direct)
				}
				if !gateway && (proxied != 1 || direct != 0) {
					t.Fatalf("a plain proxy from the environment was dropped (proxied=%d direct=%d)", proxied, direct)
				}
			})
		}
	}
}

// A proxy at the agent.env gateway address is skipped even when its user
// info has no credential shape the CLI knows; any other address is kept.
func TestAPIProxySkipsTheGatewayByAddress(t *testing.T) {
	origEnv, origAddr := envProxy, gatewayAddress
	t.Cleanup(func() { envProxy, gatewayAddress = origEnv, origAddr })
	gatewayAddress = func() string { return "gw.example.com:443" }
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/v2/keys", nil)
	cases := map[string]bool{ // proxy URL -> want skipped
		"https://x:newformat_abc@gw.example.com:443": true,
		"https://gw.example.com:443":                 true,
		"http://corp-proxy:3128":                     false,
		"https://gw.example.com:8443":                false,
	}
	for raw, skip := range cases {
		pu, _ := url.Parse(raw)
		envProxy = func(*http.Request) (*url.URL, error) { return pu, nil }
		got, err := apiProxy(req)
		if err != nil {
			t.Fatal(err)
		}
		if skip != (got == nil) {
			t.Errorf("%s: proxy = %v, want skipped=%v", raw, got, skip)
		}
	}
	// No agent.env: nothing is skipped by address.
	gatewayAddress = func() string { return "" }
	pu, _ := url.Parse("http://corp-proxy:3128")
	envProxy = func(*http.Request) (*url.URL, error) { return pu, nil }
	if got, _ := apiProxy(req); got == nil {
		t.Error("with no agent.env, a plain proxy was dropped")
	}
}

// gatewayAddress reads the address from agent.env in ONECLI_HOME.
func TestGatewayAddressReadsAgentEnv(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "onecli")
	t.Setenv("ONECLI_HOME", dir)
	if got := gatewayAddress(); got != "" {
		t.Fatalf("no agent.env: got %q", got)
	}
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "agent.env"),
		[]byte("export HTTPS_PROXY='https://x:oc_agent_abc@gw.example.com:443'\n"), 0o600)
	if got := gatewayAddress(); got != "gw.example.com:443" {
		t.Fatalf("got %q", got)
	}
}
