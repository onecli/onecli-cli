package main

import (
	"strings"
	"testing"
)

func TestBuildConfigFields(t *testing.T) {
	tests := []struct {
		name         string
		jsonPayload  string
		fieldFlags   []string
		clientID     string
		clientSecret string
		want         map[string]string
		wantErr      bool
	}{
		{
			name:       "repeated field flags",
			fieldFlags: []string{"appId=123", "appSlug=my-app", "privateKey=pem"},
			want:       map[string]string{"appId": "123", "appSlug": "my-app", "privateKey": "pem"},
		},
		{
			name:         "client id/secret sugar",
			clientID:     "id",
			clientSecret: "sec",
			want:         map[string]string{"clientId": "id", "clientSecret": "sec"},
		},
		{
			name:        "json payload merged with flag override",
			jsonPayload: `{"appId":"json","appSlug":"slug"}`,
			fieldFlags:  []string{"appId=flag"},
			want:        map[string]string{"appId": "flag", "appSlug": "slug"},
		},
		{
			name:       "value containing equals sign",
			fieldFlags: []string{"privateKey=a=b=c"},
			want:       map[string]string{"privateKey": "a=b=c"},
		},
		{
			name:    "no fields at all",
			wantErr: true,
		},
		{
			name:       "malformed field flag",
			fieldFlags: []string{"no-equals"},
			wantErr:    true,
		},
		{
			name:        "non-object json",
			jsonPayload: `["not","an","object"]`,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildConfigFields(tt.jsonPayload, tt.fieldFlags, tt.clientID, tt.clientSecret)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("fields = %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("fields[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestParseConditions(t *testing.T) {
	conditions, err := parseConditions(`[{"target":"body","operator":"contains","value":"x"}]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conditions) != 1 || conditions[0].Value != "x" {
		t.Errorf("conditions = %+v, want one body-contains-x", conditions)
	}

	if got, err := parseConditions(""); err != nil || got != nil {
		t.Errorf("empty input = (%v, %v), want (nil, nil)", got, err)
	}

	if _, err := parseConditions(`{"not":"an array"}`); err == nil {
		t.Error("want error for non-array input")
	}
}

func TestValidRuleActions(t *testing.T) {
	for _, action := range []string{"block", "rate_limit", "manual_approval", "allow"} {
		if !validRuleActions[action] {
			t.Errorf("action %q should be valid", action)
		}
	}
	if validRuleActions["inherit"] {
		t.Error("'inherit' is a permission setting, not a rule action")
	}
}

func TestValidPermissionSettings(t *testing.T) {
	for _, p := range []string{"allow", "manual_approval", "block", "inherit"} {
		if !validPermissionSettings[p] {
			t.Errorf("permission %q should be valid", p)
		}
	}
	if validPermissionSettings["rate_limit"] {
		t.Error("'rate_limit' is a rule action, not a permission setting")
	}
}

func TestBuildConnectFields(t *testing.T) {
	fields, err := buildConnectFields(`{"apiKey":"from-json"}`, []string{"region=us"})
	if err != nil {
		t.Fatalf("buildConnectFields: %v", err)
	}
	if fields["apiKey"] != "from-json" || fields["region"] != "us" {
		t.Errorf("fields = %v, want json+flag merge", fields)
	}

	if _, err := buildConnectFields("", nil); err == nil {
		t.Fatal("expected an error for no fields")
	} else if got := err.Error(); strings.Contains(got, "client-id") {
		t.Errorf("error mentions flags connect does not have: %q", got)
	}

	if _, err := buildConnectFields(`{}`, nil); err == nil {
		t.Fatal("expected an error for an empty JSON object")
	}
}
