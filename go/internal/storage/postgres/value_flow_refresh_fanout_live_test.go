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

const (
	refreshGlobalScope      = "eshu:global"
	refreshGlobalGeneration = "eshu:global:genesis"
	refreshSingletonItem    = "reducer_eshu_global_code_value_flow_refresh"
)

// TestValueFlowRefreshSeedMigrationSeedsGlobalSingletonLive pins migration
// 115: every bootstrap carries the eshu:global anchor (one perpetually-active
// generation) plus the refresh singleton in succeeded state, so producer
// completions in any later generation reopen it through the existing fanout
// join instead of new SQL.
func TestValueFlowRefreshSeedMigrationSeedsGlobalSingletonLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	var scopeKind, scopeStatus, activeGeneration string
	if err := db.QueryRowContext(ctx, `
SELECT scope_kind, status, active_generation_id
FROM ingestion_scopes WHERE scope_id = $1
`, refreshGlobalScope).Scan(&scopeKind, &scopeStatus, &activeGeneration); err != nil {
		t.Fatalf("read eshu:global scope: %v", err)
	}
	if scopeKind != "global" || scopeStatus != "active" {
		t.Fatalf("eshu:global scope = kind:%q status:%q, want global/active", scopeKind, scopeStatus)
	}
	if activeGeneration != refreshGlobalGeneration {
		t.Fatalf("eshu:global active generation = %q, want %q", activeGeneration, refreshGlobalGeneration)
	}
	var generationStatus string
	if err := db.QueryRowContext(ctx, `
SELECT status FROM scope_generations WHERE generation_id = $1
`, refreshGlobalGeneration).Scan(&generationStatus); err != nil {
		t.Fatalf("read eshu:global genesis generation: %v", err)
	}
	if generationStatus != "active" {
		t.Fatalf("genesis generation status = %q, want active", generationStatus)
	}
	var itemStatus, itemDomain string
	if err := db.QueryRowContext(ctx, `
SELECT status, domain FROM fact_work_items WHERE work_item_id = $1
`, refreshSingletonItem).Scan(&itemStatus, &itemDomain); err != nil {
		t.Fatalf("read refresh singleton item: %v", err)
	}
	if itemDomain != "code_value_flow_refresh" || itemStatus != "succeeded" {
		t.Fatalf("singleton = domain:%q status:%q, want code_value_flow_refresh/succeeded", itemDomain, itemStatus)
	}
}

// seedRefreshSingleton inserts the eshu:global anchor rows inline (the same
// rows migration 115 seeds) so the fanout proof does not depend on test
// execution order.
func seedRefreshSingleton(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'eshu:global', 'global', 'eshu', 'eshu:global', 'reducer', 'eshu:global',
    clock_timestamp(), clock_timestamp(), 'active', 'eshu:global:genesis'
) ON CONFLICT (scope_id) DO NOTHING
`); err != nil {
		t.Fatalf("seed eshu:global scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES (
    'eshu:global:genesis', 'eshu:global', 'synthetic', FALSE,
    clock_timestamp(), clock_timestamp(), 'active'
) ON CONFLICT (generation_id) DO NOTHING
`); err != nil {
		t.Fatalf("seed eshu:global genesis generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain,
    conflict_domain, conflict_key, status, attempt_count,
    payload, created_at, updated_at
) VALUES (
    'reducer_eshu_global_code_value_flow_refresh', 'eshu:global', 'eshu:global:genesis',
    'reducer', 'code_value_flow_refresh',
    'scope', 'eshu:global', 'succeeded', 0,
    jsonb_build_object(
        'entity_key', 'code_value_flow_refresh:global',
        'reason', 'value-flow refresh singleton',
        'source_system', 'reducer'
    ),
    clock_timestamp(), clock_timestamp()
) ON CONFLICT (work_item_id) DO NOTHING
`); err != nil {
		t.Fatalf("seed refresh singleton item: %v", err)
	}
}

// TestValueFlowRefreshFanoutReopensSingletonLive is the end-to-end proof: a
// gate-passing producer ACK emits an event, and the completion runner reopens
// the singleton (succeeded -> pending) and consumes the event.
func TestValueFlowRefreshFanoutReopensSingletonLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Second)
	const (
		scopeID    = "repository:6785-refresh-fanout"
		generation = "generation:6785-refresh-fanout"
		workItemID = "reducer_6785_refresh_fanout_producer"
		owner      = "refresh-fanout-owner"
	)
	seedRefreshSingleton(t, ctx, db)
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
	store := NewCrossScopeCompletionStore(SQLDB{DB: db})
	// The emitted event becomes visible 250ms after the ACK; run the claim
	// past that horizon like the existing fanout proofs.
	store.Now = func() time.Time { return now.Add(3 * time.Second) }
	runner := reducer.CrossScopeCompletionRunner{
		Queue:      store,
		LeaseOwner: owner,
		LeaseTTL:   time.Minute,
		BatchSize:  500,
		Now:        store.Now,
	}
	processed, result, err := runner.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("run refresh completion = %+v processed=%t err=%v", result, processed, err)
	}
	if result.EventsProcessed != 1 || result.IntentsEnqueued != 1 {
		t.Fatalf("refresh fanout = %+v, want events=1 intents=1", result)
	}
	var singletonStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, refreshSingletonItem).Scan(&singletonStatus); err != nil {
		t.Fatalf("read reopened singleton: %v", err)
	}
	if singletonStatus != "pending" {
		t.Fatalf("singleton status = %q, want pending", singletonStatus)
	}
	events, _ := countRefreshCompletionEvents(t, ctx, db, "workload_materialization")
	if events != 0 {
		t.Fatalf("producer events remaining = %d, want 0 consumed", events)
	}
}

// TestValueFlowRefreshGlobalScopeDoesNotHoldCanonicalLaneLive pins the B-7
// drain contract for the eshu:global anchor: the global scope publishes no
// git repository facts and never will, so without its vacuous
// canonical_nodes_committed phase row the canonical-code quiescence lane
// holds forever, deployable_unit_correlation defers every wave, and the
// golden drain never reaches terminal.
func TestValueFlowRefreshGlobalScopeDoesNotHoldCanonicalLaneLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	check := NewReducerGraphDrain(SQLDB{DB: db})
	uncommitted, err := check.HasUncommittedCanonicalCodeScopes(ctx)
	if err != nil {
		t.Fatalf("check canonical lane: %v", err)
	}
	if uncommitted {
		t.Fatal("eshu:global anchor holds the canonical-code quiescence lane; deployable_unit_correlation can never drain")
	}
}
