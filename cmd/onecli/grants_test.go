package main

import (
	"strings"
	"testing"
)

// The flag → wire-body derivation: no flags = the uncustomized full attach;
// --allow/--ask derive a custom grant; --json takes the raw body and cannot
// combine with the flags.

func TestBuildConnectionGrantInput(t *testing.T) {
	t.Run("no flags derives the full attach", func(t *testing.T) {
		input, err := buildConnectionGrantInput("", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if input.Access != "full" || input.Allow != nil || input.Ask != nil {
			t.Errorf("got %+v, want the bare full grant", input)
		}
	})

	t.Run("allow alone derives custom", func(t *testing.T) {
		input, err := buildConnectionGrantInput("", "search_messages, get_message", "")
		if err != nil {
			t.Fatal(err)
		}
		if input.Access != "custom" {
			t.Errorf("access = %q, want custom", input.Access)
		}
		if len(input.Allow) != 2 || input.Allow[1] != "get_message" {
			t.Errorf("allow = %v (CSV must trim)", input.Allow)
		}
		if len(input.Ask) != 0 {
			t.Errorf("ask = %v, want empty", input.Ask)
		}
	})

	t.Run("ask alone derives custom", func(t *testing.T) {
		input, err := buildConnectionGrantInput("", "", "send_email")
		if err != nil {
			t.Fatal(err)
		}
		if input.Access != "custom" || len(input.Ask) != 1 {
			t.Errorf("got %+v", input)
		}
	})

	t.Run("both lists carry through", func(t *testing.T) {
		input, err := buildConnectionGrantInput("", "get_message", "send_email")
		if err != nil {
			t.Fatal(err)
		}
		if len(input.Allow) != 1 || len(input.Ask) != 1 {
			t.Errorf("got %+v", input)
		}
	})

	t.Run("json is taken verbatim", func(t *testing.T) {
		input, err := buildConnectionGrantInput(`{"access":"custom","allow":["a_tool"],"ask":[]}`, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if input.Access != "custom" || len(input.Allow) != 1 {
			t.Errorf("got %+v", input)
		}
	})

	t.Run("json rejects combining with flags", func(t *testing.T) {
		_, err := buildConnectionGrantInput(`{"access":"full"}`, "a_tool", "")
		if err == nil || !strings.Contains(err.Error(), "do not combine") {
			t.Errorf("err = %v, want the do-not-combine rejection", err)
		}
	})

	t.Run("json rejects an unknown access", func(t *testing.T) {
		_, err := buildConnectionGrantInput(`{"access":"partial"}`, "", "")
		if err == nil || !strings.Contains(err.Error(), "full") {
			t.Errorf("err = %v, want the access rejection", err)
		}
	})

	t.Run("an invalid tool id is rejected client-side", func(t *testing.T) {
		_, err := buildConnectionGrantInput("", "ok_tool,bad tool!", "")
		if err == nil || !strings.Contains(err.Error(), "invalid tool ID") {
			t.Errorf("err = %v, want the tool-id rejection", err)
		}
	})
}
