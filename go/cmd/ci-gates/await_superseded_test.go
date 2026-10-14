// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// Why this file exists (#8053).
//
// The required-gates aggregator reported a green PR event as red. It awaited
// the gate check-runs by name on the commit and picked a failed `differential`
// job from an event that was already superseded and cancelled:
//
//   - PR #8045 was closed and reopened twice in four minutes, so two
//     `pull_request` events started their own Golden Corpus Gate run on the
//     same head commit. The workflow's concurrency group cancelled the older
//     run when the newer event started.
//   - The older run's downstream `differential` job (`if: always()`) still ran
//     after its `needs:` capture jobs were cancelled, found no recordings to
//     stage, and recorded FAILURE on a cancelled run.
//   - The aggregate awaited check-runs by name with no event affinity and read
//     that stale FAILURE, publishing `required-gates-complete` as `failure`
//     while the newest event had no failed required gate.
//
// The fix attributes failed rollup rows to the newest workflow run for the
// commit before judging them: a failure from a superseded run is ignored in
// favour of the newest run's verdict, and a failure inside a cancelled newest
// run is infrastructure state (exit 13), not a gate failure (exit 10).

// supersededFixtureIDs mirrors the #8053 report: the superseded Golden Corpus
// Gate run, its check suite, the newest run, and its suite.
const (
	supersededRunID   = 38347291615
	supersededSuiteID = 115132602500
	newestRunID       = 38347574216
	newestSuiteID     = 115132602600
)

func supersededGate() []resolvedRequiredGate {
	return []resolvedRequiredGate{{
		WorkflowName: "Golden Corpus Gate",
		Job:          "differential nornicdb vs neo4j",
		GateIDs:      []string{"golden-corpus-differential"},
	}}
}

// TestEvaluateFailureInCancelledRunIsNotAGateResult is the #8053 classification:
// an `if: always()` downstream job that records FAILURE after its upstream
// jobs were cancelled is not a gate result when the workflow run that owns it
// concluded `cancelled`.
func TestEvaluateFailureInCancelledRunIsNotAGateResult(t *testing.T) {
	t.Parallel()

	required := supersededGate()
	checks := []checkRollup{{
		Name:     "differential nornicdb vs neo4j",
		Workflow: "Golden Corpus Gate",
		Event:    "pull_request",
		Bucket:   "fail",
		State:    "FAILURE",
	}}

	got := evaluateRequiredChecks(required, checks, runConclusions{"Golden Corpus Gate": "cancelled"})
	if len(got.Failed) != 0 {
		t.Fatalf("failed = %#v; a FAILURE inside a cancelled run records cancelled upstream, not a failed gate", got.Failed)
	}
	if len(got.Cancelled) != 1 {
		t.Fatalf("cancelled = %#v; want the differential gate as infrastructure state, not a verdict", got.Cancelled)
	}
}

// TestEvaluateNewestFailureStillFails is the seeded guard paired with the test
// above: the same FAILURE row under a newest run that was NOT cancelled must
// keep failing. Relaxing this turns real gate failures green, which is worse
// than the #8053 overclaim.
func TestEvaluateNewestFailureStillFails(t *testing.T) {
	t.Parallel()

	required := supersededGate()
	checks := []checkRollup{{
		Name:     "differential nornicdb vs neo4j",
		Workflow: "Golden Corpus Gate",
		Event:    "pull_request",
		Bucket:   "fail",
		State:    "FAILURE",
	}}

	for conclusion, wantFailed := range map[string]bool{
		"failure": true,
		"success": true,
		"":        true,
	} {
		got := evaluateRequiredChecks(required, checks, runConclusions{"Golden Corpus Gate": conclusion})
		if len(got.Failed) != 1 && wantFailed {
			t.Fatalf("conclusion=%q: failed = %#v; a FAILURE outside a cancelled run must stay a gate failure", conclusion, got.Failed)
		}
	}
	nilRuns := evaluateRequiredChecks(required, checks, nil)
	if len(nilRuns.Failed) != 1 {
		t.Fatalf("failed = %#v; an unreadable run conclusion must fail closed, never reclassify", nilRuns.Failed)
	}
}

// TestNeedsRunConclusionsOnFailedCheck pins the lookup trigger the #8053 rule
// needs: a FAILURE row must resolve the owning run's conclusion, while
// cancelled-only and all-green rollups still spend no lookup.
func TestNeedsRunConclusionsOnFailedCheck(t *testing.T) {
	t.Parallel()

	failed := []checkRollup{{
		Name: "differential nornicdb vs neo4j", Workflow: "Golden Corpus Gate",
		Event: "pull_request", Bucket: "fail", State: "FAILURE",
	}}
	if !needsRunConclusions(supersededGate(), failed) {
		t.Fatal("a failed gate must resolve its owning run's conclusion; without it a superseded failure cannot be told from a genuine one")
	}
	cancelled := []checkRollup{{
		Name: "differential nornicdb vs neo4j", Workflow: "Golden Corpus Gate",
		Event: "pull_request", Bucket: "cancel", State: "CANCELLED",
	}}
	if needsRunConclusions(supersededGate(), cancelled) {
		t.Fatal("a cancelled gate is terminal and self-describing; it must not cost a run lookup")
	}
	passed := []checkRollup{{
		Name: "differential nornicdb vs neo4j", Workflow: "Golden Corpus Gate",
		Event: "pull_request", Bucket: "pass", State: "SUCCESS",
	}}
	if needsRunConclusions(supersededGate(), passed) {
		t.Fatal("an all-green rollup must spend no run lookup")
	}
}

// supersededRunsJSON is the actions-runs page shape with the run ids and check
// suite ids the newest-run join needs: the superseded cancelled run and the
// newest run on one head commit.
func supersededRunsJSON(newestConclusion string) string {
	return `[{"workflow_runs":[
		{"id":` + itoa(supersededRunID) + `,"name":"Golden Corpus Gate","event":"pull_request","conclusion":"cancelled","check_suite_id":` + itoa(supersededSuiteID) + `},
		{"id":` + itoa(newestRunID) + `,"name":"Golden Corpus Gate","event":"pull_request","conclusion":` + newestConclusionJSON(newestConclusion) + `,"check_suite_id":` + itoa(newestSuiteID) + `}
	]}]`
}

func newestConclusionJSON(conclusion string) string {
	if conclusion == "" {
		return "null"
	}
	return `"` + conclusion + `"`
}

// supersededCheckRunsJSON carries one differential check run per suite: the
// superseded run's FAILURE (its upstream capture jobs were cancelled, leaving
// nothing to stage) and the newest run's verdict, which varies per test.
func supersededCheckRunsJSON(newestConclusion string) string {
	return `[{"check_runs":[
		{"name":"differential capture (pair 0, nornicdb)","status":"completed","conclusion":"cancelled","check_suite":{"id":` + itoa(supersededSuiteID) + `}},
		{"name":"differential nornicdb vs neo4j","status":"completed","conclusion":"failure","check_suite":{"id":` + itoa(supersededSuiteID) + `}},
		{"name":"differential nornicdb vs neo4j","status":"completed","conclusion":` + newestConclusionJSON(newestConclusion) + `,"check_suite":{"id":` + itoa(newestSuiteID) + `}}
	]}]`
}

// supersededRollupJSON is what `gh pr checks` served during the incident: by
// name, with no event affinity, it surfaced the superseded run's FAILURE
// because the newest run had produced no differential check run yet.
func supersededRollupJSON() string {
	return `[{"name":"differential nornicdb vs neo4j","state":"FAILURE","bucket":"fail","workflow":"Golden Corpus Gate","event":"pull_request"}]`
}

// TestReadEventChecksKeepsOnlyTheNewestPullRequestRun pins the join the
// aggregate was missing: check runs are judged by the newest workflow run per
// workflow name (by run id), and rows from a superseded attempt never own the
// name -- the same rule the merge-group reader already applies.
func TestReadEventChecksKeepsOnlyTheNewestPullRequestRun(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{
		"actions/runs?": supersededRunsJSON("success"),
		"commits/" + headSHAFixture + "/check-runs": supersededCheckRunsJSON("success"),
	}}

	got, err := readEventChecks(context.Background(), runner, "eshu-hq/eshu", headSHAFixture, eventPullRequest)
	if err != nil {
		t.Fatalf("readEventChecks: %v", err)
	}
	want := []checkRollup{{
		Name:     "differential nornicdb vs neo4j",
		State:    "SUCCESS",
		Bucket:   "pass",
		Workflow: "Golden Corpus Gate",
		Event:    "pull_request",
		RunID:    newestRunID,
	}}
	if len(got) != 1 {
		t.Fatalf("checks = %#v; want only the newest run's differential row, the superseded FAILURE must not survive", got)
	}
	if got[0] != want[0] {
		t.Fatalf("checks = %#v; want %#v", got, want)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "actions/runs?") && !strings.Contains(call, "event=pull_request") {
			t.Errorf("run lookup %q must filter to pull_request runs", call)
		}
		if !strings.Contains(call, "--paginate") || !strings.Contains(call, "--slurp") {
			t.Errorf("call %q must paginate and slurp; the decoders read arrays of pages", call)
		}
	}
}

// TestAwaitSupersededFailureWithNewestSuccessPasses is the first #8053
// acceptance criterion end to end: the older event's differential is FAILURE
// after its upstream was cancelled, the newest event's is SUCCESS, and the
// aggregate must yield success. The pre-fix code yields failure: it awaits by
// name and reads the superseded FAILURE straight from the rollup.
func TestAwaitSupersededFailureWithNewestSuccessPasses(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{
		"pr checks":     supersededRollupJSON(),
		"actions/runs?": supersededRunsJSON("success"),
		"commits/" + headSHAFixture + "/check-runs": supersededCheckRunsJSON("success"),
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 8045, headSHAFixture, supersededGate(), 10*time.Millisecond, io_Discard{}); err != nil {
		t.Fatalf("awaitPRRequiredChecks = %v; the FAILURE belongs to the superseded event, the newest event is green", err)
	}
}

// TestAwaitNewestFailureStillFailsWithRunID is the second acceptance criterion
// plus the fourth: the same fixture with the newest event's job failed must
// still yield failure, and the log line must name the workflow run id of the
// check run that was judged.
func TestAwaitNewestFailureStillFailsWithRunID(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{
		"pr checks":     supersededRollupJSON(),
		"actions/runs?": supersededRunsJSON("failure"),
		"commits/" + headSHAFixture + "/check-runs": supersededCheckRunsJSON("failure"),
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 8045, headSHAFixture, supersededGate(), 10*time.Millisecond, io_Discard{})
	if !errors.Is(err, errGateFailed) {
		t.Fatalf("err = %v; a FAILURE in the newest run is a genuine gate failure", err)
	}
	if want := itoa(newestRunID); !strings.Contains(err.Error(), want) {
		t.Fatalf("failure message %q does not name the judged workflow run id %s", err.Error(), want)
	}
	code := classifyAwaitOutcome(err).exitCode()
	if state, _, published := publishedRequiredStatus(t, code); !published || state != "failure" {
		t.Fatalf("a genuine newest-run failure published state=%q published=%v; want failure", state, published)
	}
}

// TestAwaitStaleFailureWhileNewestRunIsInFlightKeepsWaiting reproduces the
// incident minute by minute: at aggregate time the newest event's run had not
// produced its differential check run yet, so the rollup carried only the
// superseded FAILURE. Judging that row red published failure on a head whose
// newest event was still running; the honest verdict is to keep waiting, so a
// later aggregate reads the newest check runs.
func TestAwaitStaleFailureWhileNewestRunIsInFlightKeepsWaiting(t *testing.T) {
	t.Parallel()

	runner := &endpointRunner{routes: map[string]string{
		"pr checks":     supersededRollupJSON(),
		"actions/runs?": supersededRunsJSON(""),
		"commits/" + headSHAFixture + "/check-runs": `[{"check_runs":[
			{"name":"differential capture (pair 0, nornicdb)","status":"completed","conclusion":"cancelled","check_suite":{"id":` + itoa(supersededSuiteID) + `}},
			{"name":"differential nornicdb vs neo4j","status":"completed","conclusion":"failure","check_suite":{"id":` + itoa(supersededSuiteID) + `}},
			{"name":"differential capture (pair 0, nornicdb)","status":"in_progress","conclusion":null,"check_suite":{"id":` + itoa(newestSuiteID) + `}}
		]}]`,
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := awaitPRRequiredChecks(ctx, runner, "eshu-hq/eshu", 8045, headSHAFixture, supersededGate(), 10*time.Millisecond, io_Discard{})
	if !errors.Is(err, errStillRunning) {
		t.Fatalf("err = %v; the only FAILURE belongs to the superseded event and the newest run has no verdict yet", err)
	}
	if code := classifyAwaitOutcome(err).exitCode(); code != awaitExitStillRunning {
		t.Fatalf("exit %d; waiting publishes nothing so the next aggregate reads the newest check runs", code)
	}
}
