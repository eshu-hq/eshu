// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Failing-first contract for eshu-hq/eshu#10972 Decision-3: the unstamped
// terminal-to-pending re-drive feeding advisory_group_projection and
// workload_materialization (1277/1409 = 90.6% of reopened inflow in the
// Decision-3 window; only the full unstamped class flips I2's net negative).
//
// Mechanism (live QA read-only proof, 2026-11-27): 826 advisory rows
// retouched in 15 min, all in the synthetic:advisory-groups slot, all
// carrying group_id + origin_scope_id + origin_generation_id and zero
// carrying fanout_origin; origin scopes are live supply_chain_impact batch
// passes. The writer is RetouchWorkItem
// (retouchReducerWorkItemQuery), called once per touched group per
// committed sci pass. The workload twin is the unfenced cross-scope replay
// (scheduleCrossScopeMaterializationReplayQuery) fired by the
// repo-dependency acceptance cycle after every write cycle, plus its fenced
// sibling.
//
// Why the re-drive starves the lane instead of just churning it: the
// reducer claim locks candidates by
// `reclaim_rank ASC, updated_at ASC, work_item_id ASC` and derives
// CycleStartedAt as COALESCE(reopened_at, created_at). The retouch sets
// `updated_at = $1` and `reopened_at = $1` unconditionally, so every sci
// touch moves a hot advisory row to the back of the fair-rank queue AND
// advances the winners-watermark bound it must wait for. 665 reopens vs 45
// completions per 20 min follows directly.
//
// The fix these tests pin (landing lane implements it):
//   - retouch: terminal rows (succeeded/failed/dead_letter) keep the full
//     succeeded shape (fresh updated_at + reopened_at: a new repair
//     cycle); already-outstanding rows keep updated_at (fair-rank position
//     preserved) while still advancing reopened_at (the bound must cover
//     the later touch, per #10411).
//   - fenced workload replay: a succeeded row reopens only when the new
//     fence differs from the stored one; the repo-dependency acceptance
//     cycle moves to fenced requests. Identical fence means identical
//     repo-dependency input-set fences, so the succeeded verdict already covers it.
//     Deployment-evidence triggers keep the unfenced path.
//
// Both writers evaluate one CASE on the locked row version, so the fix
// adds no lock, no ordering change, and no lease touch: concurrent
// re-drives still decide on the newest status (idempotent, as today).
func squashSQL(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Test10972RetouchKeepsQueuePositionForOutstandingRows is RED while the
// retouch rewrites updated_at on rows that were already outstanding: every
// touch requeues a hot group behind the whole fair-rank backlog.
func Test10972RetouchKeepsQueuePositionForOutstandingRows(t *testing.T) {
	t.Parallel()
	got := squashSQL(retouchReducerWorkItemQuery)
	const want = "updated_at = CASE WHEN status IN ('succeeded', 'failed', 'dead_letter') THEN $1 ELSE updated_at END"
	if !strings.Contains(got, squashSQL(want)) {
		t.Fatalf("retouch advances updated_at on already-outstanding rows " +
			"(requeue-to-back under ORDER BY reclaim_rank, updated_at):\nmissing: " + want)
	}
	const bound = "reopened_at = $1"
	if !strings.Contains(got, bound) {
		t.Fatalf("retouch must keep advancing reopened_at (%q) so the "+
			"winners-watermark bound covers the later touch", bound)
	}
}

// Test10972FencedReplayReopensSucceededOnlyOnFenceChange is RED while the
// fenced workload replay flips every succeeded row to pending even when
// the new fence equals the stored one (identical repo-dependency input-set fences, verdict
// already published).
func Test10972FencedReplayReopensSucceededOnlyOnFenceChange(t *testing.T) {
	t.Parallel()
	got := squashSQL(scheduleWorkloadMaterializationFencedReplayQuery)
	const want = "WHEN status = 'succeeded' " +
		"AND COALESCE(payload->>'repo_dependency_readiness_fence', '') <> $3 " +
		"THEN 'pending'"
	if !strings.Contains(got, squashSQL(want)) {
		t.Fatalf("fenced replay reopens succeeded rows with an unchanged " +
			"fence (no-change repo-dependency cycles ping-pong the workload row):\nmissing: " + want)
	}
}

// fencedReplayReasonOK reports whether q stamps last_requeue_reason exactly
// on the fenced-replay transitions: a succeeded row reopened under a fence
// change, or a claimed or running row newly flagged under a fence change,
// takes 'cross_scope_replay'; every other row keeps its prior reason. The
// positive pin and the seeded-violation check share it, so the pin is proved
// to bite rather than merely to pass on the shipped text.
func fencedReplayReasonOK(q string) bool {
	got := squashSQL(q)
	const succeededArm = "WHEN status = 'succeeded' " +
		"AND COALESCE(payload->>'repo_dependency_readiness_fence', '') <> $3 " +
		"THEN 'cross_scope_replay'"
	const flagArm = "WHEN status IN ('claimed', 'running') " +
		"AND NOT cross_scope_replay_required " +
		"AND COALESCE(payload->>'repo_dependency_readiness_fence', '') <> $3 " +
		"THEN 'cross_scope_replay'"
	const elseArm = "ELSE last_requeue_reason"
	return strings.Contains(got, squashSQL(succeededArm)) &&
		strings.Contains(got, squashSQL(flagArm)) &&
		strings.Contains(got, elseArm)
}

// fencedReplayReturningOK reports whether q counts exactly the fenced-replay
// transitions: a succeeded row reopened under a fence change, or a claimed or
// running row newly flagged under a fence change. The RETURNING clause must
// name the transition boolean requeued (never a matched count), so the
// per-reason counter sees transitions only.
func fencedReplayReturningOK(q string) bool {
	got := squashSQL(q)
	const succeededCount = "(target.prior_status = 'succeeded' " +
		"AND target.prior_fence <> $3)"
	const flagCount = "(target.prior_status IN ('claimed', 'running') " +
		"AND NOT target.prior_flag " +
		"AND target.prior_fence <> $3)"
	return strings.Contains(got, squashSQL(succeededCount)) &&
		strings.Contains(got, squashSQL(flagCount)) &&
		strings.Contains(got, "AS requeued")
}

// Test10972FencedReplayStampsReasonOnlyOnFenceDiffTransition pins the #11615
// reason arm on the merged fenced replay: the succeeded reopen and the
// claimed/running flag write the reason only under a fence change, and a row
// that sees no transition keeps its prior reason.
func Test10972FencedReplayStampsReasonOnlyOnFenceDiffTransition(t *testing.T) {
	t.Parallel()
	if !fencedReplayReasonOK(scheduleWorkloadMaterializationFencedReplayQuery) {
		t.Fatal("fenced replay must stamp last_requeue_reason exactly on " +
			"fence-change transitions and preserve the prior reason otherwise")
	}
}

// Test10972FencedReplayReturningCountsTransitionsOnly pins the merged fenced
// replay tail: the RETURNING boolean is true exactly for fence-change
// transitions, so the counter never counts a merely matched row.
func Test10972FencedReplayReturningCountsTransitionsOnly(t *testing.T) {
	t.Parallel()
	if !fencedReplayReturningOK(scheduleWorkloadMaterializationFencedReplayQuery) {
		t.Fatal("fenced replay must report its transition boolean AS requeued, " +
			"gated on the fence change for both the succeeded and the flagged arms")
	}
}

// Test10972FencedReplayPinsBiteOnSeededViolations is the proof-item-2
// RED/GREEN pair for the two pins above: each seeded weakening (an ungated
// reason arm, a stamped ELSE, an ungated succeeded count, a matched-count
// tail) fails its pin while the shipped query passes both.
func Test10972FencedReplayPinsBiteOnSeededViolations(t *testing.T) {
	t.Parallel()
	base := scheduleWorkloadMaterializationFencedReplayQuery
	if !fencedReplayReasonOK(base) || !fencedReplayReturningOK(base) {
		t.Fatal("shipped fenced replay query must pass both pins before the seeded violations run")
	}
	mutate := func(name, old, new string) string {
		if !strings.Contains(base, old) {
			t.Fatalf("seed %s: anchor missing from shipped query, the violation would be vacuous", name)
		}
		return strings.Replace(base, old, new, 1)
	}
	seeds := []struct {
		name string
		bad  string
		pin  func(string) bool
	}{
		{
			name: "succeeded reason arm without the fence change",
			bad: mutate("ungated-reason",
				"WHEN status = 'succeeded' AND COALESCE(payload->>'repo_dependency_readiness_fence', '') <> $3 THEN 'cross_scope_replay'",
				"WHEN status = 'succeeded' THEN 'cross_scope_replay'"),
			pin: fencedReplayReasonOK,
		},
		{
			name: "ELSE stamps instead of preserving the prior reason",
			bad: mutate("stamped-else",
				"ELSE last_requeue_reason",
				"ELSE 'cross_scope_replay'"),
			pin: fencedReplayReasonOK,
		},
		{
			name: "succeeded count without the fence change",
			bad: mutate("ungated-count",
				"(target.prior_status = 'succeeded' AND target.prior_fence <> $3)",
				"(target.prior_status = 'succeeded')"),
			pin: fencedReplayReturningOK,
		},
		{
			name: "matched-count tail instead of the transition boolean",
			bad: mutate("matched-tail",
				"AS requeued",
				"AS matched"),
			pin: fencedReplayReturningOK,
		},
	}
	for _, seed := range seeds {
		if seed.pin(seed.bad) {
			t.Fatalf("pin let seeded violation through: %s", seed.name)
		}
	}
}

// digestReplayReasonOK reports whether q stamps last_requeue_reason exactly
// on the digest-fenced replay transitions: a succeeded row reopened under a
// changed digest, or a claimed or running row newly flagged, takes
// 'cross_scope_replay'; every other row keeps its prior reason. The digest
// gate lives in the target CTE WHERE, never in the reason arms, so a quiet
// rerun selects nothing and stamps nothing. The positive pin and the
// seeded-violation check share it, so the pin is proved to bite rather than
// merely to pass on the shipped text.
func digestReplayReasonOK(q string) bool {
	got := squashSQL(q)
	const succeededArm = "WHEN work.status IN ('succeeded') THEN 'cross_scope_replay'"
	const flagArm = "WHEN work.status IN ('claimed', 'running') AND NOT work.cross_scope_replay_required THEN 'cross_scope_replay'"
	const elseArm = "ELSE work.last_requeue_reason"
	if !(strings.Contains(got, squashSQL(succeededArm)) &&
		strings.Contains(got, squashSQL(flagArm)) &&
		strings.Contains(got, elseArm)) {
		return false
	}
	// Neither reason arm may carry the digest predicate: the gate filters in
	// the CTE WHERE, so slice the reason CASE and prove it is digest-free.
	start := strings.Index(got, "last_requeue_reason = CASE")
	end := strings.Index(got, "cross_scope_replay_required = CASE")
	if start < 0 || end < 0 || end <= start {
		return false
	}
	return !strings.Contains(got[start:end], "deployment_evidence_digest")
}

// digestReplayReturningOK reports whether q counts exactly the digest-fenced
// replay transitions: a succeeded row reopened under a changed digest, or a
// claimed or running row newly flagged. The RETURNING clause must name the
// transition boolean requeued (never a matched count), so the per-reason
// counter sees transitions only.
func digestReplayReturningOK(q string) bool {
	got := squashSQL(q)
	const succeededCount = "(target.prior_status = 'succeeded'"
	const flagCount = "(target.prior_status IN ('claimed', 'running') AND NOT target.prior_flag)"
	return strings.Contains(got, squashSQL(succeededCount)) &&
		strings.Contains(got, squashSQL(flagCount)) &&
		strings.Contains(got, "AS requeued")
}

// digestReplayGateOK reports whether the digest gate filters in the target
// CTE WHERE: a succeeded row is selected only under a changed digest, so a
// quiet rerun matches nothing, locks nothing, and stamps nothing. The CTE
// takes the same FOR NO KEY UPDATE row lock the single-table UPDATE took.
func digestReplayGateOK(q string) bool {
	got := squashSQL(q)
	const gate = "status <> 'succeeded' OR COALESCE(payload->>'deployment_evidence_digest', '') <> $3 OR $3 = ''"
	return strings.Contains(got, squashSQL(gate)) &&
		strings.Contains(got, "FOR NO KEY UPDATE")
}

// Test10972DigestReplayStampsReasonOnlyOnTransition pins the #11615 reason
// arm on the digest-fenced twin: the succeeded reopen and the claimed/running
// flag write the reason with no digest predicate in either arm (the gate
// filters in the CTE WHERE), and a row that sees no transition keeps its
// prior reason.
func Test10972DigestReplayStampsReasonOnlyOnTransition(t *testing.T) {
	t.Parallel()
	if !digestReplayReasonOK(scheduleWorkloadMaterializationDigestReplayQuery) {
		t.Fatal("digest replay must stamp last_requeue_reason exactly on transitions and preserve the prior reason otherwise, with no digest predicate in either reason arm")
	}
}

// Test10972DigestReplayReturningCountsTransitionsOnly pins the digest-fenced
// twin tail: the RETURNING boolean is true exactly for transitions, so the
// counter never counts a merely matched row.
func Test10972DigestReplayReturningCountsTransitionsOnly(t *testing.T) {
	t.Parallel()
	if !digestReplayReturningOK(scheduleWorkloadMaterializationDigestReplayQuery) {
		t.Fatal("digest replay must report its transition boolean AS requeued")
	}
}

// Test10972DigestReplayGateFiltersInTargetCTE pins the digest gate in the
// target CTE WHERE: a succeeded row carrying the incoming digest is never
// selected, so a quiet rerun matches nothing and moves nothing.
func Test10972DigestReplayGateFiltersInTargetCTE(t *testing.T) {
	t.Parallel()
	if !digestReplayGateOK(scheduleWorkloadMaterializationDigestReplayQuery) {
		t.Fatal("digest replay must gate succeeded rows on the digest change in the target CTE WHERE")
	}
}

// Test10972DigestReplayPinsBiteOnSeededViolations is the RED/GREEN pair for
// the three digest pins: each seeded weakening (a digest predicate in a
// reason arm, a stamped ELSE, an ungated RETURNING arm, a matched-count
// tail, a gate that admits unchanged succeeded rows, a lock-class change)
// fails its pin while the shipped query passes all three.
func Test10972DigestReplayPinsBiteOnSeededViolations(t *testing.T) {
	t.Parallel()
	base := scheduleWorkloadMaterializationDigestReplayQuery
	if !digestReplayReasonOK(base) || !digestReplayReturningOK(base) || !digestReplayGateOK(base) {
		t.Fatal("shipped digest replay query must pass all three pins before the seeded violations run")
	}
	mutate := func(name, old, new string) string {
		if !strings.Contains(base, old) {
			t.Fatalf("seed %s: anchor missing from shipped query, the violation would be vacuous", name)
		}
		return strings.Replace(base, old, new, 1)
	}
	seeds := []struct {
		name string
		bad  string
		pin  func(string) bool
	}{
		{
			name: "succeeded reason arm with a digest predicate",
			bad: mutate("digest-gated-reason",
				"WHEN work.status IN ('succeeded') THEN 'cross_scope_replay'",
				"WHEN work.status IN ('succeeded') AND COALESCE(payload->>'deployment_evidence_digest', '') <> $3 THEN 'cross_scope_replay'"),
			pin: digestReplayReasonOK,
		},
		{
			name: "ELSE stamps instead of preserving the prior reason",
			bad: mutate("stamped-else",
				"ELSE work.last_requeue_reason",
				"ELSE 'cross_scope_replay'"),
			pin: digestReplayReasonOK,
		},
		{
			name: "flagged rows counted without the newly-flagged guard",
			bad: mutate("ungated-count",
				"AND NOT target.prior_flag",
				""),
			pin: digestReplayReturningOK,
		},
		{
			name: "matched-count tail instead of the transition boolean",
			bad: mutate("matched-tail",
				"AS requeued",
				"AS matched"),
			pin: digestReplayReturningOK,
		},
		{
			name: "gate admits unchanged succeeded rows",
			bad: mutate("ungated-gate",
				"        OR COALESCE(payload->>'deployment_evidence_digest', '') <> $3\n",
				""),
			pin: digestReplayGateOK,
		},
		{
			name: "target CTE without the row lock",
			bad: mutate("unlocked-target",
				"FOR NO KEY UPDATE",
				"FOR UPDATE"),
			pin: digestReplayGateOK,
		},
	}
	for _, seed := range seeds {
		if seed.pin(seed.bad) {
			t.Fatalf("pin let seeded violation through: %s", seed.name)
		}
	}
}

// Test10972DigestReplayOutcomeScheduledByUpdate pins the digest Outcome
// plumbing: a transitioned row schedules through the single schedule query
// with no quiet probe and no enqueue follow-up.
func Test10972DigestReplayOutcomeScheduledByUpdate(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{{true}}}}}
	queue := ReducerQueue{database: db}
	outcome, err := queue.ReplayWorkloadMaterializationForDigestOutcome(
		context.Background(), "scope-1", "gen-1", "entity-1", "digest-1",
	)
	if err != nil || outcome != reducer.WorkloadMaterializationReplayScheduled {
		t.Fatalf("outcome = (%q, %v), want (scheduled, nil)", outcome, err)
	}
	if got := len(db.queries); got != 1 {
		t.Fatalf("query count = %d, want 1: a transitioned row needs no quiet probe or enqueue", got)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("exec count = %d, want 0: the digest schedule is a single query", got)
	}
	if !strings.Contains(db.queries[0].query, "AS requeued") {
		t.Fatalf("first query = %q, want the digest schedule statement", db.queries[0].query)
	}
}

// Test10972DigestReplayOutcomeQuietStaysNotScheduled pins the quiet path: a
// finished row that already records the incoming digest reports NotScheduled
// after the quiet probe, with no enqueue follow-up.
func Test10972DigestReplayOutcomeQuietStaysNotScheduled(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{},
		{rows: [][]any{{"succeeded", "digest-same"}}},
	}}
	queue := ReducerQueue{database: db}
	outcome, err := queue.ReplayWorkloadMaterializationForDigestOutcome(
		context.Background(), "scope-1", "gen-1", "entity-1", "digest-same",
	)
	if err != nil || outcome != reducer.WorkloadMaterializationReplayNotScheduled {
		t.Fatalf("outcome = (%q, %v), want (not_scheduled, nil)", outcome, err)
	}
	if got := len(db.queries); got != 2 {
		t.Fatalf("query count = %d, want 2 (schedule, then the quiet probe)", got)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("exec count = %d, want 0: a quiet rerun enqueues nothing", got)
	}
}
