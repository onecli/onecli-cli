package agentapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/onecli/onecli-cli/internal/clientid"
)

// Every call names the client, including the plain GETs that skip do().
func TestEveryCallSendsOneCLIClient(t *testing.T) {
	clientid.SetVersion("3.0.0")
	t.Cleanup(func() { clientid.SetVersion("dev") })
	seen := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Path] = r.Header.Get(clientid.Header)
		switch r.URL.Path {
		case "/v1/gateway/ca":
			_, _ = w.Write([]byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"))
		case "/v2/skill":
			_, _ = w.Write([]byte("# skill\n"))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"agt_1","vault":{"id":"v","name":"n"},"gateway":{"address":"a:1","scheme":"https"}}`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL)
	ctx := context.Background()
	if _, err := c.CA(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Skill(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Me(ctx, "oc_agent_x"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/v1/gateway/ca", "/v2/skill", "/v2/agents/me"} {
		if seen[p] != "onecli-cli/3.0.0" {
			t.Errorf("%s: %s = %q", p, clientid.Header, seen[p])
		}
	}
}
