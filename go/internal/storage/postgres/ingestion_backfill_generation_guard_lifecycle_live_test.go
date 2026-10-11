// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Live lifecycle proofs for the #7978 reconcile re-projection (rulings a9-117
// and a9-120) on a disposable PostgreSQL database
// (ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN). They reuse the reprojectProof
// harness from ingestion_backfill_generation_guard_reproject_live_test.go.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestReconcileReopenSkipsLiveLeaseWithoutStampLive (a9-117 d8): the active
// generation's projector row is held under a live lease (a zombie heal or a
// liveness re-drive is running it). The reconcile stores nothing, leaves the
// lease alone, and writes no reconcile_requested_at, so it cannot keep the
// scope fresh on a run it did not start.
func TestReconcileReopenSkipsLiveLeaseWithoutStampLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	ctx := context.Background()
	p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
	p.project()
	if _, err := p.db.ExecContext(ctx, `UPDATE fact_work_items
SET status = 'running', lease_owner = 'other-worker', claim_until = now() + interval '5 minutes', attempt_count = 2
WHERE stage = 'projector' AND generation_id = 'gen-a'`); err != nil {
		t.Fatalf("lease gen-a: %v", err)
	}
	before := p.rowCounts()

	p.commit(p.generation("gen-r", "hint-a", "sha-a", "interval_elapsed"))

	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonReconcileReprojectionPending); got != 1 {
		t.Fatalf("reconcile_reprojection_pending skips = %d, want 1", got)
	}
	status, reopens, owner, attempts := p.workRowAttempts("gen-a")
	if status != "running" || reopens != 0 || owner.String != "other-worker" || attempts != 2 {
		t.Fatalf("gen-a work = %s reopens=%d owner=%v attempts=%d, want the live lease untouched", status, reopens, owner, attempts)
	}
	state, err := p.store.FullReconcileState(ctx, p.scope.ScopeID)
	if err != nil || !state.ReconcileRequestedAt.IsZero() {
		t.Fatalf("ReconcileRequestedAt = %v (%v), want none", state.ReconcileRequestedAt, err)
	}
	if after := p.rowCounts(); after["scope_generations"] != before["scope_generations"] || after["fact_records"] != before["fact_records"] {
		t.Fatalf("rows %v -> %v, want unchanged", before, after)
	}
}

// TestReconcileReprojectionDeadLetterKeepsActiveGenerationLive (a9-117 Q4):
// a dead-lettered re-projection of the active generation leaves it active and
// keeps the scope's pointer. It stamps reconcile_dead_lettered_at, the sweep
// reads it, and the stale reconcile that follows stores a generation.
func TestReconcileReprojectionDeadLetterKeepsActiveGenerationLive(t *testing.T) {
	p := openReprojectProof(t)
	ctx := context.Background()
	p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
	p.project()
	p.commit(p.generation("gen-r", "hint-a", "sha-a", "interval_elapsed"))

	queue := NewProjectorQueue(SQLDB{DB: p.db}, "proof-worker", time.Minute)
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok || work.Generation.GenerationID != "gen-a" {
		t.Fatalf("claim = %v ok=%v err=%v, want gen-a", work.Generation.GenerationID, ok, err)
	}
	if err := queue.Fail(ctx, work, errors.New("proof: non-retryable re-projection failure")); err != nil {
		t.Fatalf("fail gen-a re-projection: %v", err)
	}

	var generationStatus, scopeStatus string
	var pointer sql.NullString
	if err := p.db.QueryRowContext(ctx, `
SELECT generation.status, scope.status, scope.active_generation_id
FROM scope_generations AS generation
JOIN ingestion_scopes AS scope ON scope.scope_id = generation.scope_id
WHERE generation.generation_id = 'gen-a'`).Scan(&generationStatus, &scopeStatus, &pointer); err != nil {
		t.Fatalf("read gen-a after dead letter: %v", err)
	}
	if generationStatus != "active" || scopeStatus != "active" || pointer.String != "gen-a" {
		t.Fatalf("after dead letter generation=%s scope=%s pointer=%v; want active, active, gen-a", generationStatus, scopeStatus, pointer)
	}
	if status, _, _ := p.workRow("gen-a"); status != "dead_letter" {
		t.Fatalf("gen-a work = %s, want dead_letter", status)
	}
	state, err := p.store.FullReconcileState(ctx, p.scope.ScopeID)
	if err != nil {
		t.Fatalf("FullReconcileState: %v", err)
	}
	if state.ReconcileDeadLetteredAt.Before(state.ReconcileRequestedAt) || state.ReconcileRequestedAt.IsZero() {
		t.Fatalf("request=%v dead letter=%v, want a dead letter at or after the request", state.ReconcileRequestedAt, state.ReconcileDeadLetteredAt)
	}

	before := p.rowCounts()
	p.commit(p.generation("gen-heal", "hint-a", "sha-a", scope.ReconcileReasonRequestStale))
	after := p.rowCounts()
	if after["scope_generations"] != before["scope_generations"]+1 || after["fact_records"] != before["fact_records"]+p.facts {
		t.Fatalf("stale reconcile rows %v -> %v, want one stored generation and %d facts", before, after, p.facts)
	}
}

// TestPendingSameIdentitySkipsAndFailedStoresLive (a9-120 §7.1): a snapshot
// equal to a generation still pending stores nothing, because that generation
// is already queued. The same snapshot against a failed generation stores a
// new one: that is the retry.
func TestPendingSameIdentitySkipsAndFailedStoresLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	ctx := context.Background()
	p.commit(p.generation("gen-b", "hint-b", "sha-b", ""))
	p.project()
	p.commit(p.generation("gen-x", "hint-x", "sha-x", ""))

	before := p.rowCounts()
	p.commit(p.generation("gen-x-again", "hint-x", "sha-x", "reconcile_in_flight_expired"))
	if after := p.rowCounts(); after["scope_generations"] != before["scope_generations"] || after["fact_records"] != before["fact_records"] {
		t.Fatalf("rows %v -> %v against a pending match, want unchanged", before, after)
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonPendingSameIdentity); got != 1 {
		t.Fatalf("pending_same_identity skips = %d, want 1", got)
	}

	if _, err := p.db.ExecContext(ctx, `UPDATE scope_generations SET status = 'failed' WHERE generation_id = 'gen-x'`); err != nil {
		t.Fatalf("fail gen-x: %v", err)
	}
	p.commit(p.generation("gen-x-retry", "hint-x", "sha-x", "retry_after_unprojected"))
	if after := p.rowCounts(); after["scope_generations"] != before["scope_generations"]+1 || after["fact_records"] != before["fact_records"]+p.facts {
		t.Fatalf("rows %v -> %v against a failed match, want one stored generation", before, after)
	}
}

// proofRetryableError is a retryable projector failure for the live proofs.
type proofRetryableError struct{}

func (proofRetryableError) Error() string   { return "proof: retryable re-projection failure" }
func (proofRetryableError) Retryable() bool { return true }

// TestReconcileReopenResetsAttemptsSoRetryRetriesLive (lead ask on a9-117 Q4):
// a succeeded row whose attempt_count reached 2 comes back with 0, so the
// re-projection's first retryable failure retries instead of dead-lettering.
func TestReconcileReopenResetsAttemptsSoRetryRetriesLive(t *testing.T) {
	p := openReprojectProof(t)
	ctx := context.Background()
	p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
	p.project()
	if _, err := p.db.ExecContext(ctx, `UPDATE fact_work_items SET attempt_count = 2
WHERE stage = 'projector' AND generation_id = 'gen-a'`); err != nil {
		t.Fatalf("age gen-a attempts: %v", err)
	}
	p.commit(p.generation("gen-r", "hint-a", "sha-a", "interval_elapsed"))
	if status, _, _, attempts := p.workRowAttempts("gen-a"); status != "pending" || attempts != 0 {
		t.Fatalf("gen-a work = %s attempts=%d, want pending attempts=0", status, attempts)
	}
	queue := NewProjectorQueue(SQLDB{DB: p.db}, "proof-worker", time.Minute)
	queue.MaxAttempts = 3
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok || work.AttemptCount != 1 {
		t.Fatalf("claim = attempt %d ok=%v err=%v, want attempt 1", work.AttemptCount, ok, err)
	}
	if err := queue.Fail(ctx, work, proofRetryableError{}); err != nil {
		t.Fatalf("fail retryable: %v", err)
	}
	if status, _, _, _ := p.workRowAttempts("gen-a"); status != "retrying" {
		t.Fatalf("gen-a work after a retryable failure = %s, want retrying", status)
	}
}

// TestStaleReconcileSkipsPendingSameIdentityLive (lead ask reconciling a9-117
// Q3 with a9-120): the stale reason stores a generation only against an
// active match. When the generation it would store already exists as a
// pending generation (the stale full from an earlier cycle), it stores
// nothing, so a stale request can never store one generation per cycle.
func TestStaleReconcileSkipsPendingSameIdentityLive(t *testing.T) {
	p := openReprojectProof(t)
	p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
	p.project()
	before := p.rowCounts()
	p.commit(p.generation("gen-stale-1", "hint-a", "sha-a", scope.ReconcileReasonRequestStale))
	afterFirst := p.rowCounts()
	if afterFirst["scope_generations"] != before["scope_generations"]+1 {
		t.Fatalf("first stale reconcile rows %v -> %v, want one stored generation", before, afterFirst)
	}
	p.commit(p.generation("gen-stale-2", "hint-a", "sha-a", scope.ReconcileReasonRequestStale))
	if afterSecond := p.rowCounts(); afterSecond["scope_generations"] != afterFirst["scope_generations"] || afterSecond["fact_records"] != afterFirst["fact_records"] {
		t.Fatalf("second stale reconcile rows %v -> %v, want unchanged (pending same identity)", afterFirst, afterSecond)
	}
}
