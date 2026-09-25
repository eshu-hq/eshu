// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// endpointRunner answers `gh api <endpoint>` calls by the first route whose
// key is a substring of the joined argv, and records every call. Unlike
// scriptedGHRunner it does not depend on call order, which the merge-group
// reader does not promise across its two endpoints.
type endpointRunner struct {
	routes map[string]string
	calls  []string
}

func (r *endpointRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	joined := strings.Join(args, " ")
	r.calls = append(r.calls, joined)
	keys := make([]string, 0, len(r.routes))
	for key := range r.routes {
		keys = append(keys, key)
	}
	// Longest key first so a specific route wins over a prefix of it.
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		if strings.Contains(joined, key) {
			return []byte(r.routes[key]), nil
		}
	}
	return nil, fmt.Errorf("unexpected gh invocation: %s", joined)
}

func TestChangedPathsForMergeGroupUsesThreeDotCompareAgainstBase(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{
		"compare/main..." + headSHAFixture: `{"files":[
			{"filename":"go/internal/graph/a.go"},
			{"filename":"docs/new.md","previous_filename":"docs/old.md"},
			{"filename":"go/internal/graph/a.go"}
		]}`,
	}}

	got, truncated, err := changedPathsForMergeGroup(context.Background(), runner, "eshu-hq/eshu", "main", headSHAFixture)
	if err != nil {
		t.Fatalf("changedPathsForMergeGroup: %v", err)
	}
	if truncated {
		t.Fatal("three files is not a truncated compare")
	}
	want := []string{"go/internal/graph/a.go", "docs/new.md", "docs/old.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v; want %v", got, want)
	}
	wantCall := "api repos/eshu-hq/eshu/compare/main..." + headSHAFixture
	if len(runner.calls) != 1 || runner.calls[0] != wantCall {
		t.Fatalf("calls = %v; want exactly [%s]", runner.calls, wantCall)
	}
}

func TestChangedPathsForMergeGroupReportsTruncationAtTheCompareFileCap(t *testing.T) {
	t.Parallel()

	var files []string
	for i := 0; i < mergeGroupCompareFileCap; i++ {
		files = append(files, fmt.Sprintf(`{"filename":"f%d"}`, i))
	}
	runner := &endpointRunner{routes: map[string]string{
		"compare/": `{"files":[` + strings.Join(files, ",") + `]}`,
	}}

	_, truncated, err := changedPathsForMergeGroup(context.Background(), runner, "eshu-hq/eshu", "main", headSHAFixture)
	if err != nil {
		t.Fatalf("changedPathsForMergeGroup: %v", err)
	}
	if !truncated {
		t.Fatal("a compare listing at the file cap may be partial and must select every blocking gate")
	}
}

func TestChangedPathsForMergeGroupFailsOnAnEmptyDiff(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{"compare/": `{"files":[]}`}}
	if _, _, err := changedPathsForMergeGroup(context.Background(), runner, "eshu-hq/eshu", "main", headSHAFixture); err == nil {
		t.Fatal("a merge group with no changed paths must not select zero gates and pass")
	}
}

// mergeGroupRuns is newest-first, as GitHub returns it: a newer Build Test
// run (suite 11) supersedes an older one (suite 10), and a pull_request run
// for the same SHA (suite 30) belongs to a different event.
const mergeGroupRuns = `[{"workflow_runs":[
	{"name":"Build Test","event":"merge_group","conclusion":null,"check_suite_id":11},
	{"name":"Build Test","event":"merge_group","conclusion":"cancelled","check_suite_id":10},
	{"name":"Static Contract Gates","event":"merge_group","conclusion":"success","check_suite_id":20},
	{"name":"Frontend","event":"pull_request","conclusion":"success","check_suite_id":30}
]}]`

const mergeGroupCheckRuns = `[{"check_runs":[
	{"name":"go-core-complete","status":"in_progress","conclusion":null,"check_suite":{"id":11}},
	{"name":"go-core-complete","status":"completed","conclusion":"cancelled","check_suite":{"id":10}},
	{"name":"Verify OpenAPI gate","status":"completed","conclusion":"success","check_suite":{"id":20}},
	{"name":"Verify route coverage gate","status":"completed","conclusion":"failure","check_suite":{"id":20}},
	{"name":"skipped leg","status":"completed","conclusion":"skipped","check_suite":{"id":20}},
	{"name":"marketing-site","status":"completed","conclusion":"success","check_suite":{"id":30}}
]}]`

func TestReadMergeGroupChecksJoinsCheckRunsToTheNewestMergeGroupRun(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{
		"actions/runs?": mergeGroupRuns,
		"commits/" + headSHAFixture + "/check-runs": mergeGroupCheckRuns,
	}}

	got, err := readMergeGroupChecks(context.Background(), runner, "eshu-hq/eshu", headSHAFixture)
	if err != nil {
		t.Fatalf("readMergeGroupChecks: %v", err)
	}
	want := []checkRollup{
		{Name: "go-core-complete", State: "IN_PROGRESS", Bucket: "pending", Workflow: "Build Test", Event: "merge_group"},
		{Name: "Verify OpenAPI gate", State: "SUCCESS", Bucket: "pass", Workflow: "Static Contract Gates", Event: "merge_group"},
		{Name: "Verify route coverage gate", State: "FAILURE", Bucket: "fail", Workflow: "Static Contract Gates", Event: "merge_group"},
		{Name: "skipped leg", State: "SKIPPED", Bucket: "skipping", Workflow: "Static Contract Gates", Event: "merge_group"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("checks =\n%#v\nwant\n%#v", got, want)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "actions/runs?") && !strings.Contains(call, "event=merge_group") {
			t.Errorf("run lookup %q must filter to merge_group runs", call)
		}
		if !strings.Contains(call, "--paginate") || !strings.Contains(call, "--slurp") {
			t.Errorf("call %q must paginate and slurp; the decoders read arrays of pages", call)
		}
	}
}

// The runs API documents no sort order for pagination, so the newest run per
// workflow must be chosen by run id, not by position. Here the superseded
// attempt (id 900, a completed success) is listed before the current one
// (id 901, still running); a first-seen rule would let the stale SUCCESS row
// satisfy the gate.
const mergeGroupRunsOldestFirst = `[{"workflow_runs":[
	{"id":900,"name":"Build Test","event":"merge_group","conclusion":"success","check_suite_id":40},
	{"id":901,"name":"Build Test","event":"merge_group","conclusion":null,"check_suite_id":41}
]}]`

const mergeGroupCheckRunsTwoAttempts = `[{"check_runs":[
	{"name":"go-core-complete","status":"completed","conclusion":"success","check_suite":{"id":40}},
	{"name":"go-core-complete","status":"in_progress","conclusion":null,"check_suite":{"id":41}}
]}]`

func TestReadMergeGroupChecksPicksTheNewestRunByIDNotListOrder(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{
		"actions/runs?": mergeGroupRunsOldestFirst,
		"commits/" + headSHAFixture + "/check-runs": mergeGroupCheckRunsTwoAttempts,
	}}

	got, err := readMergeGroupChecks(context.Background(), runner, "eshu-hq/eshu", headSHAFixture)
	if err != nil {
		t.Fatalf("readMergeGroupChecks: %v", err)
	}
	want := []checkRollup{
		{Name: "go-core-complete", State: "IN_PROGRESS", Bucket: "pending", Workflow: "Build Test", Event: "merge_group"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("checks =\n%#v\nwant the newest run (id 901) only:\n%#v", got, want)
	}
}

func TestCheckRollupBucketMirrorsGHAggregate(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"success":         "pass",
		"skipped":         "skipping",
		"neutral":         "skipping",
		"failure":         "fail",
		"timed_out":       "fail",
		"action_required": "fail",
		"startup_failure": "fail",
		"cancelled":       "cancel",
		"stale":           "pending",
	}
	for conclusion, want := range cases {
		if got := checkRunBucket("completed", conclusion); got != want {
			t.Errorf("bucket(completed, %s) = %q; want %q", conclusion, got, want)
		}
	}
	for _, status := range []string{"queued", "in_progress", "waiting", "pending", "requested"} {
		if got := checkRunBucket(status, ""); got != "pending" {
			t.Errorf("bucket(%s) = %q; want pending", status, got)
		}
	}
}

func TestAwaitMergeGroupRequiredChecksIgnoresPullRequestRows(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{
		WorkflowName: "Static Contract Gates",
		Job:          "Verify OpenAPI gate",
		GateIDs:      []string{"openapi-surface"},
		Event:        eventMergeGroup,
	}}
	// A green pull_request row with the same workflow and name must not satisfy
	// a merge-group gate: it tested the PR head, not the merge.
	prOnly := []checkRollup{{Name: "Verify OpenAPI gate", State: "SUCCESS", Bucket: "pass", Workflow: "Static Contract Gates", Event: "pull_request"}}
	if got := evaluateRequiredChecks(required, prOnly, nil); len(got.Pending) != 1 {
		t.Fatalf("pull_request row satisfied a merge_group gate: %#v", got)
	}
	mgGreen := []checkRollup{{Name: "Verify OpenAPI gate", State: "SUCCESS", Bucket: "pass", Workflow: "Static Contract Gates", Event: "merge_group"}}
	if got := evaluateRequiredChecks(required, mgGreen, nil); len(got.Pending)+len(got.Failed)+len(got.Cancelled) != 0 {
		t.Fatalf("green merge_group row did not satisfy its gate: %#v", got)
	}
}

func TestAwaitMergeGroupRequiredChecksFailsClosedOnRedGate(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{
		"actions/runs?": mergeGroupRuns,
		"commits/" + headSHAFixture + "/check-runs": mergeGroupCheckRuns,
	}}
	required := []resolvedRequiredGate{
		{WorkflowName: "Static Contract Gates", Job: "Verify OpenAPI gate", GateIDs: []string{"openapi-surface"}, Event: eventMergeGroup},
		{WorkflowName: "Static Contract Gates", Job: "Verify route coverage gate", GateIDs: []string{"route-coverage"}, Event: eventMergeGroup},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := awaitMergeGroupRequiredChecks(ctx, runner, "eshu-hq/eshu", headSHAFixture, required, time.Millisecond, io.Discard)
	if !errors.Is(err, errGateFailed) {
		t.Fatalf("err = %v; want errGateFailed for the red route-coverage gate", err)
	}
}

func TestValidateAwaitTarget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		event   string
		pr      int
		baseRef string
		wantErr bool
	}{
		{"pull request with number", eventPullRequest, 42, "main", false},
		{"pull request without number", eventPullRequest, 0, "main", true},
		{"merge group without number", eventMergeGroup, 0, "main", false},
		{"merge group with number is ambiguous", eventMergeGroup, 42, "main", true},
		{"merge group without base", eventMergeGroup, 0, "", true},
		{"unknown event", "push", 42, "main", true},
	}
	for _, tc := range cases {
		err := validateAwaitTarget(tc.event, tc.pr, tc.baseRef)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v; wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}

func TestWorkflowRunConclusionsForMergeGroupReadsOnlyMergeGroupRuns(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{"actions/runs?": `[{"workflow_runs":[
		{"name":"Build Test","event":"pull_request","conclusion":"cancelled"},
		{"name":"Static Contract Gates","event":"merge_group","conclusion":"cancelled"}
	]}]`}}

	got, err := workflowRunConclusionsForEvent(context.Background(), runner, "eshu-hq/eshu", headSHAFixture, eventMergeGroup)
	if err != nil {
		t.Fatalf("workflowRunConclusionsForEvent: %v", err)
	}
	if !got.cancelled("Static Contract Gates") {
		t.Error("a cancelled merge_group run must be reported cancelled for a merge-group aggregate")
	}
	if _, ok := got["Build Test"]; ok {
		t.Error("a cancelled pull_request run says nothing about the merge group and must be ignored")
	}
}

func TestWorkflowRunConclusionsPicksTheNewestRunByIDNotListOrder(t *testing.T) {
	t.Parallel()

	// A re-run attempt listed after the cancelled first attempt: list order
	// must not decide which run speaks for the workflow.
	runner := &endpointRunner{routes: map[string]string{"actions/runs?": `[{"workflow_runs":[
		{"id":900,"name":"Build Test","event":"merge_group","conclusion":"cancelled"},
		{"id":901,"name":"Build Test","event":"merge_group","conclusion":null}
	]}]`}}

	got, err := workflowRunConclusionsForEvent(context.Background(), runner, "eshu-hq/eshu", headSHAFixture, eventMergeGroup)
	if err != nil {
		t.Fatalf("workflowRunConclusionsForEvent: %v", err)
	}
	if got.cancelled("Build Test") {
		t.Error("the older cancelled run (id 900) must not speak for the workflow")
	}
	if conclusion, ok := got["Build Test"]; !ok || conclusion != "" {
		t.Errorf("conclusion = %q, present %v; want the newest run (id 901) in flight", conclusion, ok)
	}
}
