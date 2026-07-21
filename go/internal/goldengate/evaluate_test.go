// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

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

func TestEvaluateNodePresent(t *testing.T) {
	if f := EvaluateNodePresent("Repository", 0); f.OK || !f.Required {
		t.Errorf("0 nodes must fail required smoke: %+v", f)
	}
	if f := EvaluateNodePresent("Repository", 5); !f.OK || !f.Required {
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
	if f := EvaluateRequiredCorrelation(rc, 0, true); f.OK || !f.Required {
		t.Errorf("count 0 must fail and be required: %+v", f)
	}
	if f := EvaluateRequiredCorrelation(rc, 1, true); !f.OK {
		t.Errorf("count 1 must pass: %+v", f)
	}
	// An advisory correlation that falls short warns but does not block.
	if f := EvaluateRequiredCorrelation(rc, 0, false); f.OK || f.Required {
		t.Errorf("advisory shortfall must warn, not block: %+v", f)
	}
	// minimum_count of 0 is clamped to 1 — an existence assertion is never vacuous.
	rc0 := RequiredCorrelation{ID: "rc-x", Relationship: "X", MinimumCount: 0}
	if f := EvaluateRequiredCorrelation(rc0, 0, true); f.OK {
		t.Errorf("clamped minimum must require >= 1: %+v", f)
	}
}

// TestEvaluateRequiredSelfLoop is the keystone acceptance for eshu-hq/eshu#5349:
// a self-loop bound must be a closed range, not a floor, so it catches BOTH
// directions of regression on the same observed count — genuine recursion
// silently dropped (below the floor) and a re-introduced declaration-vs-
// call-site self-loop bug (eshu-hq/eshu#5332) silently inflating the count
// past the pinned ceiling, one spurious self-loop per declaration.
func TestEvaluateRequiredSelfLoop(t *testing.T) {
	rsl := RequiredSelfLoop{
		ID: "sl-dart-calls-recursion", Label: "Function", Relationship: "CALLS",
		NodeProperty: "language", NodePropertyValue: "dart",
		MinimumCount: 2, MaximumCount: 2,
	}
	if f := EvaluateRequiredSelfLoop(rsl, 2); !f.OK || !f.Required {
		t.Errorf("exact pinned count must pass and be required: %+v", f)
	}
	if f := EvaluateRequiredSelfLoop(rsl, 1); f.OK {
		t.Errorf("count below the floor (genuine recursion dropped) must fail: %+v", f)
	}
	// The #5332 regression shape: every declaration in the fixture becomes a
	// spurious self-loop, pushing the count well past the pinned ceiling.
	if f := EvaluateRequiredSelfLoop(rsl, 9); f.OK {
		t.Errorf("count above the ceiling (spurious declaration self-loops) must fail: %+v", f)
	}
	if f := EvaluateRequiredSelfLoop(rsl, 0); f.OK {
		t.Errorf("zero self-loops when 2 are pinned must fail: %+v", f)
	}
}

func TestEvaluateNodeAndEdgeCountAdvisory(t *testing.T) {
	rng := CountRange{Min: 15, Max: 30}
	// Advisory variant (required=false): out-of-range warns without blocking.
	if f := EvaluateNodeCount("Repository", rng, 5, false); f.OK || f.Required {
		t.Errorf("out-of-range advisory node count must warn without blocking: %+v", f)
	}
	if f := EvaluateNodeCount("Repository", rng, 20, false); !f.OK {
		t.Errorf("in-range node count must pass: %+v", f)
	}
	if f := EvaluateEdgeCount("DEPENDS_ON", rng, 100, false); f.OK || f.Required {
		t.Errorf("out-of-range advisory edge count must warn without blocking: %+v", f)
	}
	// Required variant (required=true, #3866 full-corpus mode): out-of-range blocks.
	if f := EvaluateNodeCount("Repository", rng, 5, true); f.OK || !f.Required {
		t.Errorf("out-of-range required node count must block: %+v", f)
	}
	if f := EvaluateEdgeCount("DEPENDS_ON", rng, 100, true); f.OK || !f.Required {
		t.Errorf("out-of-range required edge count must block: %+v", f)
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
			f := EvaluateQueryShape("list_indexed_repositories", shape, []byte(c.body))
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
		f := EvaluateQueryShape("operator-control-plane", shape, []byte(`{"version":"1","health":"ok"}`))
		if !f.OK {
			t.Errorf("object-shaped response with all fields must pass: %s", f.Detail)
		}
	})

	t.Run("nested dead-code bucket paths and values", func(t *testing.T) {
		shape := QueryShape{
			RequiredResponseFields: []string{"data", "truth", "error"},
			RequiredJSONPaths: []string{
				"data.candidate_buckets.dead[]",
				"data.candidate_buckets.live_by_consumer[].consumer_evidence[].citation",
				"data.candidate_buckets.live_by_consumer[].consumer_evidence[].confidence_label",
				"data.candidate_buckets.unknown[].needs_evidence_reasons[]",
				"data.candidate_buckets.suppressed[]",
			},
			RequiredJSONValues: map[string]any{
				"truth.level":      "derived",
				"truth.basis":      "hybrid",
				"data.query_shape": "bounded_cross_repo_dead_code",
				"data.candidate_buckets.live_by_consumer[].classification": "live_by_consumer",
				"data.candidate_buckets.unknown[].classification":          "unknown_needs_evidence",
			},
		}
		body := []byte(`{
		  "data": {
		    "query_shape": "bounded_cross_repo_dead_code",
		    "candidate_buckets": {
		      "dead": [{"entity_id": "producer-dead"}],
		      "live_by_consumer": [{
		        "entity_id": "producer-live",
		        "classification": "live_by_consumer",
		        "consumer_evidence": [{
		          "citation": "code_reachability_rows:scope/gen/consumer/root/producer-live",
		          "confidence_label": "high"
		        }]
		      }],
		      "unknown": [{
		        "entity_id": "producer-unknown",
		        "classification": "unknown_needs_evidence",
		        "needs_evidence_reasons": ["ambiguous_consumer_ownership"]
		      }],
		      "suppressed": [{"entity_id": "producer-root"}]
		    }
		  },
		  "truth": {"level": "derived", "basis": "hybrid"},
		  "error": null
		}`)
		if f := EvaluateQueryShape("dead-code-cross-repo", shape, body); !f.OK {
			t.Fatalf("dead-code query shape failed: %s", f.Detail)
		}

		missingCitation := []byte(`{
		  "data": {
		    "query_shape": "bounded_cross_repo_dead_code",
		    "candidate_buckets": {
		      "dead": [{"entity_id": "producer-dead"}],
		      "live_by_consumer": [{"classification": "live_by_consumer", "consumer_evidence": [{}]}],
		      "unknown": [{"classification": "unknown_needs_evidence", "needs_evidence_reasons": ["ambiguous_consumer_ownership"]}],
		      "suppressed": [{"entity_id": "producer-root"}]
		    }
		  },
		  "truth": {"level": "derived", "basis": "hybrid"},
		  "error": null
		}`)
		if f := EvaluateQueryShape("dead-code-cross-repo", shape, missingCitation); f.OK {
			t.Fatalf("missing nested citation passed unexpectedly: %s", f.Detail)
		}
	})
}

func TestEvaluateQuerySurfaceParity(t *testing.T) {
	snap := Snapshot{QueryShapes: QueryShapes{
		HTTP: map[string]QueryShape{
			"GET /api/v0/repositories": {
				Description: "repository list",
				TruthClass:  "deterministic",
			},
		},
		MCP: map[string]QueryShape{
			"list_indexed_repositories": {
				Description: "repository list tool",
				TruthClass:  "deterministic",
			},
		},
		CLI: map[string]QueryShape{
			"eshu list": {
				Description: "repository list CLI",
				Command:     []string{"list"},
				TruthClass:  "deterministic",
				ParityWith: []string{
					"http:GET /api/v0/repositories",
					"mcp:list_indexed_repositories",
				},
			},
		},
	}}

	var r Report
	EvaluateQuerySurfaceParity(snap, &r)
	if r.Failed() {
		t.Fatalf("parity metadata should pass: %+v", r.Findings)
	}

	snap.QueryShapes.MCP["list_indexed_repositories"] = QueryShape{TruthClass: "code_hint"}
	r = Report{}
	EvaluateQuerySurfaceParity(snap, &r)
	if !r.Failed() {
		t.Fatal("truth-class mismatch between API/MCP/CLI must fail")
	}
	if !contains(r.Findings[0].Detail, "truth class") {
		t.Fatalf("failure should name truth class mismatch: %+v", r.Findings)
	}
}

func TestEvaluateQueryShapeRequiredJSONObjectMatches(t *testing.T) {
	t.Parallel()

	shape := QueryShape{
		RequiredResponseFields: []string{"data"},
		RequiredJSONObjectMatches: map[string][]map[string]any{
			"data.topology_edges[]": {
				{
					"relationship_type": "DEFINES",
					"source_id":         "repository:r_fixture",
					"target_id":         "workload:fixture",
				},
				{
					"relationship_type": "INSTANCE_OF",
					"source_id":         "instance:fixture:prod",
					"target_id":         "workload:fixture",
				},
			},
		},
	}
	body := []byte(`{"data":{"topology_edges":[{"relationship_type":"DEFINES","source_id":"repository:r_fixture","target_id":"workload:fixture","confidence":0.98},{"relationship_type":"INSTANCE_OF","source_id":"instance:fixture:prod","target_id":"workload:fixture","confidence":0.98}]}}`)
	if finding := EvaluateQueryShape("deployment-topology", shape, body); !finding.OK {
		t.Fatalf("exact endpoint objects failed: %s", finding.Detail)
	}

	mutated := []byte(`{"data":{"topology_edges":[{"relationship_type":"DEFINES","source_id":"workload:fixture","target_id":"repository:r_fixture"},{"relationship_type":"INSTANCE_OF","source_id":"instance:other:prod","target_id":"workload:fixture"}]}}`)
	if finding := EvaluateQueryShape("deployment-topology", shape, mutated); finding.OK {
		t.Fatalf("reversed or unrelated endpoint objects passed unexpectedly: %s", finding.Detail)
	}
}

func TestEvaluateTiming(t *testing.T) {
	baseline := 100 * time.Second
	t.Run("within 2x", func(t *testing.T) {
		var r Report
		EvaluateTiming(150*time.Second, baseline, 2.0, &r)
		if r.Failed() {
			t.Error("150s within 2x of 100s baseline must pass")
		}
	})
	t.Run("over 2x", func(t *testing.T) {
		var r Report
		EvaluateTiming(250*time.Second, baseline, 2.0, &r)
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
