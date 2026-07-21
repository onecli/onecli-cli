package api

import "testing"

// Golden cases mirroring the OneCLI server's policy-diff lib
// (packages/api/src/lib/policy-diff.ts) — the two implementations must agree.

func diffRule(over func(*PolicyRule)) PolicyRule {
	r := PolicyRule{
		ID:        "id",
		LogicalID: "l1",
		Source:    "custom",
		Status:    "draft",
		Priority:  0,
		Enabled:   true,
		Name:      "rule",
		Action:    "allow",
		Targets: []PolicyTarget{
			{Kind: "network", HostPattern: "a.com"},
		},
	}
	if over != nil {
		over(&r)
	}
	return r
}

func TestDiffCleanScopeIsZero(t *testing.T) {
	draft := []PolicyRule{diffRule(nil)}
	published := []PolicyRule{diffRule(func(r *PolicyRule) {
		r.ID = "other-row-id" // row ids differ across generations — irrelevant
		r.Status = "published"
		r.Priority = 7 // absolute priorities differ; relative order governs
	})}
	dDef := diffRule(func(r *PolicyRule) { r.IsDefault = true; r.Source = "default"; r.Action = "block" })
	pDef := diffRule(func(r *PolicyRule) { r.IsDefault = true; r.Source = "default"; r.Action = "block" })

	diff := DiffPolicyChanges(draft, published, &dDef, &pDef)
	if diff.Count != 0 {
		t.Fatalf("clean scope: Count = %d, want 0 (%+v)", diff.Count, diff)
	}
}

func TestDiffExcludesDerivedSources(t *testing.T) {
	// Derived rows churn logicalIds each rematerialization — they must never
	// count as staged changes.
	draft := []PolicyRule{
		diffRule(func(r *PolicyRule) { r.Source = "blocklist"; r.LogicalID = "fresh-1" }),
		diffRule(func(r *PolicyRule) { r.Source = "equipment"; r.LogicalID = "fresh-2" }),
	}
	published := []PolicyRule{
		diffRule(func(r *PolicyRule) { r.Source = "blocklist"; r.LogicalID = "old-1"; r.Status = "published" }),
	}
	diff := DiffPolicyChanges(draft, published, nil, nil)
	if diff.Count != 0 {
		t.Fatalf("derived-only scope: Count = %d, want 0", diff.Count)
	}
}

func TestDiffAddedChangedRemoved(t *testing.T) {
	draft := []PolicyRule{
		diffRule(func(r *PolicyRule) { r.LogicalID = "keep"; r.Action = "block" }), // changed
		diffRule(func(r *PolicyRule) { r.LogicalID = "new"; r.Priority = 1 }),      // added
	}
	published := []PolicyRule{
		diffRule(func(r *PolicyRule) { r.LogicalID = "keep"; r.Status = "published" }),
		diffRule(func(r *PolicyRule) { r.LogicalID = "gone"; r.Status = "published"; r.Priority = 1 }),
	}
	diff := DiffPolicyChanges(draft, published, nil, nil)
	if len(diff.Added) != 1 || diff.Added[0].LogicalID != "new" {
		t.Errorf("Added = %+v", diff.Added)
	}
	if len(diff.Changed) != 1 || diff.Changed[0].LogicalID != "keep" {
		t.Errorf("Changed = %+v", diff.Changed)
	}
	if len(diff.Changed) == 1 && diff.Changed[0].Details[0] != "Action: Allow → Block" {
		t.Errorf("Changed details = %+v", diff.Changed[0].Details)
	}
	if len(diff.Removed) != 1 || diff.Removed[0].LogicalID != "gone" {
		t.Errorf("Removed = %+v", diff.Removed)
	}
	if diff.Count != 3 {
		t.Errorf("Count = %d, want 3", diff.Count)
	}
}

func TestDiffReorderAndDefaultChange(t *testing.T) {
	draft := []PolicyRule{
		diffRule(func(r *PolicyRule) { r.LogicalID = "a"; r.Priority = 0 }),
		diffRule(func(r *PolicyRule) { r.LogicalID = "b"; r.Priority = 1 }),
	}
	published := []PolicyRule{
		diffRule(func(r *PolicyRule) { r.LogicalID = "b"; r.Priority = 0; r.Status = "published" }),
		diffRule(func(r *PolicyRule) { r.LogicalID = "a"; r.Priority = 1; r.Status = "published" }),
	}
	dDef := diffRule(func(r *PolicyRule) { r.IsDefault = true; r.Action = "block" })
	pDef := diffRule(func(r *PolicyRule) { r.IsDefault = true; r.Action = "allow" })

	diff := DiffPolicyChanges(draft, published, &dDef, &pDef)
	if !diff.Reordered {
		t.Error("Reordered = false, want true")
	}
	if diff.DefaultChange == nil || diff.DefaultChange.From != "allow" || diff.DefaultChange.To != "block" {
		t.Errorf("DefaultChange = %+v", diff.DefaultChange)
	}
	if diff.Count != 2 {
		t.Errorf("Count = %d, want 2 (reorder + default)", diff.Count)
	}
}

func TestDiffFieldSensitivity(t *testing.T) {
	base := func() PolicyRule {
		return diffRule(func(r *PolicyRule) { r.Status = "published" })
	}
	limit := 5
	window := "minute"
	desc := "documented"
	emptyDesc := ""
	cases := []struct {
		name   string
		mutate func(*PolicyRule)
		detail string
	}{
		{"rename", func(r *PolicyRule) { r.Name = "renamed" }, `Renamed from "rule"`},
		{"description", func(r *PolicyRule) { r.Description = &desc }, "Description edited"},
		// The TS lib compares `?? null`, so "" and null are DISTINCT values —
		// collapsing them would hide (or invent) a staged edit.
		{"description-empty-vs-null", func(r *PolicyRule) { r.Description = &emptyDesc }, "Description edited"},
		{"approval", func(r *PolicyRule) { r.RequireApproval = true }, "Now requires approval"},
		{"rate", func(r *PolicyRule) { r.RateLimit = &limit; r.RateLimitWindow = &window }, "Rate limit: none → 5/minute"},
		{"disable", func(r *PolicyRule) { r.Enabled = false }, "Disabled"},
		{"identity", func(r *PolicyRule) { r.Identities = []PolicyIdentity{{Type: "agent", ID: "a"}} }, "Applies-to edited"},
		{"target", func(r *PolicyRule) { r.Targets = []PolicyTarget{{Kind: "network", HostPattern: "b.com"}} }, "Target edited"},
		{"conditions", func(r *PolicyRule) { r.Conditions = []any{map[string]any{"target": "body"}} }, "Conditions edited"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			draftRow := diffRule(nil)
			tc.mutate(&draftRow)
			diff := DiffPolicyChanges([]PolicyRule{draftRow}, []PolicyRule{base()}, nil, nil)
			if len(diff.Changed) != 1 {
				t.Fatalf("Changed = %+v, want 1 entry", diff.Changed)
			}
			found := false
			for _, d := range diff.Changed[0].Details {
				if d == tc.detail {
					found = true
				}
			}
			if !found {
				t.Errorf("Details = %+v, want %q", diff.Changed[0].Details, tc.detail)
			}
		})
	}
}
