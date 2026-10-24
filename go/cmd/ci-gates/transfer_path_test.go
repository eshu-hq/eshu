// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"testing"
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
		{name: "green aggregate missing heavy shard", jobs: sourceFullJobs(true, true), selected: selected},
		{name: "green smoke job skipped full sentinel", jobs: sourceFullJobs(false, false), selected: selected},
		{name: "independent legacy leaf", jobs: sourceFullJobs(true, false), selected: []resolvedRequiredGate{{WorkflowFile: "build.yml", Job: "build"}}},
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
