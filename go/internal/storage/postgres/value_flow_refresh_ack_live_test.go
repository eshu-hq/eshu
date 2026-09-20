// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/affected"
)

func seedValueFlowRefreshAckWorkItem(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	domain reducer.Domain,
	scopeID string,
	generationID string,
	leaseOwner string,
	claimUntil time.Time,
	updatedAt time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain,
    conflict_domain, conflict_key, status, attempt_count,
    lease_owner, claim_until, last_attempt_at, payload, created_at, updated_at
) VALUES (
    $1, $2, $3, 'reducer', $4,
    'intent', $1, 'claimed', 1,
    $5, $6, $7,
    jsonb_build_object(
        'entity_key', $1::text,
        'reason', 'value-flow refresh emit-gate proof',
        'source_system', 'reducer'
    ),
    $7, $7
)
`, workItemID, scopeID, generationID, string(domain), leaseOwner, claimUntil, updatedAt); err != nil {
		t.Fatalf("seed value-flow refresh ACK work item %s: %v", workItemID, err)
	}
}

func valueFlowRefreshAckQueue(db *sql.DB, owner string, now time.Time) ReducerQueue {
	return ReducerQueue{
		database: SQLDB{DB: db}, LeaseOwner: owner,
		LeaseDuration: time.Minute, Now: func() time.Time { return now },
	}
}

func countRefreshCompletionEvents(t *testing.T, ctx context.Context, db *sql.DB, domain string) (events int, items int64) {
	t.Helper()
	if err := db.QueryRowContext(ctx, `
SELECT count(*), COALESCE(sum(producer_item_count), 0)
FROM cross_scope_completion_events
WHERE producer_domain = $1
  AND status = 'pending'
`, domain).Scan(&events, &items); err != nil {
		t.Fatalf("read refresh completion events for %s: %v", domain, err)
	}
	return events, items
}

func refreshAckIntent(workItemID string, domain reducer.Domain, claimedAt time.Time) reducer.Intent {
	return reducer.Intent{IntentID: workItemID, Domain: domain, ClaimedAt: &claimedAt}
}

func refreshAckResult(writes int, signals map[string]float64) reducer.Result {
	return reducer.Result{CanonicalWrites: writes, SubSignals: signals}
}

// TestValueFlowRefreshAckEmitsEventWhenGatePassesLive is the GREEN side: a
// producer ACK whose run wrote rows and reports affected repos emits one
// coalesced completion event for its domain.
func TestValueFlowRefreshAckEmitsEventWhenGatePassesLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Second)
	const (
		scopeID    = "repository:6785-refresh-emit-green"
		generation = "generation:6785-refresh-emit-green"
		workItemID = "reducer_6785_refresh_emit_green"
		owner      = "refresh-emit-green-owner"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	seedValueFlowRefreshAckWorkItem(
		t, ctx, db, workItemID, reducer.DomainWorkloadMaterialization,
		scopeID, generation, owner, now.Add(time.Minute), now,
	)
	queue := valueFlowRefreshAckQueue(db, owner, now)
	if err := queue.Ack(
		ctx,
		refreshAckIntent(workItemID, reducer.DomainWorkloadMaterialization, now),
		refreshAckResult(3, map[string]float64{affected.RefreshAffectedReposSignal: 2}),
	); err != nil {
		t.Fatalf("ACK refresh producer: %v", err)
	}
	events, items := countRefreshCompletionEvents(t, ctx, db, "workload_materialization")
	if events != 1 || items != 1 {
		t.Fatalf("completion aggregate = events:%d items:%d, want 1/1", events, items)
	}
}

// TestValueFlowRefreshAckSuppressesEventOnExplicitZeroLive is the RED side of
// the emit gate: an explicit zero affected-repo signal means "gated, none
// affected", so the ACK succeeds but emits no completion event.
func TestValueFlowRefreshAckSuppressesEventOnExplicitZeroLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Second)
	const (
		scopeID    = "repository:6785-refresh-emit-red"
		generation = "generation:6785-refresh-emit-red"
		workItemID = "reducer_6785_refresh_emit_red"
		owner      = "refresh-emit-red-owner"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	seedValueFlowRefreshAckWorkItem(
		t, ctx, db, workItemID, reducer.DomainIAMCanPerformMaterialization,
		scopeID, generation, owner, now.Add(time.Minute), now,
	)
	queue := valueFlowRefreshAckQueue(db, owner, now)
	if err := queue.Ack(
		ctx,
		refreshAckIntent(workItemID, reducer.DomainIAMCanPerformMaterialization, now),
		refreshAckResult(3, map[string]float64{affected.RefreshAffectedReposSignal: 0}),
	); err != nil {
		t.Fatalf("ACK refresh producer: %v", err)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, workItemID).Scan(&status); err != nil {
		t.Fatalf("read ACKed item status: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("ACKed item status = %q, want succeeded", status)
	}
	events, items := countRefreshCompletionEvents(t, ctx, db, "iam_can_perform_materialization")
	if events != 0 || items != 0 {
		t.Fatalf("completion aggregate = events:%d items:%d, want 0/0 on explicit zero", events, items)
	}
}

// TestValueFlowRefreshAckFailsOpenWithoutSignalLive pins fail-open: a producer
// run that wrote rows but carries no gate signal (unwired path) still emits.
func TestValueFlowRefreshAckFailsOpenWithoutSignalLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Second)
	const (
		scopeID    = "repository:6785-refresh-emit-open"
		generation = "generation:6785-refresh-emit-open"
		workItemID = "reducer_6785_refresh_emit_open"
		owner      = "refresh-emit-open-owner"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	seedValueFlowRefreshAckWorkItem(
		t, ctx, db, workItemID, reducer.DomainAWSResourceMaterialization,
		scopeID, generation, owner, now.Add(time.Minute), now,
	)
	queue := valueFlowRefreshAckQueue(db, owner, now)
	if err := queue.Ack(
		ctx,
		refreshAckIntent(workItemID, reducer.DomainAWSResourceMaterialization, now),
		refreshAckResult(2, nil),
	); err != nil {
		t.Fatalf("ACK refresh producer: %v", err)
	}
	events, items := countRefreshCompletionEvents(t, ctx, db, "aws_resource_materialization")
	if events != 1 || items != 1 {
		t.Fatalf("completion aggregate = events:%d items:%d, want 1/1 fail-open", events, items)
	}
}

// TestValueFlowRefreshAckBatchEmitsOnlyForGatedItemsLive pins batch emission:
// of two same-domain items only the gate-passing one contributes to the event.
func TestValueFlowRefreshAckBatchEmitsOnlyForGatedItemsLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Second)
	const (
		scopeID = "repository:6785-refresh-emit-batch"
		owner   = "refresh-emit-batch-owner"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, "generation:6785-refresh-emit-batch")
	seedValueFlowRefreshAckWorkItem(
		t, ctx, db, "reducer_6785_refresh_batch_emit", reducer.DomainWorkloadCloudRelationshipMaterialization,
		scopeID, "generation:6785-refresh-emit-batch", owner, now.Add(time.Minute), now,
	)
	seedValueFlowRefreshAckWorkItem(
		t, ctx, db, "reducer_6785_refresh_batch_hold", reducer.DomainWorkloadCloudRelationshipMaterialization,
		scopeID, "generation:6785-refresh-emit-batch", owner, now.Add(time.Minute), now,
	)
	queue := valueFlowRefreshAckQueue(db, owner, now)
	intents := []reducer.Intent{
		refreshAckIntent("reducer_6785_refresh_batch_emit", reducer.DomainWorkloadCloudRelationshipMaterialization, now),
		refreshAckIntent("reducer_6785_refresh_batch_hold", reducer.DomainWorkloadCloudRelationshipMaterialization, now),
	}
	results := []reducer.Result{
		refreshAckResult(4, map[string]float64{affected.RefreshAffectedReposSignal: 1}),
		refreshAckResult(4, map[string]float64{affected.RefreshAffectedReposSignal: 0}),
	}
	if err := queue.AckBatch(ctx, intents, results); err != nil {
		t.Fatalf("batch ACK refresh producers: %v", err)
	}
	events, items := countRefreshCompletionEvents(t, ctx, db, "workload_cloud_relationship_materialization")
	if events != 1 || items != 1 {
		t.Fatalf("completion aggregate = events:%d items:%d, want 1/1 (emit item only)", events, items)
	}
}
