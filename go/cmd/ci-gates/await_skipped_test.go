// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Why this file exists (#6189, second round).
//
// Two more GitHub shapes reached the aggregate's "everything else is a gate
// failure" arm without any gate having failed. They are kept apart here
// because they carry very different risk:
//
//   - STALE is terminal, unambiguous, and nobody's fault. It needs no lookup.
//   - SKIPPED is ambiguous. GitHub uses the same conclusion for "a `needs:`
//     dependency was cancelled so this never ran" and for "this job's own `if:`
//     excluded it", and the second MUST keep failing closed. Telling them apart
//     needs the owning workflow run's conclusion.
//
// TestAwaitGenuineSkipStillPublishesFailure is the load-bearing test in this
// file. It is deliberately first, and it was written and watched pass BEFORE
// the change it guards, so it characterizes behaviour that must not move
// rather than describing behaviour that was just written.

// headSHAFixture is the head the await loop is told to evaluate. Any non-empty
// value works: the stub runners key off the subcommand, not the SHA.
const headSHAFixture = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"

// routedRunner answers `gh pr checks` and `gh api .../actions/runs` from
// separate fixtures, and counts the run lookups so a test can assert the
// aggregate does NOT spend one on the common path.
type routedRunner struct {
	checks    []byte
	runs      []byte
	runsErr   error
	runsCalls int
}

func (r *routedRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	if strings.Contains(strings.Join(args, " "), "actions/runs") {
		r.runsCalls++
		if r.runsErr != nil {
			return nil, r.runsErr
		}
		return r.runs, nil
	}
	return r.checks, nil
}

// workflowRunsJSON builds the `gh api --paginate --slurp` shape: an array of
// pages, each an object carrying a workflow_runs array.
func workflowRunsJSON(t *testing.T, conclusionByWorkflow map[string]string) []byte {
	t.Helper()
	runs := make([]workflowRunConclusion, 0, len(conclusionByWorkflow))
	for name, conclusion := range conclusionByWorkflow {
		runs = append(runs, workflowRunConclusion{Name: name, Event: "pull_request", Conclusion: conclusion})
	}
	raw, err := json.Marshal([]struct {
		WorkflowRuns []workflowRunConclusion `json:"workflow_runs"`
	}{{WorkflowRuns: runs}})
	if err != nil {
		t.Fatalf("marshal workflow runs: %v", err)
	}
	return raw
}

func skippedGateFixture() ([]resolvedRequiredGate, []checkRollup) {
	required := []resolvedRequiredGate{{
		WorkflowName: "Build Test",
		Job:          "docs-helm-hygiene",
		GateIDs:      []string{"docs-catalog-metadata"},
	}}
	checks := []checkRollup{{
		Name:     "docs-helm-hygiene",
		Workflow: "Build Test",
		Event:    "pull_request",
		// gh maps both SKIPPED and NEUTRAL to the "skipping" bucket
		// (cli/cli v2.97.0 pkg/cmd/pr/checks/aggregate.go).
		Bucket: "skipping",
		State:  "SKIPPED",
	}}
	return required, checks
}

// TestAwaitGenuineSkipStillPublishesFailure is the contract four documents
// state and this change must not break: a selected gate GitHub reports as
// `skipped`, in a workflow run that was NOT cancelled, still fails the
// aggregate.
//
// The registry selected this gate for the changed paths, so "it did not run"
// is a real discrepancy between the registry and the workflow's own `if:`
// conditions. Waving it through would put a silent hole in the one status that
// guards the merge -- far worse than the overclaim being fixed here.
func TestAwaitGenuineSkipStillPublishesFailure(t *testing.T) {
	t.Parallel()

	required, checks := skippedGateFixture()
	runner := &routedRunner{
		checks: rollupJSON(t, checks),
		// The owning run finished normally, so nothing was cancelled and the
		// skip is the job's own decision.
		runs: workflowRunsJSON(t, map[string]string{"Build Test": "success"}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{})
	if err == nil {
		t.Fatal("a selected gate that never ran must not be reported as a clean pass")
	}
	code := classifyAwaitOutcome(err).exitCode()
	state, description, published := publishedRequiredStatus(t, code)
	if !published {
		t.Fatalf("exit code %d publishes nothing; a genuine skip must stay visible", code)
	}
	if state != "failure" {
		t.Fatalf("a genuine skip published state=%q description=%q (exit %d); want failure -- "+
			"a skip that is not a cancellation artifact must keep failing closed", state, description, code)
	}
	if !strings.Contains(err.Error(), "docs-helm-hygiene") {
		t.Errorf("failure message must name the gate that did not run, got %q", err.Error())
	}
}

// TestAwaitSkipCausedByCancelledRunDoesNotPublishFailure is the other half of
// the same fixture: identical check rollup, and only the owning run's
// conclusion differs. GitHub marks a job `skipped` when a `needs:` dependency
// is cancelled, so this is the #6189 overclaim wearing a different costume.
func TestAwaitSkipCausedByCancelledRunDoesNotPublishFailure(t *testing.T) {
	t.Parallel()

	required, checks := skippedGateFixture()
	runner := &routedRunner{
		checks: rollupJSON(t, checks),
		runs:   workflowRunsJSON(t, map[string]string{"Build Test": "cancelled"}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{})
	if err == nil {
		t.Fatal("a gate that never ran must not be reported as a clean pass")
	}
	code := classifyAwaitOutcome(err).exitCode()
	state, description, published := publishedRequiredStatus(t, code)
	if !published {
		t.Fatalf("exit code %d publishes nothing; the cancellation must stay visible", code)
	}
	if state == "failure" {
		t.Fatalf("a job skipped by its run's cancellation published state=%q description=%q (exit %d); no gate failed",
			state, description, code)
	}
	if state != "error" {
		t.Fatalf("skipped-by-cancellation published state=%q (exit %d); want error", state, code)
	}
	if runner.runsCalls == 0 {
		t.Error("the skipped gate must have been resolved against the owning run's conclusion")
	}
}

// TestAwaitSkippedRunLookupFailureFailsClosed pins requirement five: when the
// run conclusions cannot be read, the aggregate degrades to the behaviour it
// had before this change -- `failure` -- and never to a pass.
func TestAwaitSkippedRunLookupFailureFailsClosed(t *testing.T) {
	t.Parallel()

	required, checks := skippedGateFixture()
	runner := &routedRunner{
		checks:  rollupJSON(t, checks),
		runsErr: errors.New("HTTP 403: Resource not accessible by integration"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{})
	if err == nil {
		t.Fatal("a failed run lookup must not turn a skipped gate into a pass")
	}
	code := classifyAwaitOutcome(err).exitCode()
	state, description, published := publishedRequiredStatus(t, code)
	if !published || state != "failure" {
		t.Fatalf("a failed run lookup published state=%q description=%q published=%v; want failure (fail closed)",
			state, description, published)
	}
}

// TestAwaitDoesNotReadRunConclusionsWithoutASkippedCheck keeps the lookup off
// the common path. The await loop polls every 30 seconds for up to 55 minutes,
// so an unconditional call per poll would be real API traffic for a question
// that only a SKIPPED check can raise.
func TestAwaitDoesNotReadRunConclusionsWithoutASkippedCheck(t *testing.T) {
	t.Parallel()

	required, checks := cancelledTranscriptChecks()
	runner := &routedRunner{
		checks: rollupJSON(t, checks),
		runs:   workflowRunsJSON(t, map[string]string{"Build Test": "cancelled"}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{}); err == nil {
		t.Fatal("cancelled dependencies must not be reported as a clean pass")
	}
	if runner.runsCalls != 0 {
		t.Errorf("run conclusions were read %d time(s) with no skipped check in the rollup; the lookup must stay off the common path",
			runner.runsCalls)
	}
}

// TestAwaitStaleGateDoesNotStrandTheStatus is the STALE half, and it fixes a
// different defect than the skipped half does.
//
// gh sends STALE through its bucket switch's default arm, so it arrives
// bucketed "pending" (cli/cli v2.97.0 pkg/cmd/pr/checks/aggregate.go). Before
// this change the aggregate therefore treated a permanently-stale check as
// still running: it waited out the full timeout, exited 11, and the publisher
// wrote NOTHING -- leaving required-gates-complete on the pending status the
// first step posted, with no red check anywhere for an operator to act on.
func TestAwaitStaleGateDoesNotStrandTheStatus(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{
		WorkflowName: "Build Test",
		Job:          "go-core",
		GateIDs:      []string{"go-core"},
	}}
	checks := []checkRollup{{
		Name:     "go-core",
		Workflow: "Build Test",
		Event:    "pull_request",
		Bucket:   "pending",
		State:    "STALE",
	}}

	runner := &routedRunner{checks: rollupJSON(t, checks)}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{})
	if err == nil {
		t.Fatal("a stale gate never produced a verdict and must not pass")
	}
	code := classifyAwaitOutcome(err).exitCode()
	state, description, published := publishedRequiredStatus(t, code)
	if !published {
		t.Fatalf("exit code %d publishes nothing; a stale gate would strand the status on pending with no red check", code)
	}
	if state != "error" {
		t.Fatalf("stale gate published state=%q description=%q (exit %d); want error", state, description, code)
	}
	if runner.runsCalls != 0 {
		t.Errorf("STALE is self-describing and must not cost a run lookup, got %d", runner.runsCalls)
	}
}

// TestEvaluateRequiredChecksStaleIsNotPending pins the ordering the STALE fix
// depends on. STALE arrives in gh's "pending" bucket, so the not-a-gate-result
// test has to run BEFORE the pending-bucket test or it never fires.
func TestEvaluateRequiredChecksStaleIsNotPending(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{WorkflowName: "Build Test", Job: "go-core", GateIDs: []string{"go-core"}}}
	checks := []checkRollup{{
		Name: "go-core", Workflow: "Build Test", Event: "pull_request",
		Bucket: "pending", State: "STALE",
	}}

	got := evaluateRequiredChecks(required, checks, nil)
	if len(got.Pending) != 0 {
		t.Fatalf("pending = %#v; a stale check is terminal and waiting on it never ends", got.Pending)
	}
	if len(got.Failed) != 0 {
		t.Fatalf("failed = %#v; GitHub marked the run stale, no gate failed", got.Failed)
	}
	if len(got.Cancelled) != 1 {
		t.Fatalf("cancelled = %#v; a stale gate belongs with the outcomes that want a re-run", got.Cancelled)
	}
}

// TestEvaluateRequiredChecksKeepsGenuinelyPendingGatesWaiting is the guard for
// the ordering above: moving the not-a-gate-result test ahead of the pending
// bucket must not swallow a check that really is still running.
func TestEvaluateRequiredChecksKeepsGenuinelyPendingGatesWaiting(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{WorkflowName: "Build Test", Job: "go-core", GateIDs: []string{"go-core"}}}
	for _, state := range []string{"IN_PROGRESS", "QUEUED", "PENDING", "WAITING", "REQUESTED", "EXPECTED"} {
		checks := []checkRollup{{
			Name: "go-core", Workflow: "Build Test", Event: "pull_request",
			Bucket: "pending", State: state,
		}}
		got := evaluateRequiredChecks(required, checks, map[string]bool{"Build Test": true})
		if len(got.Pending) != 1 {
			t.Errorf("state=%s: pending = %#v; a running gate must keep the wait alive even when its run is cancelled",
				state, got.Pending)
		}
		if len(got.Cancelled) != 0 {
			t.Errorf("state=%s: cancelled = %#v; a running gate has not concluded anything", state, got.Cancelled)
		}
	}
}

// TestEvaluateRequiredChecksNeutralKeepsFailingClosed guards the narrowness of
// the skipped rule. gh files NEUTRAL in the same "skipping" bucket as SKIPPED,
// but NEUTRAL is a conclusion a job actually reached, so it must keep failing
// closed even when the owning run was cancelled. Keying the rule on the bucket
// instead of the state would quietly relax that.
func TestEvaluateRequiredChecksNeutralKeepsFailingClosed(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{WorkflowName: "Build Test", Job: "go-core", GateIDs: []string{"go-core"}}}
	checks := []checkRollup{{
		Name: "go-core", Workflow: "Build Test", Event: "pull_request",
		Bucket: "skipping", State: "NEUTRAL",
	}}

	got := evaluateRequiredChecks(required, checks, map[string]bool{"Build Test": true})
	if len(got.Failed) != 1 {
		t.Fatalf("failed = %#v; NEUTRAL is a conclusion the job reached and fails closed", got.Failed)
	}
}

// TestCancelledWorkflowRunsKeepsTheNewestRunPerWorkflow covers the re-run
// case. GitHub returns runs newest-first; once a cancelled workflow has been
// re-run, its skipped jobs are no longer cancellation artifacts, so the newer
// conclusion has to win.
func TestCancelledWorkflowRunsKeepsTheNewestRunPerWorkflow(t *testing.T) {
	t.Parallel()

	page := []byte(`[{"workflow_runs":[
		{"name":"Build Test","event":"pull_request","conclusion":null},
		{"name":"Build Test","event":"pull_request","conclusion":"cancelled"},
		{"name":"Frontend","event":"push","conclusion":"cancelled"},
		{"name":"Static Contract Gates","event":"pull_request","conclusion":"cancelled"}
	]}]`)
	runner := &routedRunner{runs: page}

	got, err := cancelledWorkflowRuns(context.Background(), runner, "eshu-hq/eshu", headSHAFixture)
	if err != nil {
		t.Fatalf("cancelledWorkflowRuns: %v", err)
	}
	if got["Build Test"] {
		t.Error("the newest Build Test run is a re-run that has not concluded; its skipped jobs are not cancellation artifacts")
	}
	if !got["Static Contract Gates"] {
		t.Error("a cancelled pull-request run must be reported cancelled")
	}
	if _, ok := got["Frontend"]; ok {
		t.Error("a push-event run does not own the pull-request checks this aggregate evaluates")
	}
}
