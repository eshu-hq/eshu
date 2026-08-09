// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import "testing"

// Drain-evaluation tests, split from evaluate_test.go alongside the
// evaluate_drains.go split so both stay under the repo's 500-line cap.

func TestEvaluateDrains(t *testing.T) {
	a := strictDrainAssertions()
	t.Run("drained", func(t *testing.T) {
		var r Report
		EvaluateDrains(DrainCounts{}, a, 0, &r)
		if r.Failed() {
			t.Errorf("clean drain must pass; findings: %+v", r.Findings)
		}
	})
	t.Run("fact residual reports dead_letter subset", func(t *testing.T) {
		var r Report
		EvaluateDrains(DrainCounts{FactWorkItemsResidual: 3, FactWorkItemsDeadLetter: 2}, a, 0, &r)
		if !r.Failed() {
			t.Error("nonzero fact_work_items residual must fail")
		}
		var found bool
		for _, f := range r.Findings {
			if f.Check == "fact_work_items_residual" {
				found = true
				if !contains(f.Detail, "dead_letter=2") {
					t.Errorf("detail missing dead_letter breakdown: %q", f.Detail)
				}
			}
		}
		if !found {
			t.Error("missing fact_work_items_residual finding")
		}
	})
	t.Run("required intent nonterminal includes repo_dependency detail", func(t *testing.T) {
		var r Report
		EvaluateDrains(DrainCounts{SharedIntentsNonterminal: 2, SharedIntentsRequiredNonterminal: 2, RepoDependencyNonterminal: 2}, a, 0, &r)
		if !r.Failed() {
			t.Error("nonterminal required shared intents must fail (B-13 gate)")
		}
		var found bool
		for _, f := range r.Findings {
			if f.Check == "shared_projection_intents_nonterminal" {
				found = true
				if !contains(f.Detail, "repo_dependency subset=2") {
					t.Errorf("detail missing repo_dependency subset: %q", f.Detail)
				}
			}
		}
		if !found {
			t.Error("missing shared_projection_intents_nonterminal finding")
		}
	})
	t.Run("cross-scope completion event blocks drain", func(t *testing.T) {
		var r Report
		EvaluateDrains(DrainCounts{CrossScopeCompletionEventsNonterminal: 1}, a, 0, &r)
		if !r.Failed() {
			t.Error("nonterminal cross-scope completion event must fail")
		}
		var found bool
		for _, f := range r.Findings {
			if f.Check == "cross_scope_completion_events_nonterminal" {
				found = true
				if !contains(f.Detail, "nonterminal=1") {
					t.Errorf("detail missing completion-event count: %q", f.Detail)
				}
			}
		}
		if !found {
			t.Error("missing cross_scope_completion_events_nonterminal finding")
		}
	})
	t.Run("unpopulated pipeline fails the populated-then-drained guard", func(t *testing.T) {
		var r Report
		// Queues read empty but the reducer emitted nothing — must fail.
		EvaluateDrains(DrainCounts{PopulatedDomainsPresent: 0}, a, 1, &r)
		if !r.Failed() {
			t.Error("a drained-but-unreduced pipeline must fail the population guard")
		}
	})
	t.Run("populated and drained passes the guard", func(t *testing.T) {
		var r Report
		EvaluateDrains(DrainCounts{PopulatedDomainsPresent: 1}, a, 1, &r)
		if r.Failed() {
			t.Errorf("populated + drained must pass; findings: %+v", r.Findings)
		}
	})
	t.Run("advisory-domain nonterminal does not block", func(t *testing.T) {
		var r Report
		EvaluateDrains(DrainCounts{SharedIntentsNonterminal: 6, SharedIntentsRequiredNonterminal: 0, SharedIntentsAdvisoryNonterminal: 6}, a, 0, &r)
		if r.Failed() {
			t.Errorf("advisory-domain nonterminal must not fail the gate; findings: %+v", r.Findings)
		}
		var advisory bool
		for _, f := range r.Findings {
			if f.Check == "shared_projection_intents_advisory_nonterminal" {
				advisory = true
				if f.Required {
					t.Error("advisory drain finding must not be required")
				}
			}
		}
		if !advisory {
			t.Error("missing advisory drain finding when advisory nonterminal > 0")
		}
	})
}

func TestDrainCountsDrained(t *testing.T) {
	a := strictDrainAssertions()
	if !(DrainCounts{}).Drained(a) {
		t.Error("zero counts must be drained")
	}
	if (DrainCounts{FactWorkItemsResidual: 1}).Drained(a) {
		t.Error("residual=1 must not be drained")
	}
	if (DrainCounts{SharedIntentsRequiredNonterminal: 1}).Drained(a) {
		t.Error("required nonterminal=1 must not be drained")
	}
	if (DrainCounts{CrossScopeCompletionEventsNonterminal: 1}).Drained(a) {
		t.Error("cross-scope completion nonterminal=1 must not be drained")
	}
	// Advisory-only nonterminal still counts as drained for poll convergence.
	if !(DrainCounts{SharedIntentsNonterminal: 5, SharedIntentsAdvisoryNonterminal: 5}).Drained(a) {
		t.Error("advisory-only nonterminal must be considered drained")
	}
}

// TestEvaluateDrainsFailsOnQuarantinedUnroutableIntents pins the assertion that
// keeps the #5984 quarantine honest.
//
// Completing quarantined intents makes the nonterminal count drain, so without
// this check the gate would go GREEN on a corpus that lost edges — trading a
// silent loss in the pipeline for a silent loss in the gate.
func TestEvaluateDrainsFailsOnQuarantinedUnroutableIntents(t *testing.T) {
	t.Parallel()

	report := &Report{}
	EvaluateDrains(DrainCounts{SharedIntentsUnroutableQuarantined: 1}, DrainAssertions{}, 0, report)

	var found bool
	for _, f := range report.Findings {
		if f.Check == "shared_projection_unroutable_quarantined" {
			found = true
			if f.OK {
				t.Error("check passed with a quarantined row; it must be a hard zero")
			}
			if !f.Required {
				t.Error("check is advisory; a lost edge must block the gate")
			}
		}
	}
	if !found {
		t.Fatal("no shared_projection_unroutable_quarantined check emitted")
	}
}

// TestDrainedIgnoresQuarantinedUnroutableIntents is the other half, and the
// more subtle one.
//
// Quarantined rows are terminal: the intent that produced them was completed,
// so the count never decreases. Including it in the poll predicate would mean
// the drain never converges, and a crisp assertion failure would surface as a
// drain TIMEOUT instead — which reads as "the pipeline needed longer" and hides
// the finding entirely.
func TestDrainedIgnoresQuarantinedUnroutableIntents(t *testing.T) {
	t.Parallel()

	counts := DrainCounts{SharedIntentsUnroutableQuarantined: 5}
	if !counts.Drained(DrainAssertions{}) {
		t.Fatal("Drained() = false with only quarantined rows outstanding; the poll would never converge and the real finding would surface as a timeout")
	}
}
