// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

const sourceMergeSHA = "4444444444444444444444444444444444444444"

func sourceFullJobs(withSentinel bool, omitRaceShard bool) string {
	step := `{"name":"Record full plan mode","status":"completed","conclusion":"success"}`
	if !withSentinel {
		step = `{"name":"Record full plan mode","status":"completed","conclusion":"skipped"}`
	}
	jobs := `{"name":"go-core","status":"completed","conclusion":"success","run_attempt":1,"steps":[` + step + `]},` +
		`{"name":"pr-plan","status":"completed","conclusion":"success","run_attempt":1,"steps":[` + step + `]},` +
		`{"name":"go-core-complete","status":"completed","conclusion":"success","run_attempt":1},` +
		`{"name":"go-race-complete","status":"completed","conclusion":"success","run_attempt":1}`
	for shard := 1; shard <= 4; shard++ {
		if omitRaceShard && shard == 4 {
			continue
		}
		jobs += fmt.Sprintf(`,{"name":"go-race (%d)","status":"completed","conclusion":"success","run_attempt":1}`, shard)
	}
	return `[{"jobs":[` + jobs + `]}]`
}

func docsFullJobs() string {
	return `[{"jobs":[` +
		`{"name":"pr-plan","status":"completed","conclusion":"success","run_attempt":1,"steps":[{"name":"Record full plan mode","status":"completed","conclusion":"success"}]},` +
		`{"name":"go-core-complete","status":"completed","conclusion":"success","run_attempt":1},` +
		`{"name":"go-race-complete","status":"completed","conclusion":"success","run_attempt":1},` +
		`{"name":"call-docs / docs","status":"completed","conclusion":"success","run_attempt":1}` + `]}]`
}

func reuseAttemptJobs() string {
	return `[{"jobs":[` +
		`{"name":"pr-plan","status":"completed","conclusion":"success","run_attempt":1,"steps":[{"name":"Record full plan mode","status":"completed","conclusion":"skipped"},{"name":"Record reuse plan mode","status":"completed","conclusion":"success"}]},` +
		`{"name":"go-core-complete","status":"completed","conclusion":"success","run_attempt":1},` +
		`{"name":"go-race-complete","status":"completed","conclusion":"success","run_attempt":1}` + `]}]`
}

func boundFullSourceRoutes(sourceStatus string) []mappedGHRoute {
	return append([]mappedGHRoute{
		{match: "graphql", output: forcePushJSON(reuseOldSHA, reuseHeadSHA)},
		{match: "git/commits/" + reuseHeadSHA, output: commitJSON(reuseTreeSHA, reuseParentSHA)},
		{match: "git/commits/" + reuseOldSHA, output: commitJSON(reuseTreeSHA, "")},
		{match: "git/commits/" + reuseParentSHA, output: commitJSON(reuseOtherTree, "")},
		{match: "commits/" + reuseOldSHA + "/statuses", output: sourceStatus},
		{match: "actions/runs/101/attempts/1/jobs", output: sourceFullJobs(true, false)},
		{match: "actions/runs/101", output: testedRunJSON(reuseOldSHA, testedRefsJSON(sourceMergeSHA), 1)},
		{match: "git/commits/" + sourceMergeSHA, output: testedCommitJSON(sourceMergeSHA, reuseOldSHA)},
	}, successRoutes(reuseOldSHA)...)
}

func boundFullStatus() string {
	description, _ := formatTransferProof(transferProof{Mode: "full", Input: testedInput{RunID: 101, Attempt: 1, MergeSHA: sourceMergeSHA}})
	return `[{"context":"required-gates-complete","state":"success","description":"` + description + `"}]`
}

func TestProbeReuseBoundTestedTreeAndFullSource(t *testing.T) {
	t.Parallel()
	contexts := []string{"go-core-complete", "go-race-complete", "required-gates-complete"}
	for _, tc := range []struct {
		name      string
		mergeTree string
		wantReuse bool
	}{
		{name: "equal tested tree", mergeTree: testedTreeSHA, wantReuse: true},
		{name: "different tested tree", mergeTree: reuseOtherTree, wantReuse: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &mappedGHRunner{routes: boundFullSourceRoutes(boundFullStatus())}
			ctx := withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: testedBaseSHA, MergeTree: tc.mergeTree})
			got, err := probeReuse(ctx, runner, "eshu-hq/eshu", 42, reuseHeadSHA, contexts)
			if err != nil || got.Reuse != tc.wantReuse {
				t.Fatalf("reuse=%#v err=%v, want %v", got, err, tc.wantReuse)
			}
			if got.Reuse && (got.FullRootSHA != reuseOldSHA || got.MergeTree != tc.mergeTree) {
				t.Fatalf("wrong full-root or tested tree: %#v", got)
			}
		})
	}
}

func TestFullCoverageRejectsGreenTransferAggregatesAndLegacyLeaf(t *testing.T) {
	t.Parallel()
	input := testedInput{RunID: 101, Attempt: 1, MergeSHA: sourceMergeSHA, MergeTree: testedTreeSHA}
	selected := []resolvedRequiredGate{{WorkflowFile: "test.yml", Job: "go-core"}, {WorkflowFile: "test.yml", Job: "go-race-complete"}}
	for _, tc := range []struct {
		name         string
		jobs         string
		selected     []resolvedRequiredGate
		wantCoverage bool
	}{
		{name: "full", jobs: sourceFullJobs(true, false), selected: selected, wantCoverage: true},
		{name: "core applicable without race", jobs: sourceFullJobs(true, true), selected: selected[:1], wantCoverage: true},
		{name: "green aggregate missing heavy shard", jobs: sourceFullJobs(true, true), selected: selected},
		{name: "green smoke job skipped full sentinel", jobs: sourceFullJobs(false, false), selected: selected},
		{name: "independent legacy leaf", jobs: sourceFullJobs(true, false), selected: []resolvedRequiredGate{{WorkflowFile: "build.yml", Job: "build"}}},
		{name: "docs-only selected caller", jobs: docsFullJobs(), selected: []resolvedRequiredGate{{WorkflowFile: "docs.yml", Job: "docs"}}, wantCoverage: true},
		{name: "selected docs caller failed", jobs: strings.Replace(docsFullJobs(), `"name":"call-docs / docs","status":"completed","conclusion":"success"`, `"name":"call-docs / docs","status":"completed","conclusion":"failure"`, 1), selected: []resolvedRequiredGate{{WorkflowFile: "docs.yml", Job: "docs"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &mappedGHRunner{routes: []mappedGHRoute{{match: "actions/runs/101/attempts/1/jobs", output: tc.jobs}}}
			covered, err := proveFullCoverage(context.Background(), runner, "eshu-hq/eshu", input, tc.selected)
			if err != nil || covered != tc.wantCoverage {
				t.Fatalf("coverage=%v err=%v, want %v", covered, err, tc.wantCoverage)
			}
		})
	}
}

func TestTryProvenReuseRejectsFailedTargetCaller(t *testing.T) {
	t.Parallel()
	ctx := withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: testedBaseSHA, MergeTree: testedTreeSHA, RunStatus: "completed", RunConclusion: "failure"})
	runner := &mappedGHRunner{}
	terminal, err := tryProvenReuse(ctx, runner, &cigates.Registry{}, ".", "eshu-hq/eshu", 42, reuseHeadSHA, 0, "")
	if terminal || err != nil || len(runner.calls) != 0 {
		t.Fatalf("failed exact caller transferred: terminal=%v err=%v calls=%v", terminal, err, runner.calls)
	}
}

func TestPlanModeProofRefusesMismatch(t *testing.T) {
	t.Parallel()
	var pages []struct {
		Jobs []attemptJob `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(reuseAttemptJobs()), &pages); err != nil {
		t.Fatal(err)
	}
	jobs := make(map[string]attemptJob)
	for _, job := range pages[0].Jobs {
		jobs[job.Name] = job
	}
	if !planModeProvenByJobs(jobs, "reuse") || planModeProvenByJobs(jobs, "full") || planModeProvenByJobs(jobs, "smoke") {
		t.Fatal("exact pr-plan mode was not enforced")
	}
}

func TestProbeReuseDocsOnlyFullSource(t *testing.T) {
	t.Parallel()
	routes := boundFullSourceRoutes(boundFullStatus())
	for i := range routes {
		if routes[i].match == "actions/runs/101/attempts/1/jobs" {
			routes[i].output = docsFullJobs()
		}
	}
	ctx := withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: testedBaseSHA, MergeTree: testedTreeSHA})
	got, err := probeReuse(ctx, &mappedGHRunner{routes: routes}, "eshu-hq/eshu", 42, reuseHeadSHA,
		[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
	if err != nil || !got.Reuse || got.FullRootSHA != reuseOldSHA {
		t.Fatalf("docs-only full source did not reuse: %#v err=%v", got, err)
	}
}

func TestProbeReuseDirectFullRootMustRemainCurrent(t *testing.T) {
	t.Parallel()
	const rootMergeSHA = "8888888888888888888888888888888888888888"
	reuseDescription, _ := formatTransferProof(transferProof{Mode: "reuse", Input: testedInput{RunID: 101, Attempt: 1, MergeSHA: sourceMergeSHA}, FullRootSHA: reuseParentSHA})
	rootDescription, _ := formatTransferProof(transferProof{Mode: "full", Input: testedInput{RunID: 103, Attempt: 1, MergeSHA: rootMergeSHA}})
	for _, tc := range []struct {
		name       string
		rootState  string
		attempt    int
		conclusion string
		wantReuse  bool
		wantError  bool
	}{
		{name: "current full root", rootState: "success", attempt: 1, wantReuse: true},
		{name: "root status pending", rootState: "pending", attempt: 1},
		{name: "root rerun attempt advanced", rootState: "success", attempt: 2, wantError: true},
		{name: "root run red despite green status", rootState: "success", attempt: 1, conclusion: "failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := boundFullSourceRoutes(`[{"context":"required-gates-complete","state":"success","description":"` + reuseDescription + `"}]`)
			for i := range routes {
				if routes[i].match == "actions/runs/101/attempts/1/jobs" {
					routes[i].output = reuseAttemptJobs()
				}
			}
			rootRun := strings.Replace(testedRunJSON(reuseParentSHA, testedRefsJSON(rootMergeSHA), tc.attempt), `"id":101`, `"id":103`, 1)
			if tc.conclusion != "" {
				rootRun = strings.Replace(rootRun, `"conclusion":"success"`, `"conclusion":"`+tc.conclusion+`"`, 1)
			}
			routes = append([]mappedGHRoute{
				{match: "commits/" + reuseParentSHA + "/statuses", output: `[{"context":"required-gates-complete","state":"` + tc.rootState + `","description":"` + rootDescription + `"}]`},
				{match: "actions/runs/103/attempts/1/jobs", output: sourceFullJobs(true, false)},
				{match: "actions/runs/103", output: rootRun},
				{match: "git/commits/" + rootMergeSHA, output: testedCommitJSON(rootMergeSHA, reuseParentSHA)},
				{match: "git/commits/" + reuseParentSHA, output: commitJSON(reuseTreeSHA, "")},
			}, routes...)
			ctx := withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: testedBaseSHA, MergeTree: testedTreeSHA})
			got, err := probeReuse(ctx, &mappedGHRunner{routes: routes}, "eshu-hq/eshu", 42, reuseHeadSHA,
				[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
			if (err != nil) != tc.wantError || got.Reuse != tc.wantReuse {
				t.Fatalf("reuse=%#v err=%v, want reuse=%v error=%v", got, err, tc.wantReuse, tc.wantError)
			}
		})
	}
}

func TestProbeSmokeChecksHeadAndTestedMergeDeltas(t *testing.T) {
	t.Parallel()
	const protectedMergeTree = "9999999999999999999999999999999999999999"
	for _, tc := range []struct {
		name       string
		mergeTree  string
		mergePaths []string
		wantSmoke  bool
	}{
		{name: "disjoint base", mergeTree: reuseOtherTree, mergePaths: []string{"go/subject.go", "docs/disjoint.md"}, wantSmoke: true},
		{name: "merge-only protected workflow", mergeTree: protectedMergeTree, mergePaths: []string{"go/subject.go", "docs/disjoint.md", ".github/workflows/test.yml"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := boundFullSourceRoutes(boundFullStatus())
			for i := range routes {
				if routes[i].match == "git/commits/"+reuseHeadSHA {
					routes[i].output = commitJSON(reuseOtherTree, reuseParentSHA)
				}
			}
			routes = append([]mappedGHRoute{
				{match: "git/commits/" + testedBaseSHA, output: commitJSON(tc.mergeTree, "")},
				{match: "git/trees/" + reuseTreeSHA, output: treeJSONFixture("go/subject.go")},
				{match: "git/trees/" + testedTreeSHA, output: treeJSONFixture("go/subject.go")},
				{match: "git/trees/" + reuseOtherTree, output: treeJSONFixture("go/subject.go", "docs/disjoint.md")},
				{match: "git/trees/" + protectedMergeTree, output: treeJSONFixture(tc.mergePaths...)},
			}, routes...)
			ctx := withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: testedBaseSHA, MergeTree: tc.mergeTree})
			got, err := probeSmokeEvidence(ctx, &mappedGHRunner{routes: routes}, "eshu-hq/eshu", 42, reuseHeadSHA,
				[]string{"go/subject.go"}, []string{"go-core-complete", "go-race-complete", "required-gates-complete"})
			if err != nil || got.Applied != tc.wantSmoke {
				t.Fatalf("smoke=%#v err=%v, want %v", got, err, tc.wantSmoke)
			}
		})
	}
}

func TestBuildPRPlanUsesSameBoundTestedInput(t *testing.T) {
	t.Parallel()
	defs := []cigates.PRWorkflowDefinition{{File: "test.yml", Name: "Build Test", Synchronize: true, Jobs: []string{"go-core"}}}
	reg := &cigates.Registry{
		Gates:                []cigates.Gate{{ID: "go-build", Tier: cigates.TierPrePR, Triggers: []string{"go/**"}, CI: cigates.CI{Workflow: "test.yml", Job: "go-core"}}},
		RequiredStatusChecks: []cigates.RequiredStatusCheck{{Context: "go-core-complete"}, {Context: "go-race-complete"}, {Context: "required-gates-complete"}},
	}
	for _, tc := range []struct {
		name      string
		mergeTree string
		wantMode  string
	}{
		{name: "equal tested tree", mergeTree: testedTreeSHA, wantMode: "reuse"},
		{name: "different tested tree", mergeTree: reuseOtherTree, wantMode: "full"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := append([]mappedGHRoute{
				{match: "--json headRefOid", output: reuseHeadSHA},
				{match: "--json baseRefName", output: "main"},
				{match: "--json changedFiles", output: "1"},
				{match: "/pulls/42/files", output: `[[{"filename":"go/subject.go"}]]`},
			}, boundFullSourceRoutes(boundFullStatus())...)
			ctx := withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: testedBaseSHA, MergeTree: tc.mergeTree})
			plan, err := buildPRPlan(ctx, &mappedGHRunner{routes: routes}, reg, defs, "eshu-hq/eshu", 42, reuseHeadSHA)
			if err != nil || plan.Mode != tc.wantMode || plan.MergeTree != tc.mergeTree || plan.TargetRunID != 102 || plan.TargetAttempt != 1 {
				t.Fatalf("plan=%#v err=%v want mode=%s", plan, err, tc.wantMode)
			}
			if err := validatePRPlan(plan); err != nil {
				t.Fatalf("plan output invalid: %v", err)
			}
		})
	}
}
