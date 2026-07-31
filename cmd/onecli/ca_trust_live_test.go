package main

import (
	"os"
	"strings"
	"testing"

	"github.com/onecli/onecli-cli/pkg/output"
)

// Live check against the REAL login keychain and the REAL gateway CA on disk.
// This is the end-to-end version of the unit tests: it proves the warning fires
// (or stays silent) for the machine's actual trust state, which is the thing
// that broke Cursor. Skipped unless ONECLI_LIVE_CA_CHECK=1 so CI stays hermetic.
func TestLiveGatewayCATrustState(t *testing.T) {
	if os.Getenv("ONECLI_LIVE_CA_CHECK") != "1" {
		t.Skip("set ONECLI_LIVE_CA_CHECK=1 to check this machine's real keychain")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	pemBytes, err := os.ReadFile(home + "/.onecli/gateway-ca.pem")
	if err != nil {
		t.Skipf("no gateway CA on disk yet: %v", err)
	}

	var buf strings.Builder
	w := output.NewWithWriters(&buf, &buf)
	warnIfGatewayCANotTrusted(w, string(pemBytes))

	got := buf.String()
	t.Logf("warning output: %q", got)
	if got == "" {
		t.Log("RESULT: the live gateway CA is trusted; GUI editors will work")
	} else {
		t.Log("RESULT: warning fired, GUI editors would fail until the CA is re-trusted")
	}
}
