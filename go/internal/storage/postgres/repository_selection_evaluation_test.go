// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func selectionTestEvaluation() scope.SelectionEvaluation {
	return scope.SelectionEvaluation{
		SelectorID:            "sel_test",
		SelectorKind:          scope.SelectionSelectorKindGitHubOrg,
		Org:                   "BoatsGroup",
		EvaluatedAt:           time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		LivenessWindowSeconds: 48 * 3600,
	}
}

func TestNormalizeSelectionEvaluationRejectsBadBoundary(t *testing.T) {
	t.Parallel()

	base := selectionTestEvaluation()
	cases := map[string]func(*scope.SelectionEvaluation){
		"blank selector": func(evaluation *scope.SelectionEvaluation) { evaluation.SelectorID = "  " },
		"unknown kind":   func(evaluation *scope.SelectionEvaluation) { evaluation.SelectorKind = "filesystem" },
		"zero stamp":     func(evaluation *scope.SelectionEvaluation) { evaluation.EvaluatedAt = time.Time{} },
		"zero window":    func(evaluation *scope.SelectionEvaluation) { evaluation.LivenessWindowSeconds = 0 },
		"blank org":      func(evaluation *scope.SelectionEvaluation) { evaluation.Org = "" },
		"slashed org":    func(evaluation *scope.SelectionEvaluation) { evaluation.Org = "boatsgroup/extra" },
		"blank listed scope": func(evaluation *scope.SelectionEvaluation) {
			evaluation.Listed = []scope.EvaluatedRepository{{ScopeID: ""}}
		},
	}
	for name, mutate := range cases {
		evaluation := base
		mutate(&evaluation)
		if _, err := normalizeSelectionEvaluation(evaluation); err == nil {
			t.Fatalf("%s: normalizeSelectionEvaluation() error = nil, want caller-bug rejection", name)
		}
	}
}

func TestNormalizeSelectionEvaluationCanonicalizes(t *testing.T) {
	t.Parallel()

	evaluation := selectionTestEvaluation()
	evaluation.Listed = []scope.EvaluatedRepository{
		{ScopeID: " scope:b ", GitHubID: 2},
		{ScopeID: "scope:b", GitHubID: 3},
		{ScopeID: "scope:a"},
	}
	normalized, err := normalizeSelectionEvaluation(evaluation)
	if err != nil {
		t.Fatalf("normalizeSelectionEvaluation() error = %v", err)
	}
	if normalized.Org != "boatsgroup" {
		t.Fatalf("Org = %q, want lower-cased %q", normalized.Org, "boatsgroup")
	}
	if len(normalized.listedIDs) != 2 || normalized.listedIDs[0] != "scope:b" || normalized.listedIDs[1] != "scope:a" {
		t.Fatalf("listedIDs = %q, want trimmed deduped [scope:b scope:a]", normalized.listedIDs)
	}
	if got := normalized.listedGitHubIDs["scope:b"]; got != 2 {
		t.Fatalf("listedGitHubIDs[scope:b] = %d, want first id 2", got)
	}
}

func TestSelectionFullPlanStates(t *testing.T) {
	t.Parallel()

	evaluation := selectionTestEvaluation()
	evaluation.Listed = []scope.EvaluatedRepository{{ScopeID: "scope:kept", GitHubID: 11}}
	evaluation.Archived = []scope.EvaluatedRepository{{ScopeID: "scope:archived", GitHubID: 12}}
	evaluation.RuleExcluded = []scope.EvaluatedRepository{{ScopeID: "scope:ruled-out", GitHubID: 13}}
	normalized, err := normalizeSelectionEvaluation(evaluation)
	if err != nil {
		t.Fatalf("normalizeSelectionEvaluation() error = %v", err)
	}
	plan := normalized.fullPlan([]string{"scope:kept", "scope:archived", "scope:ruled-out", "scope:gone"})
	if len(plan.rows) != 4 {
		t.Fatalf("len(plan.rows) = %d, want 4", len(plan.rows))
	}
	states := make(map[string]string, len(plan.rows))
	for _, row := range plan.rows {
		states[row.scopeID] = row.state
	}
	want := map[string]string{
		"scope:kept":      scope.SelectionStateSelected,
		"scope:archived":  scope.SelectionStateArchivedExcluded,
		"scope:ruled-out": scope.SelectionStateRuleExcluded,
		"scope:gone":      scope.SelectionStateNotListed,
	}
	for id, wantState := range want {
		if states[id] != wantState {
			t.Fatalf("plan[%s] = %q, want %q", id, states[id], wantState)
		}
	}
	// The plan is sorted by scope ID for the ordered upsert.
	for i := 1; i < len(plan.rows); i++ {
		if plan.rows[i-1].scopeID >= plan.rows[i].scopeID {
			t.Fatalf("plan not sorted at %d: %q before %q", i, plan.rows[i-1].scopeID, plan.rows[i].scopeID)
		}
	}
}

func TestSelectionFullPlanIgnoresListedUnknownScopes(t *testing.T) {
	t.Parallel()

	evaluation := selectionTestEvaluation()
	evaluation.Listed = []scope.EvaluatedRepository{{ScopeID: "scope:new", GitHubID: 99}}
	normalized, err := normalizeSelectionEvaluation(evaluation)
	if err != nil {
		t.Fatalf("normalizeSelectionEvaluation() error = %v", err)
	}
	// A listed scope with no ingestion_scopes row yet is not recorded: the
	// plan covers known scopes only, and the next evaluation picks it up
	// once it has synced.
	plan := normalized.fullPlan(nil)
	if len(plan.rows) != 0 {
		t.Fatalf("len(plan.rows) = %d, want 0 (unknown scopes wait for their first sync)", len(plan.rows))
	}
}

func TestSelectionMassMissTripped(t *testing.T) {
	t.Parallel()

	planWithMissing := func(missing int) selectionPlan {
		plan := selectionPlan{}
		for i := range missing {
			plan.rows = append(plan.rows, selectionPlannedRow{
				scopeID: fmt.Sprintf("scope:gone-%03d", i),
				state:   scope.SelectionStateNotListed,
			})
		}
		return plan
	}

	// QA shape: 802 known, 26 newly missing -- far below max(10, 10%).
	if selectionMassMissTripped(selectionNewlyMissing(planWithMissing(26), nil), 802) {
		t.Fatalf("QA shape (26 missing of 802) tripped the guard, want it to proceed")
	}
	// Past the percentage arm: 81 newly missing of 802 exceeds max(10, 80).
	if !selectionMassMissTripped(selectionNewlyMissing(planWithMissing(81), nil), 802) {
		t.Fatalf("81 missing of 802 did not trip the guard, want guard_tripped")
	}
	// Absolute floor: 11 newly missing of 50 exceeds max(10, 5).
	if !selectionMassMissTripped(selectionNewlyMissing(planWithMissing(11), nil), 50) {
		t.Fatalf("11 missing of 50 did not trip the guard, want guard_tripped")
	}
	// At the boundary the guard holds: 10 missing of 50 does not exceed 10.
	if selectionMassMissTripped(selectionNewlyMissing(planWithMissing(10), nil), 50) {
		t.Fatalf("10 missing of 50 tripped the guard, want it to proceed (exceeds, not meets)")
	}
	// Already-excluded scopes do not count as newly missing: a small org
	// with long-missing repositories must not trip every cycle.
	prior := map[string]selectionPriorRow{
		"scope:gone-000": {state: scope.SelectionStateNotListed},
		"scope:gone-001": {state: scope.SelectionStateArchivedExcluded},
	}
	if newly := selectionNewlyMissing(planWithMissing(12), prior); newly != 10 {
		t.Fatalf("newly missing with 2 already-excluded = %d, want 10", newly)
	}
	if selectionMassMissTripped(selectionNewlyMissing(planWithMissing(12), prior), 50) {
		t.Fatalf("10 newly + 2 already-excluded missing tripped the guard, want it to proceed")
	}
}

func TestSelectionPositiveRowsSelectedOnly(t *testing.T) {
	t.Parallel()

	evaluation := selectionTestEvaluation()
	evaluation.SelectorKind = scope.SelectionSelectorKindExplicit
	evaluation.Org = ""
	evaluation.Listed = []scope.EvaluatedRepository{{ScopeID: "scope:b"}, {ScopeID: "scope:a"}}
	normalized, err := normalizeSelectionEvaluation(evaluation)
	if err != nil {
		t.Fatalf("normalizeSelectionEvaluation() error = %v", err)
	}
	plan := normalized.positiveRows()
	if len(plan.rows) != 2 {
		t.Fatalf("len(plan.rows) = %d, want 2", len(plan.rows))
	}
	for _, row := range plan.rows {
		if row.state != scope.SelectionStateSelected {
			t.Fatalf("plan[%s] = %q, want selected (explicit mode writes positive rows only)", row.scopeID, row.state)
		}
	}
}
