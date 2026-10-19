// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func smokeRoutes(compareFiles string) []mappedGHRoute {
	return smokeRoutesDesc(compareFiles, fullRunDescription)
}

// smokeRoutesDesc scripts the candidate's required-gates-complete status
// with the given publisher description. The statuses route precedes the
// successRoutes one (first match wins) so the description survives.
func smokeRoutesDesc(compareFiles, description string) []mappedGHRoute {
	return append([]mappedGHRoute{
		{match: "graphql", output: forcePushJSON(reuseOldSHA, reuseHeadSHA)},
		{match: "compare/", output: compareFiles},
		{match: "commits/" + reuseOldSHA + "/statuses", output: `[{"context":"required-gates-complete","state":"success","description":"` + description + `"}]`},
	}, successRoutes(reuseOldSHA)...)
}

const (
	fullRunDescription = "All path-selected blocking gates passed"
	smokeDescription   = "Smoke subset passed on a clean rebase; heavy verdicts transferred (see run log)"
	reuseDescription   = "Tree-identical reuse: verdict copied from a previous head (see run log)"
)

func compareJSON(files ...string) string {
	parts := make([]string, 0, len(files))
	for _, f := range files {
		parts = append(parts, `{"filename":"`+f+`"}`)
	}
	return `{"files":[` + strings.Join(parts, ",") + `]}`
}

func TestProbeSmoke_DisjointBaseDeltaApplies(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: smokeRoutes(compareJSON("docs/x.md", "go/internal/other/y.go"))}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"},
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeSmoke returned error: %v", err)
	}
	if !got {
		t.Fatal("disjoint base delta with a green candidate must apply smoke")
	}
}

func TestProbeSmoke_OverlappingBaseDeltaRefuses(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: smokeRoutes(compareJSON("go/cmd/ci-gates/reuse.go"))}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"},
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeSmoke returned error: %v", err)
	}
	if got {
		t.Fatal("base delta touching a PR file must refuse smoke")
	}
}

func TestProbeSmoke_WorkflowDeltaRefuses(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: smokeRoutes(compareJSON(".github/workflows/test.yml"))}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"},
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeSmoke returned error: %v", err)
	}
	if got {
		t.Fatal("a workflow-definition delta must refuse smoke")
	}
}

// A base delta touching the gate registry refuses transfer: the old
// verdicts came from different gate definitions.
func TestProbeSmoke_GateDefinitionDeltaRefuses(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: smokeRoutes(compareJSON("specs/ci-gates.v1.yaml"))}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"},
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeSmoke returned error: %v", err)
	}
	if got {
		t.Fatal("a gate-registry delta must refuse smoke")
	}
}

// The smoke filter keeps a resolved job when any gate it covers is a
// fast local tier, and drops ci-heavy/manual-only jobs: their verdicts
// transfer from the green candidate.
// A candidate whose own success was a smoke pass refuses transfer:
// smoke-on-smoke chains would accumulate unbounded base drift with the
// heavy gates never re-running. One hop from a fully run tree is the
// bound.
func TestProbeSmoke_SmokeCandidateRefusesChain(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: smokeRoutesDesc(compareJSON("docs/x.md"), smokeDescription)}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"},
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeSmoke returned error: %v", err)
	}
	if got {
		t.Fatal("a smoke-passed candidate must refuse chained smoke")
	}
}

// A tree-identical-reuse candidate stays eligible: its tree equals a
// fully run tree byte for byte, so it is not a weaker hop.
func TestProbeSmoke_ReuseCandidateApplies(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: smokeRoutesDesc(compareJSON("docs/x.md"), reuseDescription)}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"},
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeSmoke returned error: %v", err)
	}
	if !got {
		t.Fatal("a tree-identical-reuse candidate must remain smoke-eligible")
	}
}

// A compare response at the page cap cannot prove disjointness and
// must refuse, even when every listed file is disjoint.
func TestProbeSmoke_TruncatedCompareRefuses(t *testing.T) {
	t.Parallel()

	files := make([]string, 0, 101)
	for i := range 101 {
		files = append(files, "docs/disjoint.md")
		_ = i
	}
	runner := &mappedGHRunner{routes: smokeRoutes(compareJSON(files...))}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"},
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeSmoke returned error: %v", err)
	}
	if got {
		t.Fatal("a possibly truncated compare must refuse smoke")
	}
}

func TestSmokeSubsetKeepsFastJobsOnly(t *testing.T) {
	t.Parallel()

	reg := &cigates.Registry{Gates: []cigates.Gate{
		{ID: "fast-one", Tier: cigates.TierPrePush},
		{ID: "heavy-one", Tier: cigates.TierCIHeavy},
		{ID: "mid-one", Tier: cigates.TierPrePR},
	}}
	resolved := []resolvedRequiredGate{
		{Job: "fast", GateIDs: []string{"fast-one"}},
		{Job: "heavy", GateIDs: []string{"heavy-one"}},
		{Job: "mixed", GateIDs: []string{"heavy-one", "mid-one"}},
		{Job: "unknown", GateIDs: []string{"not-in-registry"}},
	}
	got := smokeSubset(reg, resolved)
	jobs := make([]string, 0, len(got))
	for _, g := range got {
		jobs = append(jobs, g.Job)
	}
	// mid-one (pre-pr) is not smoke tier, so mixed drops too; an unknown
	// gate id cannot be proven fast, so it stays (fail closed to running).
	want := []string{"fast", "unknown"}
	if strings.Join(jobs, ",") != strings.Join(want, ",") {
		t.Fatalf("smokeSubset jobs = %v, want %v", jobs, want)
	}
}
