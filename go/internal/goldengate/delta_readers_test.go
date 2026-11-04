// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"strings"
	"testing"
)

func deltaReaderSpec() *DeltaReaderAssertions {
	return &DeltaReaderAssertions{
		RepositoryID: "repository:r_0c077ab3",
		ScopeID:      "git-repository-scope:repository:r_0c077ab3",
		Edges: []DeltaReaderEdge{
			{
				ID: "dr-control", Reader: "code_call_symbols", Relationship: "CALLS",
				FromLabel: "Function", FromName: "main", ToLabel: "Function", ToName: "sameFile",
			},
			{
				ID: "dr-broken", Reader: "code_call_symbols", Relationship: "CALLS",
				FromLabel: "Function", FromName: "main", ToLabel: "Function", ToName: "unchangedFile",
				ExpectedFailure: &DeltaReaderExpectedFailure{Defect: "#10003", Reason: "symbol reader drops base rows"},
			},
		},
	}
}

func edge(rel, fromLabel, fromName, toLabel, toName string) DeltaReaderObservedEdge {
	return DeltaReaderObservedEdge{
		Relationship: rel, FromLabels: []string{fromLabel}, FromName: fromName,
		ToLabels: []string{toLabel}, ToName: toName,
	}
}

func fullObservation() DeltaReaderObservation {
	return DeltaReaderObservation{
		ActiveGenerationID: "gen-full",
		Edges: []DeltaReaderObservedEdge{
			edge("CALLS", "Function", "main", "Function", "sameFile"),
			edge("CALLS", "Function", "main", "Function", "unchangedFile"),
		},
	}
}

func deltaObservation(edges ...DeltaReaderObservedEdge) DeltaReaderObservation {
	return DeltaReaderObservation{ActiveGenerationID: "gen-delta", ActiveIsDelta: true, Edges: edges}
}

func findingFor(t *testing.T, r Report, check string) Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Check == check {
			return f
		}
	}
	t.Fatalf("no finding %q in %+v", check, r.Findings)
	return Finding{}
}

func TestDeltaReadersFullStageRequiresEveryEdgeIncludingExpectedFailures(t *testing.T) {
	var r Report
	EvaluateDeltaReaderAssertions(deltaReaderSpec(), DeltaReaderStageFull, fullObservation(), true, &r)
	if r.Failed() {
		t.Fatalf("full stage with every edge present failed: %+v", r.Findings)
	}

	// Seeded violation: the full generation never produced the edge that
	// carries an expected failure. The delta stage would then pass that
	// entry vacuously, so the full stage must fail it.
	obs := fullObservation()
	obs.Edges = obs.Edges[:1]
	r = Report{}
	EvaluateDeltaReaderAssertions(deltaReaderSpec(), DeltaReaderStageFull, obs, true, &r)
	if f := findingFor(t, r, "edge_dr-broken"); f.OK || !f.Required {
		t.Fatalf("full stage passed an edge the full generation lacks: %+v", f)
	}
}

func TestDeltaReadersFullStageRejectsDeltaActiveScope(t *testing.T) {
	var r Report
	EvaluateDeltaReaderAssertions(deltaReaderSpec(), DeltaReaderStageFull, deltaObservation(fullObservation().Edges...), true, &r)
	if f := findingFor(t, r, "active_generation"); f.OK || !f.Required {
		t.Fatalf("full stage accepted a delta-active scope: %+v", f)
	}
}

func TestDeltaReadersDeltaStageExpectedFailureHolds(t *testing.T) {
	var r Report
	obs := deltaObservation(edge("CALLS", "Function", "main", "Function", "sameFile"))
	EvaluateDeltaReaderAssertions(deltaReaderSpec(), DeltaReaderStageDelta, obs, true, &r)
	if r.Failed() {
		t.Fatalf("delta stage with the expected loss failed: %+v", r.Findings)
	}
	f := findingFor(t, r, "edge_dr-broken")
	if !strings.Contains(f.Detail, "#10003") {
		t.Fatalf("expected-failure detail does not name the defect: %q", f.Detail)
	}
}

func TestDeltaReadersDeltaStageUnexpectedPassFails(t *testing.T) {
	// Seeded XPASS: the defect is fixed, so the edge survives. The entry must
	// fail until its expected_failure marker is removed, or the follow-up fix
	// never gets forced to flip the assertion.
	var r Report
	EvaluateDeltaReaderAssertions(deltaReaderSpec(), DeltaReaderStageDelta, deltaObservation(fullObservation().Edges...), true, &r)
	f := findingFor(t, r, "edge_dr-broken")
	if f.OK || !f.Required {
		t.Fatalf("XPASS was not a required failure: %+v", f)
	}
	if !strings.Contains(f.Detail, "remove expected_failure") {
		t.Fatalf("XPASS detail does not say how to flip it: %q", f.Detail)
	}
}

func TestDeltaReadersDeltaStageLostEdgeFails(t *testing.T) {
	// Seeded violation: the control edge (no expected failure) is lost.
	var r Report
	obs := deltaObservation(edge("CALLS", "Function", "main", "Function", "unchangedFile"))
	EvaluateDeltaReaderAssertions(deltaReaderSpec(), DeltaReaderStageDelta, obs, true, &r)
	if f := findingFor(t, r, "edge_dr-control"); f.OK || !f.Required {
		t.Fatalf("lost control edge passed: %+v", f)
	}
}

func TestDeltaReadersIgnoreExpectedFailuresAssertsFixedTruth(t *testing.T) {
	var r Report
	obs := deltaObservation(edge("CALLS", "Function", "main", "Function", "sameFile"))
	EvaluateDeltaReaderAssertions(deltaReaderSpec(), DeltaReaderStageDelta, obs, false, &r)
	if f := findingFor(t, r, "edge_dr-broken"); f.OK || !f.Required {
		t.Fatalf("ignoring expected failures still passed the lost edge: %+v", f)
	}
}

func TestDeltaReadersDeltaStageRejectsFullActiveScope(t *testing.T) {
	// Seeded vacuity: the delta never became active, so every edge is
	// trivially present from the full generation.
	var r Report
	EvaluateDeltaReaderAssertions(deltaReaderSpec(), DeltaReaderStageDelta, fullObservation(), true, &r)
	if f := findingFor(t, r, "active_generation"); f.OK || !f.Required {
		t.Fatalf("delta stage accepted a full-active scope: %+v", f)
	}
}

func TestDeltaReadersEdgeMatchIsExact(t *testing.T) {
	cases := map[string]DeltaReaderObservedEdge{
		"wrong relationship": edge("REFERENCES", "Function", "main", "Function", "sameFile"),
		"wrong from name":    edge("CALLS", "Function", "other", "Function", "sameFile"),
		"wrong to label":     edge("CALLS", "Function", "main", "Variable", "sameFile"),
		"wrong to name":      edge("CALLS", "Function", "main", "Function", "sameFileX"),
	}
	for name, observed := range cases {
		t.Run(name, func(t *testing.T) {
			var r Report
			obs := deltaObservation(observed, edge("CALLS", "Function", "main", "Function", "unchangedFile"))
			spec := deltaReaderSpec()
			spec.Edges = spec.Edges[:1]
			EvaluateDeltaReaderAssertions(spec, DeltaReaderStageDelta, obs, true, &r)
			if f := findingFor(t, r, "edge_dr-control"); f.OK {
				t.Fatalf("near-miss edge satisfied the assertion: %+v", f)
			}
		})
	}
	// A node with several labels still matches its asserted label.
	var r Report
	multi := edge("CALLS", "Function", "main", "Function", "sameFile")
	multi.FromLabels = []string{"Entity", "Function"}
	spec := deltaReaderSpec()
	spec.Edges = spec.Edges[:1]
	EvaluateDeltaReaderAssertions(spec, DeltaReaderStageDelta, deltaObservation(multi), true, &r)
	if r.Failed() {
		t.Fatalf("multi-label node did not match: %+v", r.Findings)
	}
}

func TestDeltaReadersInvalidSpecFails(t *testing.T) {
	cases := map[string]func(*DeltaReaderAssertions) *DeltaReaderAssertions{
		"missing section": func(*DeltaReaderAssertions) *DeltaReaderAssertions { return nil },
		"no edges":        func(s *DeltaReaderAssertions) *DeltaReaderAssertions { s.Edges = nil; return s },
		"no repository":   func(s *DeltaReaderAssertions) *DeltaReaderAssertions { s.RepositoryID = ""; return s },
		"no scope":        func(s *DeltaReaderAssertions) *DeltaReaderAssertions { s.ScopeID = " "; return s },
		"duplicate id":    func(s *DeltaReaderAssertions) *DeltaReaderAssertions { s.Edges[1].ID = "dr-control"; return s },
		"blank to name":   func(s *DeltaReaderAssertions) *DeltaReaderAssertions { s.Edges[0].ToName = ""; return s },
		"blank defect": func(s *DeltaReaderAssertions) *DeltaReaderAssertions {
			s.Edges[1].ExpectedFailure.Defect = ""
			return s
		},
		"defect not issue": func(s *DeltaReaderAssertions) *DeltaReaderAssertions {
			s.Edges[1].ExpectedFailure.Defect = "TODO"
			return s
		},
		"blank reason": func(s *DeltaReaderAssertions) *DeltaReaderAssertions {
			s.Edges[1].ExpectedFailure.Reason = ""
			return s
		},
		"gap without issue": func(s *DeltaReaderAssertions) *DeltaReaderAssertions {
			s.Unexpressed = []DeltaReaderGap{{Reader: "x", Reason: "y"}}
			return s
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var r Report
			EvaluateDeltaReaderAssertions(mutate(deltaReaderSpec()), DeltaReaderStageDelta, deltaObservation(fullObservation().Edges...), true, &r)
			if f := findingFor(t, r, "spec"); f.OK || !f.Required {
				t.Fatalf("invalid spec passed: %+v", f)
			}
		})
	}
}

func TestDeltaReadersUnknownStageFails(t *testing.T) {
	var r Report
	EvaluateDeltaReaderAssertions(deltaReaderSpec(), DeltaReaderStage("partial"), fullObservation(), true, &r)
	if f := findingFor(t, r, "stage"); f.OK || !f.Required {
		t.Fatalf("unknown stage passed: %+v", f)
	}
}

func TestDeltaReadersGapsAreReportedNotRequired(t *testing.T) {
	spec := deltaReaderSpec()
	spec.Unexpressed = []DeltaReaderGap{{Reader: "terraform_config_loaders", Defect: "#10006", Reason: "needs a state cassette"}}
	var r Report
	EvaluateDeltaReaderAssertions(spec, DeltaReaderStageFull, fullObservation(), true, &r)
	f := findingFor(t, r, "unexpressed_terraform_config_loaders")
	if !f.OK || f.Required || !strings.Contains(f.Detail, "#10006") {
		t.Fatalf("gap finding = %+v, want a passing advisory naming the defect", f)
	}
}
