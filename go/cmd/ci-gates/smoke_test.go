// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
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
	var compared compareResponseFixture
	if err := json.Unmarshal([]byte(compareFiles), &compared); err != nil {
		panic(err)
	}
	paths := make([]string, 0, len(compared.Files))
	for _, file := range compared.Files {
		paths = append(paths, file.Filename)
	}
	newTree := treeJSONFixture(paths...)
	return append([]mappedGHRoute{
		{match: "graphql", output: forcePushJSON(reuseOldSHA, reuseHeadSHA)},
		{match: "git/commits/" + reuseOldSHA, output: commitJSON(reuseTreeSHA, "")},
		{match: "git/commits/" + reuseHeadSHA, output: commitJSON(reuseOtherTree, "")},
		{match: "git/trees/" + reuseTreeSHA, output: treeJSONFixture()},
		{match: "git/trees/" + reuseOtherTree, output: newTree},
		{match: "commits/" + reuseOldSHA + "/statuses", output: `[{"context":"required-gates-complete","state":"success","description":"` + description + `"}]`},
	}, successRoutes(reuseOldSHA)...)
}

type compareResponseFixture struct {
	Files []struct {
		Filename string `json:"filename"`
	} `json:"files"`
}

func treeJSONFixture(paths ...string) string {
	entries := make([]string, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, `{"path":"`+path+`","mode":"100644","type":"blob","sha":"`+reuseTreeSHA+`"}`)
	}
	return `{"truncated":false,"tree":[` + strings.Join(entries, ",") + `]}`
}

const (
	fullRunDescription = "All path-selected blocking gates passed"
	smokeDescription   = "Smoke subset passed on a clean rebase; heavy verdicts transferred (see run log)"
	reuseDescription   = "Tree-identical reuse: verdict copied from a previous head (see run log)"
	reuseV2Description = "Tree-identical reuse v2: full-backed verdict copied from a previous head (see run log)"
)

func compareJSON(files ...string) string {
	parts := make([]string, 0, len(files))
	for _, f := range files {
		parts = append(parts, `{"filename":"`+f+`"}`)
	}
	return `{"files":[` + strings.Join(parts, ",") + `]}`
}

func TestProbeSmoke_LegacyUnboundFullRefuses(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: smokeRoutes(compareJSON("docs/x.md", "go/internal/other/y.go"))}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"}, false,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeSmoke returned error: %v", err)
	}
	if got {
		t.Fatal("legacy full description is not bound smoke evidence")
	}
}

func TestProbeSmoke_OverlappingBaseDeltaRefuses(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: smokeRoutes(compareJSON("go/cmd/ci-gates/reuse.go"))}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"}, false,
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
		[]string{"go/cmd/ci-gates/reuse.go"}, false,
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
		[]string{"go/cmd/ci-gates/reuse.go"}, false,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeSmoke returned error: %v", err)
	}
	if got {
		t.Fatal("a gate-registry delta must refuse smoke")
	}
}

// A candidate whose own success was a smoke pass refuses transfer:
// smoke-on-smoke chains would accumulate unbounded base drift with the
// heavy gates never re-running. One hop from a fully run tree is the
// bound.
func TestProbeSmoke_SmokeCandidateRefusesChain(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: smokeRoutesDesc(compareJSON("docs/x.md"), smokeDescription)}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"}, false,
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
func TestProbeSmoke_LegacyReuseV2Refuses(t *testing.T) {
	t.Parallel()

	runner := &mappedGHRunner{routes: smokeRoutesDesc(compareJSON("docs/x.md"), reuseV2Description)}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"}, false,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeSmoke returned error: %v", err)
	}
	if got {
		t.Fatal("unbound v2 reuse must not seed smoke")
	}
}

func TestProbeSmoke_HistoricalGenericReuseRefuses(t *testing.T) {
	runner := &mappedGHRunner{routes: smokeRoutesDesc(compareJSON("docs/x.md"), reuseDescription)}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"}, false,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil || got {
		t.Fatalf("historical generic reuse must refuse smoke: got=%v err=%v", got, err)
	}
	for _, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), "/git/trees/") {
			t.Fatal("unbacked source fetched endpoint trees")
		}
	}
}

// A truncated recursive tree cannot prove disjointness and must refuse.
func TestProbeSmoke_TruncatedTreeRefuses(t *testing.T) {
	t.Parallel()

	routes := append([]mappedGHRoute{{match: "git/trees/" + reuseOtherTree, output: `{"truncated":true,"tree":[]}`}}, smokeRoutes(compareJSON("docs/disjoint.md"))...)
	runner := &mappedGHRunner{routes: routes}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"}, false,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatalf("probeSmoke returned error: %v", err)
	}
	if got {
		t.Fatal("a truncated endpoint tree must refuse smoke")
	}
}

// The smoke filter keeps a resolved job when any gate it covers is a
// fast local tier, and drops ci-heavy/manual-only jobs: their verdicts
// transfer from the green candidate.
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

// An all-heavy selection filters to nothing. The await path must
// refuse smoke on an empty subset and run the full selection instead
// of publishing a smoke success with zero jobs run.
func TestSmokeSubsetAllHeavyIsEmpty(t *testing.T) {
	t.Parallel()

	reg := &cigates.Registry{Gates: []cigates.Gate{
		{ID: "heavy-one", Tier: cigates.TierCIHeavy},
		{ID: "manual-one", Tier: cigates.TierManual},
	}}
	resolved := []resolvedRequiredGate{
		{Job: "heavy", GateIDs: []string{"heavy-one"}},
		{Job: "manual", GateIDs: []string{"manual-one"}},
	}
	if got := smokeSubset(reg, resolved); len(got) != 0 {
		t.Fatalf("smokeSubset all-heavy = %v, want empty", got)
	}
}

func TestProbeSmoke_PRPlanManifestDeltaRefuses(t *testing.T) {
	t.Parallel()
	runner := &mappedGHRunner{routes: smokeRoutes(compareJSON("specs/ci-pr-workflows.d/frontend.yaml"))}
	got, err := probeSmoke(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"}, false,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("planner-manifest delta must refuse smoke transfer")
	}
}

func TestProbeSmokeEvidenceBindsFullBackedSourceAndCurrentTree(t *testing.T) {
	t.Parallel()
	runner := &mappedGHRunner{routes: smokeRoutes(compareJSON("docs/x.md"))}
	verdict, err := probeSmokeEvidence(context.Background(), runner, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go/cmd/ci-gates/reuse.go"}, false,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil || verdict.Applied {
		t.Fatalf("smoke provenance = %#v, err=%v", verdict, err)
	}
}

func TestSmokeSubsetPlannerAloneDoesNotCountAsFastWork(t *testing.T) {
	t.Parallel()
	planner := resolvedRequiredGate{WorkflowFile: "test.yml", WorkflowName: "Build Test", Job: "pr-plan", GateIDs: []string{"central-pr-plan"}}
	if smokeHasFastWork([]resolvedRequiredGate{planner}) {
		t.Fatal("planner-only smoke would transfer heavy results without a fast gate")
	}
	fast := resolvedRequiredGate{WorkflowFile: "test.yml", WorkflowName: "Build Test", Job: "go-core", GateIDs: []string{"go-fmt"}}
	if !smokeHasFastWork([]resolvedRequiredGate{planner, fast}) {
		t.Fatal("real fast work was excluded")
	}
}
