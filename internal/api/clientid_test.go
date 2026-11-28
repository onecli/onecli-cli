package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/onecli/onecli-cli/internal/clientid"
)

// The header rides on both transports buildHTTPClient can pick, and on the
// prefix probe as well as the call itself.
func TestEveryCallSendsOneCLIClient(t *testing.T) {
	clientid.SetVersion("3.0.0")
	t.Cleanup(func() { clientid.SetVersion("dev") })
	for _, name := range []string{"default transport", "SSL_CERT_FILE transport"} {
		t.Run(name, func(t *testing.T) {
			if name == "SSL_CERT_FILE transport" {
				f := filepath.Join(t.TempDir(), "ca.pem")
				if err := os.WriteFile(f, []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("SSL_CERT_FILE", f)
			} else {
				t.Setenv("SSL_CERT_FILE", "")
			}
			seen := map[string]string{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen[r.URL.Path] = r.Header.Get(clientid.Header)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[],"nextCursor":null}`))
			}))
			defer srv.Close()
			c := New(srv.URL, "oc_key_x")
			if _, err := c.ListKeys(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(seen) == 0 {
				t.Fatal("no call reached the server")
			}
			for p, v := range seen {
				if v != "onecli-cli/3.0.0" {
					t.Errorf("%s: %s = %q", p, clientid.Header, v)
				}
			}
		})
	}
}
