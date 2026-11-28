package agentapi

import "testing"

func TestPlainHTTPWarning(t *testing.T) {
	cases := map[string]bool{
		"https://api.onecli.sh":        false,
		"http://localhost:10254":       false,
		"http://127.0.0.1:10254":       false,
		"http://[::1]:10254":           false,
		"http://api.example.com":       true,
		"http://10.0.0.5:10254":        true,
		"http://localhost.example.com": true,
	}
	for base, want := range cases {
		if got := PlainHTTPWarning(base) != ""; got != want {
			t.Errorf("%s: warned %v, want %v", base, got, want)
		}
	}
}
