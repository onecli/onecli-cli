package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain keeps every test away from the real browser: a test that
// reaches the opener gets an error, as on a machine with no display.
func TestMain(m *testing.M) {
	openBrowser = func(string) error { return errors.New("tests never open a browser") }
	os.Exit(m.Run())
}

// No probe may hand a URL to open or xdg-open: if the deny were missing,
// the run would launch a real browser on the machine running the tests.
func TestNoProbeOpensAURL(t *testing.T) {
	for _, p := range sandboxProbesFor(1, "http://127.0.0.1:1/") {
		if len(p.argv) == 0 {
			continue
		}
		base := filepath.Base(p.argv[0])
		if base != "open" && base != "xdg-open" {
			continue
		}
		for _, a := range p.argv[1:] {
			if strings.Contains(a, "://") {
				t.Errorf("probe %s opens %s", p.name, a)
			}
		}
	}
}
