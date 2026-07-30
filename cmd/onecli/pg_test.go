package main

import (
	"testing"

	"github.com/onecli/onecli-cli/internal/api"
)

func TestResolvePgTarget(t *testing.T) {
	conns := []api.PgConnection{
		{ID: "conn-1", Label: strPtr("main-db"), Host: "db.example.com", Port: 5432},
		{ID: "conn-2", Label: strPtr("analytics"), Host: "warehouse.example.com", Port: 6543},
		{ID: "conn-3", Label: strPtr("replica"), Host: "db.example.com", Port: 5433},
	}

	cases := []struct {
		name    string
		target  string
		wantID  string
		wantErr bool
	}{
		{"by id", "conn-2", "conn-2", false},
		{"by label", "main-db", "conn-1", false},
		{"label case-insensitive", "ANALYTICS", "conn-2", false},
		{"by host:port", "db.example.com:5433", "conn-3", false},
		{"host:port default 5432", "warehouse.example.com:6543", "conn-2", false},
		{"host normalized case+dot", "DB.Example.COM.:5432", "conn-1", false},
		{"bare host ambiguous (two ports)", "db.example.com", "", true},
		{"bare host unique", "warehouse.example.com", "conn-2", false},
		{"unknown", "nope.example.com", "", true},
		{"empty", "  ", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolvePgTarget(tc.target, conns)
			if tc.wantErr {
				if err == nil {
					t.Errorf("want error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != tc.wantID {
				t.Errorf("got %s, want %s", got.ID, tc.wantID)
			}
		})
	}
}

func TestNormalizeTargetHostPort(t *testing.T) {
	if got := normalizeTargetHostPort("db.example.com"); got != "db.example.com:5432" {
		t.Errorf("default port: %q", got)
	}
	if got := normalizeTargetHostPort("DB.example.com:6543"); got != "db.example.com:6543" {
		t.Errorf("explicit port: %q", got)
	}
	if got := normalizeTargetHostPort("[::1]:5432"); got != "[::1]:5432" {
		t.Errorf("ipv6: %q", got)
	}
}
