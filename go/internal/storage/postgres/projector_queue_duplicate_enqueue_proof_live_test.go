// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason claim:duplicate-enqueue collapse proof (eight concurrent Enqueue calls for one scope generation collapse to exactly one pending row via ON CONFLICT (work_item_id) DO NOTHING with no error, and disjoint scope generations both land concurrently), untagged and gated by the ESHU_REDUCER_FAIRNESS_PROOF_DSN env Skip; Postgres only; about 5s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Duplicate-enqueue collapse proof for issue #11354.
//
// The projector service enqueues projection work per scope generation from
// several entry points (ingester wiring, projector runtime wiring,
// bootstrap-index wiring). Retries and overlapping sweeps enqueue the same
// (scope_id, generation_id) twice. The enqueue statement
// (enqueueProjectorWorkQuery) keys the row by the deterministic
// projectorWorkItemID and ends in ON CONFLICT (work_item_id) DO NOTHING, so
// concurrent duplicates must collapse to one pending row instead of erroring
// (unique violation) or doubling the work.
//
// These proofs run real concurrent Enqueue() callers against a live Postgres
// and the production enqueue SQL. They are skipped unless a DSN is provided
// so the package unit suite stays hermetic. Run with -race to catch data
// races in the concurrent enqueue drivers.

// TestProjectorEnqueueCollapsesConcurrentDuplicateWorkItems proves that many
// concurrent enqueues of the same scope generation collapse to exactly one
// pending fact_work_items row with no error. A regression that dropped the
// ON CONFLICT DO NOTHING clause would surface as unique-violation errors;
// a regression that swallowed the conflict without inserting would surface
// as a missing row.
func TestProjectorEnqueueCollapsesConcurrentDuplicateWorkItems(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the duplicate-enqueue collapse proof")
	}

	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	const scopeID = "scope-enqueue-collapse"
	seedReducerFairnessScope(t, ctx, db, scopeID, now)

	// Each enqueuer gets its OWN Postgres connection to the shared
	// disposable database so their INSERT statements truly interleave at
	// the database — sharing one pooled connection would serialize them
	// and make this collapse proof vacuous.
	const enqueuers = 8
	var (
		start = make(chan struct{})
		wg    sync.WaitGroup
		errs  = make([]error, enqueuers)
	)
	for i := 0; i < enqueuers; i++ {
		enqueuerDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
		q := ProjectorQueue{
			database:      SQLDB{DB: enqueuerDB},
			LeaseOwner:    fmt.Sprintf("enqueue-collapser-%d", i),
			LeaseDuration: time.Minute,
			Now:           func() time.Time { return now },
		}
		wg.Add(1)
		go func(q ProjectorQueue, slot int) {
			defer wg.Done()
			<-start
			errs[slot] = q.Enqueue(ctx, scopeID, "gen-fair")
		}(q, i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("enqueuer %d Enqueue() error = %v; concurrent duplicates must collapse, not fail", i, err)
		}
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fact_work_items WHERE work_item_id = $1`,
		ProjectorWorkItemID(scopeID, "gen-fair")).Scan(&count); err != nil {
		t.Fatalf("count collapsed work items: %v", err)
	}
	if count != 1 {
		t.Fatalf("collapsed duplicate enqueues = %d rows, want exactly 1; ON CONFLICT (work_item_id) DO NOTHING must collapse concurrent duplicates (issue #11354)", count)
	}

	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`,
		ProjectorWorkItemID(scopeID, "gen-fair")).Scan(&status); err != nil {
		t.Fatalf("read collapsed work item status: %v", err)
	}
	if status != "pending" {
		t.Fatalf("collapsed work item status = %q, want pending; the collapse must keep the item claimable", status)
	}
}

// TestProjectorEnqueueAdmitsDisjointGenerationsConcurrently proves the
// collapse is the ON CONFLICT key, not serialization: two concurrent
// enqueues of DISJOINT scope generations both land. A "fix" that serialized
// all projector enqueues (single-threaded drain) would still pass the
// collapse proof above, so this companion guards the partition-by-work-item
// design (AGENTS.md Serialization Is Not A Fix).
func TestProjectorEnqueueAdmitsDisjointGenerationsConcurrently(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the duplicate-enqueue collapse proof")
	}

	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)

	now := time.Date(2026, time.June, 22, 13, 0, 0, 0, time.UTC)
	// Two scopes, one active generation each: scope_generations holds at
	// most one active generation per scope
	// (scope_generations_active_scope_idx), and ingestion_scopes holds at
	// most one scope per active generation
	// (ingestion_scopes_active_generation_idx), so disjointness runs
	// across scopes with distinct generations rather than generations of
	// one scope.
	type scopeGen struct{ scope, generation string }
	pairs := []scopeGen{
		{"scope-enqueue-disjoint-a", "gen-disjoint-a"},
		{"scope-enqueue-disjoint-b", "gen-disjoint-b"},
	}
	for _, pair := range pairs {
		seedEnqueueCollapseScope(t, ctx, db, pair.scope, pair.generation, now)
	}

	var (
		start = make(chan struct{})
		wg    sync.WaitGroup
		errs  = make([]error, len(pairs))
	)
	for i, pair := range pairs {
		enqueuerDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
		q := ProjectorQueue{
			database:      SQLDB{DB: enqueuerDB},
			LeaseOwner:    fmt.Sprintf("enqueue-disjoint-%d", i),
			LeaseDuration: time.Minute,
			Now:           func() time.Time { return now },
		}
		wg.Add(1)
		go func(q ProjectorQueue, slot int, scopeID, generationID string) {
			defer wg.Done()
			<-start
			errs[slot] = q.Enqueue(ctx, scopeID, generationID)
		}(q, i, pair.scope, pair.generation)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("disjoint enqueue %d Enqueue() error = %v", i, err)
		}
	}
	for _, pair := range pairs {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fact_work_items WHERE work_item_id = $1`,
			ProjectorWorkItemID(pair.scope, pair.generation)).Scan(&count); err != nil {
			t.Fatalf("count disjoint work item %q: %v", pair.scope, err)
		}
		if count != 1 {
			t.Fatalf("disjoint scope %q enqueues = %d rows, want 1; concurrent disjoint enqueues must both land", pair.scope, count)
		}
	}
}

// seedEnqueueCollapseScope seeds one scope with one active generation,
// parameterized by generation: unlike seedReducerFairnessScope (which pins
// 'gen-fair'), disjoint proofs need distinct generations per scope because
// ingestion_scopes_active_generation_idx holds at most one scope per active
// generation.
func seedEnqueueCollapseScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string, now time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'cloud', 'aws', $1, NULL, 'aws', $1, $2, $2, 'active', $3, '{}'::jsonb)`,
		scopeID, now, generationID); err != nil {
		t.Fatalf("insert enqueue scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ($1, $2, 'snapshot', 'fair', $3, $3, 'active', $3, NULL, '{}'::jsonb)`,
		generationID, scopeID, now); err != nil {
		t.Fatalf("insert enqueue generation: %v", err)
	}
}
