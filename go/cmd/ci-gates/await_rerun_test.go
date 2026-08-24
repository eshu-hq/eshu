// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"
)

// Why this file exists (#6189, third round).
//
// The re-run repair the cancelled-gate description tells an operator to
// perform -- `gh run rerun` on the cancelled workflow -- has a window in it.
// Between the moment the replacement run starts and the moment its check runs
// replace the old ones in the rollup, the aggregate reads a rollup that still
// carries the cancelled run's SKIPPED row and a run list whose newest entry has
// not concluded. required-gates.yml triggers this aggregate on `in_progress`,
// so that window is not theoretical: it is reachable on the documented repair
// path.
//
// Reading "has not concluded" as "not cancelled" turns that window into an
// immediate `A required gate failed` on a head where nothing failed -- the
// #6189 overclaim, reintroduced by the fix for it. The aggregate has to keep
// waiting instead.

// TestAwaitSkipWaitsWhileTheReplacementRunIsInFlight pins that window.
//
// The fixture is the exact re-run shape: GitHub returns runs newest-first, so
// the in-flight replacement (`conclusion: null`) comes before the cancelled run
// it replaced, while the check rollup still reports the old run's SKIPPED job.
func TestAwaitSkipWaitsWhileTheReplacementRunIsInFlight(t *testing.T) {
	t.Parallel()

	required, checks := skippedGateFixture()
	runner := &routedRunner{
		checks: rollupJSON(t, checks),
		runs: []byte(`[{"workflow_runs":[
			{"name":"Build Test","event":"pull_request","conclusion":null},
			{"name":"Build Test","event":"pull_request","conclusion":"cancelled"}
		]}]`),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{})
	if err == nil {
		t.Fatal("a gate whose replacement run has not concluded has not passed")
	}
	code := classifyAwaitOutcome(err).exitCode()
	state, description, published := publishedRequiredStatus(t, code)
	if code == awaitExitGateFailed {
		t.Fatalf("a skipped gate whose replacement run is still executing published state=%q description=%q (exit %d); "+
			"no gate failed -- the replacement's checks have not landed in the rollup yet",
			state, description, code)
	}
	if code != awaitExitStillRunning {
		t.Fatalf("exit %d (state=%q description=%q); an in-flight replacement run is still running, so the aggregate must keep waiting: "+
			"the run's own completion re-triggers this workflow with a rollup that can be decided",
			code, state, description)
	}
	if published {
		t.Fatalf("exit %d published state=%q; gates that have not finished must not publish a terminal status", code, state)
	}
}

// TestAwaitSkipStillFailsClosedWhenNoRunIsKnown keeps the waiting rule narrow.
// "The owning run has not concluded" is the only shape that earns more waiting.
// A workflow with no run on this head at all is unknown, not in flight, and a
// SKIPPED gate under it must keep failing closed exactly as it did before.
func TestAwaitSkipStillFailsClosedWhenNoRunIsKnown(t *testing.T) {
	t.Parallel()

	required, checks := skippedGateFixture()
	runner := &routedRunner{
		checks: rollupJSON(t, checks),
		// A real page, but nothing in it owns the "Build Test" checks.
		runs: workflowRunsJSON(t, map[string]string{"Static Contract Gates": "cancelled"}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 6186, headSHAFixture, required, 10*time.Millisecond, io_Discard{})
	if err == nil {
		t.Fatal("a skipped gate with no known owning run must not be reported as a clean pass")
	}
	code := classifyAwaitOutcome(err).exitCode()
	state, description, published := publishedRequiredStatus(t, code)
	if !published || state != "failure" {
		t.Fatalf("an unknown owning run published state=%q description=%q published=%v; want failure (fail closed)",
			state, description, published)
	}
}
