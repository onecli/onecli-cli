package clientid

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetVersion(t *testing.T) {
	t.Cleanup(func() { SetVersion("dev") })
	cases := map[string]string{
		"3.0.0":             "onecli-cli/3.0.0",
		"v3.0.0-rc.1+abc12": "onecli-cli/v3.0.0-rc.1+abc12",
		"":                  "onecli-cli/unknown",
		"3.0\r\nX-Evil: 1":  "onecli-cli/unknown",
		"3 0":               "onecli-cli/unknown",
	}
	for in, want := range cases {
		SetVersion(in)
		if got := Value(); got != want {
			t.Errorf("SetVersion(%q): Value() = %q, want %q", in, got, want)
		}
	}
}

func TestTransportSetsTheHeaderAndLeavesTheRequestAlone(t *testing.T) {
	SetVersion("3.0.0")
	t.Cleanup(func() { SetVersion("dev") })
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(Header)
	}))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := (&http.Client{Transport: Wrap(nil)}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got != "onecli-cli/3.0.0" {
		t.Fatalf("%s = %q", Header, got)
	}
	if req.Header.Get(Header) != "" {
		t.Fatal("the caller's request was changed")
	}
}
