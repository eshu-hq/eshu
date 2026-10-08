// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"strings"
	"testing"
)

// TestReopenReducerWorkQuerySetsEveryClaimColumn pins the reopen UPDATE's
// SET list to the replaySucceededReducerDomainQuery assignments
// (reducer_queue_replay.go): every state column the reducer claim path
// reads. This hermetic guard runs in CI; the cross-query structural parity
// itself is #7731.
func TestReopenReducerWorkQuerySetsEveryClaimColumn(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"UPDATE fact_work_items",
		"status = 'pending'",
		"attempt_count = 0",
		"container_image_identity_v2_authorized_status = CASE",
		"WHEN work.container_image_identity_v2_required THEN 'pending'",
		"container_image_identity_v3_authorized_status = CASE",
		"WHEN work.container_image_identity_v3_required THEN 'pending'",
		"lease_owner = NULL",
		"claim_until = NULL",
		"visible_at = $1",
		"next_attempt_at = NULL",
		"updated_at = $1",
		"reopened_at = $1",
		"failure_class = NULL",
		"failure_message = NULL",
		"failure_details = NULL",
		// Full-predicate recheck after the SKIP LOCKED selection.
		"work.scope_id = $3",
		"work.generation_id = $4",
		"work.domain = $5",
		"work.stage = 'reducer'",
		"work.status = 'succeeded'",
		"RETURNING work.work_item_id",
	} {
		if !strings.Contains(reopenReducerWorkQuery, want) {
			t.Fatalf("reopen reducer query missing %q:\n%s", want, reopenReducerWorkQuery)
		}
	}
}

// TestReopenIntentQueriesClearCompletedAtWithGuard pins the intent reopen
// shape: one row per acceptance unit at an accepted run, cleared (never
// deleted) with the completed guard re-checked, locked skipping locked rows.
func TestReopenIntentQueriesClearCompletedAtWithGuard(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"SELECT DISTINCT ON (intent.acceptance_unit_id)",
		"JOIN shared_projection_acceptance",
		"intent.completed_at IS NOT NULL",
	} {
		if !strings.Contains(reopenIntentCandidatesQuery, want) {
			t.Fatalf("reopen intent candidates query missing %q:\n%s", want, reopenIntentCandidatesQuery)
		}
	}
	if !strings.Contains(reopenIntentLockQuery, "FOR UPDATE SKIP LOCKED") {
		t.Fatalf("reopen intent lock query must SKIP LOCKED:\n%s", reopenIntentLockQuery)
	}
	if !strings.Contains(reopenReducerLockQuery, "FOR UPDATE SKIP LOCKED") {
		t.Fatalf("reopen reducer lock query must SKIP LOCKED:\n%s", reopenReducerLockQuery)
	}
	for _, want := range []string{
		"SET completed_at = NULL",
		"intent.completed_at IS NOT NULL",
		"RETURNING intent.intent_id, intent.acceptance_unit_id, intent.source_run_id",
	} {
		if !strings.Contains(reopenIntentsQuery, want) {
			t.Fatalf("reopen intents query missing %q:\n%s", want, reopenIntentsQuery)
		}
	}
}
