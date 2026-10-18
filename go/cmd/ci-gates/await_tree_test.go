// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

// Why this file exists (#8336).
//
// A pull-request merge-preview failure (PR #8334's registry-mirror failure on
// refs/remotes/pull/8334/merge) was published correctly on the PR head, yet
// the aggregate's own runs concluded failure filed under the main tip —
// workflow_run runs always report the default-branch head, and the row JSON
// carries no PR attribution. The await log named only workflow, job and run
// id, so nothing beside an awaited run said which tree it evaluated. These
// tests pin the other half of that fix: every await names its evaluated tree
// (ref and SHA), and every judged finding carries its owning run's tree.

// TestAwaitPRRequiredChecks_NamesEvaluatedTree pins the await header: a
// pull_request await evaluates the PR's merge preview, not the head commit
// itself. The head SHA attaches to the PR head and the merge-preview ref is
// named as what the runs evaluate: pairing the ref with the head SHA would
// print a false ref+SHA pair, because the merge preview resolves to its own
// merge commit (#8336).
func TestAwaitPRRequiredChecks_NamesEvaluatedTree(t *testing.T) {
	t.Parallel()

	runner := &scriptedGHRunner{results: []scriptedGHResult{{
		output: `[{"name":"verify-contracts","state":"COMPLETED","bucket":"pass","workflow":"Build Test","event":"pull_request"}]`,
	}}}
	required := []resolvedRequiredGate{{
		WorkflowName: "Build Test",
		Job:          "verify-contracts",
		GateIDs:      []string{"license-header"},
	}}
	var out bytes.Buffer
	if err := awaitPRRequiredChecks(context.Background(), runner, "eshu-hq/eshu", 42, headSHAFixture, required, time.Millisecond, &out); err != nil {
		t.Fatalf("awaitPRRequiredChecks returned error: %v", err)
	}
	text := out.String()
	if want := "pull_request head " + headSHAFixture + "; runs evaluate refs/remotes/pull/42/merge"; !strings.Contains(text, want) {
		t.Fatalf("pull_request await header must attach the SHA to the PR head, want %q in:\n%s", want, text)
	}
	if strings.Contains(text, "refs/remotes/pull/42/merge ("+headSHAFixture+")") {
		t.Fatalf("pull_request await header pairs the merge-preview ref with the head SHA, a false ref+SHA pair; got:\n%s", text)
	}
}

// TestAwaitMergeGroupRequiredChecks_NamesEvaluatedTree pins the same header
// for the queue: a merge_group await evaluates the queue branch at the group
// commit, and the log must name both.
func TestAwaitMergeGroupRequiredChecks_NamesEvaluatedTree(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{
		"actions/runs?": `[{"workflow_runs":[
			{"id":21,"name":"Static Contract Gates","event":"merge_group","conclusion":"success","check_suite_id":20,"head_branch":"` + queueBranchFixture + `","head_sha":"` + headSHAFixture + `"}
		]}]`,
		"commits/" + headSHAFixture + "/check-runs": `[{"check_runs":[
			{"name":"Verify OpenAPI gate","status":"completed","conclusion":"success","check_suite":{"id":20}}
		]}]`,
	}}
	required := []resolvedRequiredGate{{
		WorkflowName: "Static Contract Gates",
		Job:          "Verify OpenAPI gate",
		GateIDs:      []string{"openapi-surface"},
		Event:        eventMergeGroup,
	}}
	var out bytes.Buffer
	if err := awaitMergeGroupRequiredChecks(context.Background(), runner, "eshu-hq/eshu", headSHAFixture, queueBranchFixture, required, time.Millisecond, &out); err != nil {
		t.Fatalf("awaitMergeGroupRequiredChecks returned error: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, queueBranchFixture) || !strings.Contains(text, headSHAFixture) {
		t.Fatalf("merge_group await output names no evaluated tree; got:\n%s", text)
	}
}

// TestReadEventChecks_CarriesEvaluatedTree pins the per-row half: rows joined
// to a workflow run carry that run's branch and SHA, so a judged finding can
// name the tree beside its run id.
func TestReadEventChecks_CarriesEvaluatedTree(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{
		"actions/runs?": `[{"workflow_runs":[
			{"id":21,"name":"Static Contract Gates","event":"merge_group","conclusion":"success","check_suite_id":20,"head_branch":"` + queueBranchFixture + `","head_sha":"` + headSHAFixture + `"}
		]}]`,
		"commits/" + headSHAFixture + "/check-runs": `[{"check_runs":[
			{"name":"Verify OpenAPI gate","status":"completed","conclusion":"success","check_suite":{"id":20}}
		]}]`,
	}}
	got, err := readMergeGroupChecks(context.Background(), runner, "eshu-hq/eshu", headSHAFixture)
	if err != nil {
		t.Fatalf("readMergeGroupChecks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("checks = %#v; want the one joined row", got)
	}
	if got[0].HeadBranch != queueBranchFixture || got[0].HeadSHA != headSHAFixture {
		t.Fatalf("row carries tree %q @ %q; want %q @ %q (full %#v)",
			got[0].HeadBranch, got[0].HeadSHA, queueBranchFixture, headSHAFixture, got[0])
	}
}

// TestRunAttribution_OmitsAtSignWithoutSHA pins the branch-without-SHA
// shape: a finding that names a branch but has no SHA prints no trailing
// `@` (#8336). Unreachable from current readers (readEventChecks
// sets both fields from one run record; `gh pr checks` rows set neither),
// pinned so a future reader that sets one without the other stays readable.
func TestRunAttribution_OmitsAtSignWithoutSHA(t *testing.T) {
	t.Parallel()

	if got, want := runAttribution(eventPullRequest, "feat/x", "", 1), " (run 1 pull_request feat/x)"; got != want {
		t.Fatalf("runAttribution with a branch but no SHA = %q; want %q", got, want)
	}
}

// TestFormatFindings_NamesEvaluatedTreeBesideRun pins the finding line: a
// judged run is named with its event and tree, so the verdict that gets
// republished carries its attribution with it.
func TestFormatFindings_NamesEvaluatedTreeBesideRun(t *testing.T) {
	t.Parallel()

	got := formatFindings([]requiredCheckFinding{{
		WorkflowName: "Verify CI Gate Registry",
		Job:          "Verify ci-gate registry test mirror",
		GateIDs:      []string{"ci-gate-registry"},
		State:        "FAILURE",
		RunID:        39050801851,
		Event:        eventPullRequest,
		HeadBranch:   "feat/7803-batch2-go-sweep",
		HeadSHA:      "8a58b161c1d4d8117b1b697169b868b5760d83bf",
	}})
	for _, want := range []string{"39050801851", "pull_request", "feat/7803-batch2-go-sweep", "8a58b161"} {
		if !strings.Contains(got, want) {
			t.Fatalf("finding names no evaluated tree; got %q, want it to contain %q", got, want)
		}
	}
}
