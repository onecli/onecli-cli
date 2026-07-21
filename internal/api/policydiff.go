package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// A faithful Go port of the OneCLI server's staged-changes diff
// (packages/api/src/lib/policy-diff.ts) — keep the two in sync.
// The diff covers CUSTOM rules + the Default Rule's action only: bridge-
// derived rows (blocklist/equipment) re-materialize with fresh logicalIds and
// legitimately differ between draft and published, so they never count as
// staged changes. Rules are keyed by logicalId — the generation-stable
// identity (row ids regenerate on every publish).

// PolicyRuleChange is one added/changed/removed entry in a PolicyDiff.
type PolicyRuleChange struct {
	LogicalID string   `json:"logicalId"`
	Name      string   `json:"name"`
	Action    string   `json:"action"`
	Details   []string `json:"details"`
}

// PolicyDefaultChange records a staged Default Rule action flip.
type PolicyDefaultChange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// PolicyDiff summarizes the staged (draft vs published) differences.
type PolicyDiff struct {
	Added         []PolicyRuleChange   `json:"added"`
	Changed       []PolicyRuleChange   `json:"changed"`
	Removed       []PolicyRuleChange   `json:"removed"`
	Reordered     bool                 `json:"reordered"`
	DefaultChange *PolicyDefaultChange `json:"defaultChange"`
	Count         int                  `json:"count"`
}

// customsOf filters to user-owned rules (source "custom", non-default) in
// priority order — the only rows the staged diff considers.
func customsOf(rules []PolicyRule) []PolicyRule {
	customs := make([]PolicyRule, 0, len(rules))
	for _, r := range rules {
		if r.Source == "custom" && !r.IsDefault {
			customs = append(customs, r)
		}
	}
	sort.SliceStable(customs, func(i, j int) bool {
		return customs[i].Priority < customs[j].Priority
	})
	return customs
}

func rateLabel(r PolicyRule) string {
	if r.RateLimit != nil && r.RateLimitWindow != nil {
		return fmt.Sprintf("%d/%s", *r.RateLimit, *r.RateLimitWindow)
	}
	return "none"
}

func identitySig(r PolicyRule) string {
	parts := make([]string, 0, len(r.Identities))
	for _, i := range r.Identities {
		parts = append(parts, i.Type+":"+i.ID)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// targetSig canonicalizes a rule's targets for equality. Both sides come
// from the same server serialization, so a deterministic re-marshal of each
// target (fixed struct field order), sorted, is equality-faithful — the
// mirror of the TS lib's JSON.stringify-and-sort.
func targetSig(r PolicyRule) string {
	parts := make([]string, 0, len(r.Targets))
	for _, t := range r.Targets {
		b, err := json.Marshal(t)
		if err != nil {
			parts = append(parts, fmt.Sprintf("%+v", t))
			continue
		}
		parts = append(parts, string(b))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func conditionsSig(r PolicyRule) string {
	if r.Conditions == nil {
		return ""
	}
	b, err := json.Marshal(r.Conditions)
	if err != nil {
		return fmt.Sprintf("%+v", r.Conditions)
	}
	return string(b)
}

func actionLabel(a string) string {
	if a == "allow" {
		return "Allow"
	}
	return "Block"
}

// strPtrEq mirrors the TS lib's `(a ?? null) !== (b ?? null)` comparison:
// nil and "" are DISTINCT values (collapsing them would hide a real staged
// edit — or invent one — and miscount the publish guard's pending total).
func strPtrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// describeRuleChanges lists the compact, human field-level differences
// between the draft and published versions of one rule.
func describeRuleChanges(draft, published PolicyRule) []string {
	var details []string
	if draft.Name != published.Name {
		details = append(details, fmt.Sprintf("Renamed from %q", published.Name))
	}
	if !strPtrEq(draft.Description, published.Description) {
		details = append(details, "Description edited")
	}
	if draft.Action != published.Action {
		details = append(details, fmt.Sprintf(
			"Action: %s → %s", actionLabel(published.Action), actionLabel(draft.Action),
		))
	}
	if draft.Enabled != published.Enabled {
		if draft.Enabled {
			details = append(details, "Enabled")
		} else {
			details = append(details, "Disabled")
		}
	}
	if identitySig(draft) != identitySig(published) {
		details = append(details, "Applies-to edited")
	}
	if targetSig(draft) != targetSig(published) {
		details = append(details, "Target edited")
	}
	if draft.RequireApproval != published.RequireApproval {
		if draft.RequireApproval {
			details = append(details, "Now requires approval")
		} else {
			details = append(details, "Approval removed")
		}
	}
	if rateLabel(draft) != rateLabel(published) {
		details = append(details, fmt.Sprintf(
			"Rate limit: %s → %s", rateLabel(published), rateLabel(draft),
		))
	}
	if conditionsSig(draft) != conditionsSig(published) {
		details = append(details, "Conditions edited")
	}
	return details
}

// DiffPolicyChanges computes the staged diff between the draft and the
// active published generation (pass the two GET /rules lists plus the two
// GET /default results; nil defaults are tolerated).
func DiffPolicyChanges(draftRules, publishedRules []PolicyRule, draftDefault, publishedDefault *PolicyRule) PolicyDiff {
	draft := customsOf(draftRules)
	published := customsOf(publishedRules)

	publishedByID := make(map[string]PolicyRule, len(published))
	for _, r := range published {
		publishedByID[r.LogicalID] = r
	}
	draftByID := make(map[string]PolicyRule, len(draft))
	for _, r := range draft {
		draftByID[r.LogicalID] = r
	}

	diff := PolicyDiff{
		Added:   []PolicyRuleChange{},
		Changed: []PolicyRuleChange{},
		Removed: []PolicyRuleChange{},
	}

	for _, r := range draft {
		prev, ok := publishedByID[r.LogicalID]
		if !ok {
			diff.Added = append(diff.Added, PolicyRuleChange{
				LogicalID: r.LogicalID, Name: r.Name, Action: r.Action, Details: []string{},
			})
			continue
		}
		if details := describeRuleChanges(r, prev); len(details) > 0 {
			diff.Changed = append(diff.Changed, PolicyRuleChange{
				LogicalID: r.LogicalID, Name: r.Name, Action: r.Action, Details: details,
			})
		}
	}
	for _, r := range published {
		if _, ok := draftByID[r.LogicalID]; !ok {
			diff.Removed = append(diff.Removed, PolicyRuleChange{
				LogicalID: r.LogicalID, Name: r.Name, Action: r.Action, Details: []string{},
			})
		}
	}

	// Reorder: the rules both sides share, compared by relative order.
	var draftOrder, publishedOrder []string
	for _, r := range draft {
		if _, ok := publishedByID[r.LogicalID]; ok {
			draftOrder = append(draftOrder, r.LogicalID)
		}
	}
	for _, r := range published {
		if _, ok := draftByID[r.LogicalID]; ok {
			publishedOrder = append(publishedOrder, r.LogicalID)
		}
	}
	diff.Reordered = strings.Join(draftOrder, "|") != strings.Join(publishedOrder, "|")

	if draftDefault != nil && publishedDefault != nil && draftDefault.Action != publishedDefault.Action {
		diff.DefaultChange = &PolicyDefaultChange{
			From: publishedDefault.Action, To: draftDefault.Action,
		}
	}

	diff.Count = len(diff.Added) + len(diff.Changed) + len(diff.Removed)
	if diff.Reordered {
		diff.Count++
	}
	if diff.DefaultChange != nil {
		diff.Count++
	}
	return diff
}
