// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Why this file exists (#6189, fourth door).
//
// The three shapes await_notrun.go already handles all arrive as a check
// rollup ROW: CANCELLED, STALE, or SKIPPED. There is a fourth way the same
// cancellation reaches the aggregate, and it arrives as no row at all. Cancel
// a workflow run before GitHub creates the job, and the check run for that job
// never exists -- so `gh pr checks` reports nothing for it, the gate resolves
// to zero matches, and it is filed MISSING.
//
// MISSING went to Pending, which exits 11, which publishes NOTHING. The status
// stays on the `pending` the first step wrote, forever, on every subsequent
// aggregate for that head: a blocked pull request with no red check anywhere
// to act on. That is the same operator dead end #6189 exists to remove.
//
// The constraint that decides the fix is the one that governed the SKIPPED
// half: a check missing for ANY OTHER REASON must keep failing closed. A gate
// the registry selected whose job simply never ran is a real disagreement
// between the registry and the workflow, and it must not become "probably
// fine". The only thing that separates the two is the conclusion of the
// workflow run that owns the job -- the same signal, and the same lookup, the
// SKIPPED half already uses.

// missingGateFixture is one selected gate with an EMPTY rollup: the job's
// check run was never created, so nothing in `gh pr checks` refers to it.
func missingGateFixture() ([]resolvedRequiredGate, []checkRollup) {
	return []resolvedRequiredGate{{
		WorkflowName: "Static Contract Gates",
		Job:          "Verify docs-refs gate",
		GateIDs:      []string{"docs-refs"},
	}}, nil
}

// TestAwaitGenuinelyMissingCheckNeverPasses is the guard that matters more
// than the fix, and it passes both before and after it.
//
// Every reason a check can be missing OTHER than its owning run being
// cancelled must keep the aggregate from concluding. A run that finished
// without ever producing the job, a workflow that never ran on this head at
// all, and a lookup that failed are all cases where the honest answer is "this
// gate has not reported", not "this gate is fine". Turning any of them into a
// pass would put a silent hole in the status that guards every merge.
func TestAwaitGenuinelyMissingCheckNeverPasses(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		runs    map[string]string
		runsErr error
	}{
		"the owning run finished without ever creating the job": {
			runs: map[string]string{"Static Contract Gates": "success"},
		},
		"the owning run concluded failure": {
			runs: map[string]string{"Static Contract Gates": "failure"},
		},
		"no run for that workflow exists on this head": {
			runs: map[string]string{"Build Test": "cancelled"},
		},
		"the run lookup itself failed": {
			runsErr: errors.New("api rate limit exceeded"),
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			required, checks := missingGateFixture()
			runner := &routedRunner{checks: rollupJSON(t, checks), runsErr: tc.runsErr}
			if tc.runs != nil {
				runner.runs = workflowRunsJSON(t, tc.runs)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()

			err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{})
			if err == nil {
				t.Fatal("a gate whose check never reported must not be a clean pass; that is a silent hole in the merge gate")
			}
			code := classifyAwaitOutcome(err).exitCode()
			if code != awaitExitStillRunning {
				t.Fatalf("exit %d; a check missing for any reason other than a cancelled owning run must keep "+
					"the aggregate waiting (exit %d), never resolve", code, awaitExitStillRunning)
			}
			if state, description, published := publishedRequiredStatus(t, code); published || state == "success" {
				t.Fatalf("exit %d published state=%q description=%q; nothing may publish success here",
					code, state, description)
			}
		})
	}
}

// TestAwaitMissingCheckFromACancelledRunDoesNotStrandTheStatus is the defect.
// The run that owned the job was cancelled before the job was created, so the
// check run does not exist and never will. Waiting for it cannot terminate.
func TestAwaitMissingCheckFromACancelledRunDoesNotStrandTheStatus(t *testing.T) {
	t.Parallel()

	required, checks := missingGateFixture()
	runner := &routedRunner{
		checks: rollupJSON(t, checks),
		runs:   workflowRunsJSON(t, map[string]string{"Static Contract Gates": "cancelled"}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{})
	if err == nil {
		t.Fatal("a gate whose run was cancelled is not a pass")
	}
	code := classifyAwaitOutcome(err).exitCode()
	if code != awaitExitGateCancelled {
		t.Fatalf("exit %d, want %d; a job whose run was cancelled before the job existed is the same "+
			"cancellation as a CANCELLED check, and must reach the same terminal answer", code, awaitExitGateCancelled)
	}
	state, description, published := publishedRequiredStatus(t, code)
	if !published {
		t.Fatal("exit code published nothing; the status would stay pending forever, which is the defect")
	}
	if state != "error" {
		t.Fatalf("published state=%q description=%q; want error -- it blocks the merge and names the re-run",
			state, description)
	}
	if !strings.Contains(strings.ToLower(description), "cancel") {
		t.Fatalf("published description %q does not tell the operator a gate was cancelled", description)
	}
}

// TestAwaitMissingCheckFromACancelledRunResolvesOnTheFirstPoll is the
// termination proof, and it is why the answer is a verdict rather than a
// longer wait. The re-await design was rejected earlier in this PR precisely
// because polling a terminal state cannot terminate: the check run does not
// exist, nothing will create it, and waiting would burn the full timeout and
// then publish nothing at all.
func TestAwaitMissingCheckFromACancelledRunResolvesOnTheFirstPoll(t *testing.T) {
	t.Parallel()

	required, checks := missingGateFixture()
	runner := &routedRunner{
		checks: rollupJSON(t, checks),
		runs:   workflowRunsJSON(t, map[string]string{"Static Contract Gates": "cancelled"}),
	}
	// Deliberately generous next to the 10ms poll: if the verdict needed the
	// wait to expire, this would take the full two seconds and the elapsed
	// check below would catch it.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	started := time.Now()
	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{})
	elapsed := time.Since(started)

	if !errors.Is(err, errGateCancelled) {
		t.Fatalf("err = %v; want the cancelled-gate outcome", err)
	}
	if runner.checksCalls != 1 {
		t.Fatalf("read the check rollup %d time(s); the verdict is available on the first poll and waiting "+
			"for a check run that will never be created cannot terminate", runner.checksCalls)
	}
	if elapsed > time.Second {
		t.Fatalf("took %v of a 2s budget; the answer must not depend on the wait expiring", elapsed)
	}
}

// TestAwaitMissingCheckWaitsWhileTheOwningRunIsStillGoing keeps the honest
// wait reachable. A run still executing may yet create the job, so neither
// terminal answer is available: "cancelled" would publish `error` against a
// run that may pass, and concluding anything else would judge a gate that has
// not reported. This wait terminates on its own -- the run's completion
// re-triggers the aggregate with a rollup that can be decided.
func TestAwaitMissingCheckWaitsWhileTheOwningRunIsStillGoing(t *testing.T) {
	t.Parallel()

	required, checks := missingGateFixture()
	runner := &routedRunner{
		checks: rollupJSON(t, checks),
		runs:   workflowRunsJSON(t, map[string]string{"Static Contract Gates": ""}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{})
	if code := classifyAwaitOutcome(err).exitCode(); code != awaitExitStillRunning {
		t.Fatalf("exit %d, want %d; a run still executing may still create the job", code, awaitExitStillRunning)
	}
}

// TestAwaitStaleGateDoesNotDeferTheMissingGateLookup pins a trap the rest of
// this file would not catch. gh sends STALE through its bucket switch's
// default arm, so a stale check arrives bucketed "pending" while being
// completely terminal (cli/cli v2.97.0 pkg/cmd/pr/checks/aggregate.go) -- the
// same trap isStaleCheck already documents.
//
// The lookup that resolves a missing gate is deferred while a selected check
// is genuinely still running, because until then the aggregate waits either
// way and the call would be spent for nothing. Reading a STALE row as "still
// running" would defer it forever, and the head would strand on pending with
// the fix in place.
func TestAwaitStaleGateDoesNotDeferTheMissingGateLookup(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{
		{WorkflowName: "Build Test", Job: "go-core", GateIDs: []string{"go-core"}},
		{WorkflowName: "Static Contract Gates", Job: "Verify docs-refs gate", GateIDs: []string{"docs-refs"}},
	}
	checks := []checkRollup{{
		Name: "go-core", Workflow: "Build Test", Event: "pull_request", Bucket: "pending", State: "STALE",
	}}
	runner := &routedRunner{
		checks: rollupJSON(t, checks),
		runs:   workflowRunsJSON(t, map[string]string{"Static Contract Gates": "cancelled"}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{})
	if code := classifyAwaitOutcome(err).exitCode(); code != awaitExitGateCancelled {
		t.Fatalf("exit %d, want %d; a STALE row is terminal and must not hold the missing-gate lookup off",
			code, awaitExitGateCancelled)
	}
}

// TestAwaitDoesNotReadRunConclusionsWhileASelectedGateIsStillRunning is the
// cost guard for the new path, and the reason the lookup is not simply made
// whenever a gate is missing.
//
// Early in a head's life most selected gates have no rollup row yet, so an
// unconditional missing-gate lookup would fire on nearly every poll of nearly
// every aggregate. It is deferred while any selected check is genuinely
// running because the outcome cannot change until those finish: the aggregate
// waits either way, so the call would buy nothing.
func TestAwaitDoesNotReadRunConclusionsWhileASelectedGateIsStillRunning(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{
		{WorkflowName: "Build Test", Job: "go-core", GateIDs: []string{"go-core"}},
		{WorkflowName: "Static Contract Gates", Job: "Verify docs-refs gate", GateIDs: []string{"docs-refs"}},
	}
	checks := []checkRollup{{
		Name: "go-core", Workflow: "Build Test", Event: "pull_request", Bucket: "pending", State: "IN_PROGRESS",
	}}
	runner := &routedRunner{
		checks: rollupJSON(t, checks),
		runs:   workflowRunsJSON(t, map[string]string{"Static Contract Gates": "cancelled"}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{})
	if runner.runsCalls != 0 {
		t.Errorf("run conclusions were read %d time(s) while a selected gate was still running; the aggregate "+
			"waits either way, so that call is spent for nothing on the common early-poll path", runner.runsCalls)
	}
}

// TestAwaitDoesNotReadRunConclusionsWhenEveryGateReported is the other half of
// the cost guard: the healthy path, where every selected gate has a row and
// passed, must spend nothing extra at all.
func TestAwaitDoesNotReadRunConclusionsWhenEveryGateReported(t *testing.T) {
	t.Parallel()

	required := []resolvedRequiredGate{{
		WorkflowName: "Build Test", Job: "go-core", GateIDs: []string{"go-core"},
	}}
	checks := []checkRollup{{
		Name: "go-core", Workflow: "Build Test", Event: "pull_request", Bucket: "pass", State: "SUCCESS",
	}}
	runner := &routedRunner{
		checks: rollupJSON(t, checks),
		runs:   workflowRunsJSON(t, map[string]string{"Build Test": "cancelled"}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{}); err != nil {
		t.Fatalf("every selected gate passed, got %v", err)
	}
	if runner.runsCalls != 0 {
		t.Errorf("run conclusions were read %d time(s) on an all-green rollup; the lookup must stay off the "+
			"common path", runner.runsCalls)
	}
}
