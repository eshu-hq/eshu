// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
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
