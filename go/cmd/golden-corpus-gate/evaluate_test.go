// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"
)

func int64p(v int64) *int64 { return &v }

func strictDrainAssertions() DrainAssertions {
	return DrainAssertions{
		FactWorkItems:           DrainBound{ResidualMax: int64p(0)},
		SharedProjectionIntents: DrainBound{NonterminalMax: int64p(0)},
	}
}

func TestEvaluateDrains(t *testing.T) {
	a := strictDrainAssertions()
	t.Run("drained", func(t *testing.T) {
		var r Report
		evaluateDrains(DrainCounts{}, a, 0, &r)
		if r.Failed() {
			t.Errorf("clean drain must pass; findings: %+v", r.Findings)
		}
	})
	t.Run("fact residual reports dead_letter subset", func(t *testing.T) {
		var r Report
		evaluateDrains(DrainCounts{FactWorkItemsResidual: 3, FactWorkItemsDeadLetter: 2}, a, 0, &r)
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
		evaluateDrains(DrainCounts{SharedIntentsNonterminal: 2, SharedIntentsRequiredNonterminal: 2, RepoDependencyNonterminal: 2}, a, 0, &r)
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
	t.Run("unpopulated pipeline fails the populated-then-drained guard", func(t *testing.T) {
		var r Report
		// Queues read empty but the reducer emitted nothing — must fail.
		evaluateDrains(DrainCounts{PopulatedDomainsPresent: 0}, a, 1, &r)
		if !r.Failed() {
			t.Error("a drained-but-unreduced pipeline must fail the population guard")
		}
	})
	t.Run("populated and drained passes the guard", func(t *testing.T) {
		var r Report
		evaluateDrains(DrainCounts{PopulatedDomainsPresent: 1}, a, 1, &r)
		if r.Failed() {
			t.Errorf("populated + drained must pass; findings: %+v", r.Findings)
		}
	})
	t.Run("advisory-domain nonterminal does not block", func(t *testing.T) {
		var r Report
		evaluateDrains(DrainCounts{SharedIntentsNonterminal: 6, SharedIntentsRequiredNonterminal: 0, SharedIntentsAdvisoryNonterminal: 6}, a, 0, &r)
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

func TestEvaluateNodePresent(t *testing.T) {
	if f := evaluateNodePresent("Repository", 0); f.OK || !f.Required {
		t.Errorf("0 nodes must fail required smoke: %+v", f)
	}
	if f := evaluateNodePresent("Repository", 5); !f.OK || !f.Required {
		t.Errorf("5 nodes must pass required smoke: %+v", f)
	}
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
	// Advisory-only nonterminal still counts as drained for poll convergence.
	if !(DrainCounts{SharedIntentsNonterminal: 5, SharedIntentsAdvisoryNonterminal: 5}).Drained(a) {
		t.Error("advisory-only nonterminal must be considered drained")
	}
}

func TestEvaluateRequiredCorrelation(t *testing.T) {
	rc := RequiredCorrelation{ID: "rc-1", Relationship: "CORRELATES_DEPLOYABLE_UNIT", FromLabel: "Repository", ToLabel: "Repository", MinimumCount: 1}
	if f := evaluateRequiredCorrelation(rc, 0, true); f.OK || !f.Required {
		t.Errorf("count 0 must fail and be required: %+v", f)
	}
	if f := evaluateRequiredCorrelation(rc, 1, true); !f.OK {
		t.Errorf("count 1 must pass: %+v", f)
	}
	// An advisory correlation that falls short warns but does not block.
	if f := evaluateRequiredCorrelation(rc, 0, false); f.OK || f.Required {
		t.Errorf("advisory shortfall must warn, not block: %+v", f)
	}
	// minimum_count of 0 is clamped to 1 — an existence assertion is never vacuous.
	rc0 := RequiredCorrelation{ID: "rc-x", Relationship: "X", MinimumCount: 0}
	if f := evaluateRequiredCorrelation(rc0, 0, true); f.OK {
		t.Errorf("clamped minimum must require >= 1: %+v", f)
	}
}

func TestEvaluateNodeAndEdgeCountAdvisory(t *testing.T) {
	rng := CountRange{Min: 15, Max: 30}
	if f := evaluateNodeCount("Repository", rng, 5); f.OK || f.Required {
		t.Errorf("out-of-range node count must warn (advisory): %+v", f)
	}
	if f := evaluateNodeCount("Repository", rng, 20); !f.OK {
		t.Errorf("in-range node count must pass: %+v", f)
	}
	if f := evaluateEdgeCount("DEPENDS_ON", rng, 100); f.OK || f.Required {
		t.Errorf("out-of-range edge count must warn (advisory): %+v", f)
	}
}

func TestEvaluateQueryShape(t *testing.T) {
	shape := QueryShape{
		RequiredResponseFields:   []string{"repositories"},
		MinimumResults:           1,
		ResultItemRequiredFields: []string{"id", "name"},
	}
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"ok", `{"repositories":[{"id":"r1","name":"a"}]}`, true},
		{"missing field", `{"repos":[]}`, false},
		{"too few results", `{"repositories":[]}`, false},
		{"item missing id", `{"repositories":[{"name":"a"}]}`, false},
		{"not json", `not-json`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := evaluateQueryShape("list_indexed_repositories", shape, []byte(c.body))
			if f.OK != c.want {
				t.Errorf("OK=%v, want %v (detail: %s)", f.OK, c.want, f.Detail)
			}
			if !f.Required {
				t.Error("query findings must be required")
			}
		})
	}

	t.Run("no array result field", func(t *testing.T) {
		// operator-control-plane: required fields, no array, no minimum.
		shape := QueryShape{RequiredResponseFields: []string{"version", "health"}}
		f := evaluateQueryShape("operator-control-plane", shape, []byte(`{"version":"1","health":"ok"}`))
		if !f.OK {
			t.Errorf("object-shaped response with all fields must pass: %s", f.Detail)
		}
	})
}

func TestEvaluateTiming(t *testing.T) {
	baseline := 100 * time.Second
	t.Run("within 2x", func(t *testing.T) {
		var r Report
		evaluateTiming(150*time.Second, baseline, 2.0, &r)
		if r.Failed() {
			t.Error("150s within 2x of 100s baseline must pass")
		}
	})
	t.Run("over 2x", func(t *testing.T) {
		var r Report
		evaluateTiming(250*time.Second, baseline, 2.0, &r)
		if !r.Failed() {
			t.Error("250s over 2x of 100s baseline must fail")
		}
	})
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
