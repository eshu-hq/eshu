// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		{match: "compare/" + testedBaseSHA + "..." + sourceMergeSHA, output: messageCompareJSON(sourceMergeSHA, reuseOldSHA, "safe")},
		{match: "compare/" + testedBaseSHA + "..." + targetMessageMergeSHA, output: messageCompareJSON(targetMessageMergeSHA, reuseHeadSHA, "safe")},
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
			ctx := withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: targetMessageMergeSHA, MergeTree: tc.mergeTree, BaseSHA: testedBaseSHA, HeadSHA: reuseHeadSHA})
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
	root := repoRoot(t)
	input := testedInput{RunID: 101, Attempt: 1, MergeSHA: sourceMergeSHA, MergeTree: testedTreeSHA}
	selected := []resolvedRequiredGate{{WorkflowFile: "test.yml", Job: "go-core"}, {WorkflowFile: "test.yml", Job: "go-race-complete"}}
	for _, tc := range []struct {
		name         string
		jobs         string
		selected     []resolvedRequiredGate
		path         string
		wantCoverage bool
	}{
		{name: "full", jobs: sourceFullJobs(true, false), selected: selected, path: "go/cmd/ci-gates/await.go", wantCoverage: true},
		{name: "core applicable without race row", jobs: sourceFullJobs(true, true), selected: selected[:1], path: "go/cmd/ci-gates/await.go"},
		{name: "green aggregate missing heavy shard", jobs: sourceFullJobs(true, true), selected: selected, path: "go/cmd/ci-gates/await.go"},
		{name: "green smoke job skipped full sentinel", jobs: sourceFullJobs(false, false), selected: selected, path: "go/cmd/ci-gates/await.go"},
		{name: "independent legacy leaf", jobs: sourceFullJobs(true, false), selected: []resolvedRequiredGate{{WorkflowFile: "build.yml", Job: "build"}}, path: ".github/workflows/build.yml"},
		{name: "docs-only selected caller", jobs: docsFullJobs(), selected: []resolvedRequiredGate{{WorkflowFile: "docs.yml", Job: "docs"}}, path: "docs/public/guide.md", wantCoverage: true},
		{name: "selected docs caller failed", jobs: strings.Replace(docsFullJobs(), `"name":"call-docs / docs","status":"completed","conclusion":"success"`, `"name":"call-docs / docs","status":"completed","conclusion":"failure"`, 1), selected: []resolvedRequiredGate{{WorkflowFile: "docs.yml", Job: "docs"}}, path: "docs/public/guide.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &mappedGHRunner{routes: []mappedGHRoute{{match: "actions/runs/101/attempts/1/jobs", output: tc.jobs}}}
			covered, err := proveFullCoverage(context.Background(), runner, "eshu-hq/eshu", input, tc.selected, root, []string{tc.path}, false)
			if err != nil || covered != tc.wantCoverage {
				t.Fatalf("coverage=%v err=%v, want %v", covered, err, tc.wantCoverage)
			}
		})
	}
}

func TestFullCoverageRequiresRaceForActualCodeSelection(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	reg, err := cigates.Load(filepath.Join(root, "specs/ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	input := testedInput{RunID: 101, Attempt: 1, MergeSHA: sourceMergeSHA, MergeTree: testedTreeSHA}
	for _, path := range []string{"go/cmd/ci-gates/await.go", "apps/console/src/App.tsx"} {
		t.Run(path, func(t *testing.T) {
			selected, err := selectRequiredGates(reg, root, []string{path}, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, gate := range selected {
				if gate.Job == "go-race-complete" {
					t.Fatal("fixture path unexpectedly selects the narrow race registry row")
				}
			}
			jobs := successfulSelectedJobs(t, sourceFullJobs(true, true), selected)
			runner := &mappedGHRunner{routes: []mappedGHRoute{{match: "actions/runs/101/attempts/1/jobs", output: jobs}}}
			covered, err := proveFullCoverage(context.Background(), runner, "eshu-hq/eshu", input, selected, root, []string{path}, false)
			if err != nil || covered {
				t.Fatalf("code path %s minted full proof without race shard: covered=%v err=%v", path, covered, err)
			}
		})
	}
}

func TestFullCodeApplicabilityUsesTrustedFilter(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, tc := range []struct {
		path string
		want bool
	}{
		{path: "README.md"},
		{path: "docs/public/guide.md"},
		{path: "go/cmd/ci-gates/README.md", want: true},
		{path: "apps/console/src/App.tsx", want: true},
	} {
		got, err := fullCodeApplicable(root, []string{tc.path}, false)
		if err != nil || got != tc.want {
			t.Fatalf("code applicability for %s = %v, %v; want %v", tc.path, got, err, tc.want)
		}
	}
	changedRoot := t.TempDir()
	workflow := filepath.Join(changedRoot, ".github/workflows/test.yml")
	if err := os.MkdirAll(filepath.Dir(workflow), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("jobs:\n  changes:\n    steps:\n      - uses: dorny/paths-filter@v3\n        with:\n          predicate-quantifier: every\n          filters: |\n            code: ['**']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fullCodeApplicable(changedRoot, []string{"README.md"}, true); err == nil {
		t.Fatal("changed code policy minted full proof even for truncated paths")
	}
}

func successfulSelectedJobs(t *testing.T, raw string, selected []resolvedRequiredGate) string {
	t.Helper()
	var pages []struct {
		Jobs []attemptJob `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(raw), &pages); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, job := range pages[0].Jobs {
		seen[job.Name] = true
	}
	for _, gate := range selected {
		name := gate.Job
		if gate.WorkflowFile != "test.yml" {
			name = calledCheckName(gate.WorkflowFile, gate.Job)
		}
		if !seen[name] {
			pages[0].Jobs = append(pages[0].Jobs, attemptJob{Name: name, Status: "completed", Conclusion: "success", Attempt: 1})
			seen[name] = true
		}
	}
	encoded, err := json.Marshal(pages)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
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
	ctx := withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: targetMessageMergeSHA, MergeTree: testedTreeSHA, BaseSHA: testedBaseSHA, HeadSHA: reuseHeadSHA})
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
		name               string
		rootState          string
		attempt            int
		conclusion         string
		raceShardSucceeded bool
		wantReuse          bool
		wantError          bool
		wantSource         string
	}{
		{name: "current full root", rootState: "success", attempt: 1, wantReuse: true},
		{name: "root status pending", rootState: "pending", attempt: 1},
		{name: "root rerun attempt advanced", rootState: "success", attempt: 2, wantError: true},
		{name: "root run red despite green status", rootState: "success", attempt: 1, conclusion: "failure"},
		{name: "reuse candidate ran a race shard", rootState: "success", attempt: 1, raceShardSucceeded: true, wantReuse: true, wantSource: reuseParentSHA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := boundFullSourceRoutes(`[{"context":"required-gates-complete","state":"success","description":"` + reuseDescription + `"}]`)
			for i := range routes {
				if routes[i].match == "actions/runs/101/attempts/1/jobs" {
					candidateJobs := reuseAttemptJobs()
					if tc.raceShardSucceeded {
						candidateJobs = strings.Replace(candidateJobs, `]}]`, `,{"name":"go-race (1)","status":"completed","conclusion":"success","run_attempt":1}]}]`, 1)
					}
					routes[i].output = candidateJobs
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
				{match: "compare/" + testedBaseSHA + "..." + rootMergeSHA, output: messageCompareJSON(rootMergeSHA, reuseParentSHA, "safe")},
				{match: "git/commits/" + reuseParentSHA, output: commitJSON(reuseTreeSHA, "")},
			}, routes...)
			ctx := withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: targetMessageMergeSHA, MergeTree: testedTreeSHA, BaseSHA: testedBaseSHA, HeadSHA: reuseHeadSHA})
			if tc.raceShardSucceeded {
				_, _, bound, sourceErr := fullBackedSource(ctx, &mappedGHRunner{routes: routes}, "eshu-hq/eshu", 42, reuseOldSHA,
					[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
				if sourceErr != nil || bound {
					t.Fatalf("reuse attempt with a completed race shard remained eligible: bound=%v err=%v", bound, sourceErr)
				}
			}
			got, err := probeReuse(ctx, &mappedGHRunner{routes: routes}, "eshu-hq/eshu", 42, reuseHeadSHA,
				[]string{"go-core-complete", "go-race-complete", "required-gates-complete"})
			if (err != nil) != tc.wantError || got.Reuse != tc.wantReuse {
				t.Fatalf("reuse=%#v err=%v, want reuse=%v error=%v", got, err, tc.wantReuse, tc.wantError)
			}
			if tc.wantSource != "" && got.SourceSHA != tc.wantSource {
				t.Fatalf("reuse source=%s, want direct full root %s", got.SourceSHA, tc.wantSource)
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
			ctx := withTestedInput(context.Background(), testedInput{RunID: 102, Attempt: 1, MergeSHA: targetMessageMergeSHA, MergeTree: tc.mergeTree, BaseSHA: testedBaseSHA, HeadSHA: reuseHeadSHA})
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
