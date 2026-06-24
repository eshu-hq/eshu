// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestClaimBatchDoesNotStarveNewerDomainsBehindOlderBacklog reproduces #3385:
// a single reducer lane that claims several domains must not let a high-volume
// domain with an older, continuously-regenerated backlog indefinitely starve a
// lower-volume domain whose work is newer. Before the per-domain fairness
// ordering, ORDER BY updated_at ASC drained only the oldest domain, so the AWS
// cloud producer domains (cloud_inventory_admission, aws_resource_materialization,
// aws_cloud_runtime_drift) never reached the front of the queue while
// supply_chain_impact / package_source_correlation kept a deeper, older backlog.
//
// The test seeds an older backlog (busyDomain) larger than the batch size and a
// smaller, newer set for starvedDomain in distinct conflict groups, then asserts
// a single ClaimBatch makes progress on starvedDomain too. It executes against a
// live Postgres; it is skipped unless a DSN is provided so the package unit suite
// stays hermetic.
func TestClaimBatchDoesNotStarveNewerDomainsBehindOlderBacklog(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the reducer domain fairness proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)

	const (
		busyDomain    = reducer.DomainSupplyChainImpact
		starvedDomain = reducer.DomainAWSCloudRuntimeDrift
		busyCount     = 40
		starvedCount  = 8
		batchSize     = 16
	)

	now := time.Date(2026, time.June, 21, 8, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-fair", now)

	// Older backlog for the busy domain: each row in its own conflict group so
	// the conflict fence and the same-group representative never collapse them.
	for i := 0; i < busyCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("busy-%03d", i),
			scopeID:        "scope-fair",
			generationID:   "gen-fair",
			domain:         string(busyDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("busy-key-%03d", i),
			updatedAt:      now.Add(time.Duration(i) * time.Second),
		})
	}
	// Newer, smaller set for the starved domain, also in distinct conflict groups.
	starvedBase := now.Add(time.Hour)
	for i := 0; i < starvedCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("starved-%03d", i),
			scopeID:        "scope-fair",
			generationID:   "gen-fair",
			domain:         string(starvedDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("starved-key-%03d", i),
			updatedAt:      starvedBase.Add(time.Duration(i) * time.Second),
		})
	}

	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "fairness-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return starvedBase.Add(2 * time.Hour) },
		ClaimDomains:  []reducer.Domain{busyDomain, starvedDomain},
	}

	intents, err := queue.ClaimBatch(ctx, batchSize)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}

	var starvedClaimed int
	for _, intent := range intents {
		if intent.Domain == starvedDomain {
			starvedClaimed++
		}
	}
	if starvedClaimed == 0 {
		t.Fatalf("ClaimBatch() claimed %d items, none for starved domain %q; "+
			"older high-volume domain %q monopolized the lane (issue #3385)",
			len(intents), starvedDomain, busyDomain)
	}
}

// TestClaimBatchFairnessRankExcludesNonClaimableRows is the TDD regression for
// the P1 found in PR #3386: the original fairness rank counted ALL visible
// same-domain representatives regardless of whether they would pass the
// supersede-stale-generation gate, inflating the rank for any domain that had
// older inactive-generation rows in the queue. This caused the first actually-
// claimable row of such a domain to sort behind every correctly-ranked row of
// other domains — recreating the starvation that #3385 originally fixed.
//
// The test seeds:
//   - starvedDomain: 10 older rows in an INACTIVE generation + 4 newer rows in
//     the ACTIVE generation for the same scope (the inactive rows would be
//     superseded and skipped by the outer candidate WHERE, so they must not
//     inflate the fairness rank for the 4 active rows).
//   - busyDomain: 8 rows all in the ACTIVE generation, with timestamps between
//     the stale starved rows and the active starved rows.
//
// With the buggy rank: starved active rows get rank≥10 (behind 10 stale rows),
// so a batch of 8 fills entirely with busyDomain rows and starvedDomain gets 0.
// With the fixed rank: stale rows are excluded → starved active rows get rank
// 0-3, busyDomain rows get rank 0-7 → the ordering interleaves them and the
// batch contains at least one starvedDomain row.
func TestClaimBatchFairnessRankExcludesNonClaimableRows(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the reducer domain fairness proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)

	const (
		busyDomain    = reducer.DomainSupplyChainImpact
		starvedDomain = reducer.DomainAWSCloudRuntimeDrift
		staleCount    = 10 // inactive-gen rows that must NOT inflate the rank
		activeStarved = 4  // active-gen rows that should be fairly ranked
		activeBusy    = 8  // active-gen rows for the competing domain
		batchSize     = 8
	)

	now := time.Date(2026, time.June, 21, 9, 0, 0, 0, time.UTC)

	// Insert scope with an active generation and a stale generation so the
	// supersede CTE will mark stale rows during the claim transaction.
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ('scope-p1', 'cloud', 'aws', 'scope-p1', NULL, 'aws', 'scope-p1',
          $1, $1, 'active', 'gen-p1-active', '{}'::jsonb)`, now); err != nil {
		t.Fatalf("insert scope-p1: %v", err)
	}
	// Active generation.
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ('gen-p1-active', 'scope-p1', 'snapshot', 'p1active',
          $1, $1, 'active', $1, NULL, '{}'::jsonb)`, now); err != nil {
		t.Fatalf("insert gen-p1-active: %v", err)
	}
	// Stale (inactive) generation — ingested earlier so supersede CTE marks its
	// fact_work_items rows during claim. Status must NOT be 'active' here because
	// scope_generations_active_scope_idx enforces at most one active generation
	// per scope_id; use 'superseded' to satisfy the constraint while still
	// having an older ingested_at that the supersede CTE looks at.
	staleTime := now.Add(-2 * time.Hour)
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ('gen-p1-stale', 'scope-p1', 'snapshot', 'p1stale',
          $1, $1, 'superseded', $1, $1, '{}'::jsonb)`, staleTime); err != nil {
		t.Fatalf("insert gen-p1-stale: %v", err)
	}

	// Stale starvedDomain rows: oldest timestamps — they inflate the rank when
	// the bug is present because the fairness subquery doesn't exclude them.
	for i := 0; i < staleCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("p1-stale-starved-%03d", i),
			scopeID:        "scope-p1",
			generationID:   "gen-p1-stale", // INACTIVE generation
			domain:         string(starvedDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("p1-stale-starved-key-%03d", i),
			updatedAt:      staleTime.Add(time.Duration(i) * time.Second),
		})
	}

	// Active busyDomain rows: timestamps between stale and active starved.
	busyBase := now.Add(-30 * time.Minute)
	for i := 0; i < activeBusy; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("p1-busy-%03d", i),
			scopeID:        "scope-p1",
			generationID:   "gen-p1-active",
			domain:         string(busyDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("p1-busy-key-%03d", i),
			updatedAt:      busyBase.Add(time.Duration(i) * time.Second),
		})
	}

	// Active starvedDomain rows: newer than busyDomain but should still appear
	// in a fair batch because their within-domain rank is 0-3 (only 4 active
	// rows exist; stale rows must not be counted).
	activeBase := now.Add(-10 * time.Minute)
	for i := 0; i < activeStarved; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("p1-active-starved-%03d", i),
			scopeID:        "scope-p1",
			generationID:   "gen-p1-active",
			domain:         string(starvedDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("p1-active-starved-key-%03d", i),
			updatedAt:      activeBase.Add(time.Duration(i) * time.Second),
		})
	}

	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "fairness-p1-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now.Add(time.Hour) },
		ClaimDomains:  []reducer.Domain{busyDomain, starvedDomain},
	}

	intents, err := queue.ClaimBatch(ctx, batchSize)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}

	var starvedClaimed int
	for _, intent := range intents {
		if intent.Domain == starvedDomain {
			starvedClaimed++
		}
	}
	if starvedClaimed == 0 {
		t.Fatalf("ClaimBatch() claimed %d items, none for active-gen starved domain %q; "+
			"inactive-gen rows inflated the fairness rank (P1 in PR #3386 / issue #3385)",
			len(intents), starvedDomain)
	}
}

func reducerDomainFairnessDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_REDUCER_FAIRNESS_PROOF_DSN")); dsn != "" {
		return dsn
	}
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

func openReducerFairnessDB(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	return db
}

// openReducerFairnessDBWithSchema creates an isolated throwaway schema, applies
// the reducer-queue DDL, and returns the owning handle plus the schema name.
// The handle is capped at one connection so its session-local search_path stays
// pinned to the new schema. The schema name lets concurrency proofs open
// additional independent connections against the SAME schema (see
// openReducerFairnessClaimerDB), which is required to exercise real concurrent
// claim statements rather than serializing behind one pooled connection.
func openReducerFairnessDBWithSchema(t *testing.T, ctx context.Context, dsn string) (*sql.DB, string) {
	t.Helper()
	schemaName := fmt.Sprintf("reducer_fairness_%d", time.Now().UnixNano())
	db := openReducerFairnessSchemaConn(t, ctx, dsn, schemaName)

	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create fairness schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	for _, stmt := range []string{
		scopeSchemaSQL,
		scopeGenerationSchemaSQL,
		workItemSchemaSQL,
		graphProjectionPhaseStateSchemaSQL,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply fairness schema: %v", err)
		}
	}
	return db, schemaName
}

// openReducerFairnessClaimerDB opens an independent single-connection handle
// bound to an already-created fairness schema. Concurrency proofs give each
// concurrent claimer its own handle so that N claimers hold N live Postgres
// connections and their claim statements truly interleave at the database;
// sharing one handle (which caps at a single pooled connection) would serialize
// them and make a fence/atomicity proof vacuous.
func openReducerFairnessClaimerDB(t *testing.T, ctx context.Context, dsn, schemaName string) *sql.DB {
	t.Helper()
	return openReducerFairnessSchemaConn(t, ctx, dsn, schemaName)
}

// openReducerFairnessSchemaConn opens a pgx handle capped at one connection and
// pins its session-local search_path to schemaName. The single-connection cap
// is what keeps SET search_path durable for the handle: search_path is
// connection-local, so a multi-connection pool would hand out fresh connections
// that no longer see the schema's tables. Each handle still represents one live
// connection, so distinct handles run concurrently.
func openReducerFairnessSchemaConn(t *testing.T, ctx context.Context, dsn, schemaName string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	return db
}

func seedReducerFairnessScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID string, now time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'cloud', 'aws', $1, NULL, 'aws', $1, $2, $2, 'active', 'gen-fair', '{}'::jsonb)`,
		scopeID, now); err != nil {
		t.Fatalf("insert fairness scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ('gen-fair', $1, 'snapshot', 'fair', $2, $2, 'active', $2, NULL, '{}'::jsonb)`,
		scopeID, now); err != nil {
		t.Fatalf("insert fairness generation: %v", err)
	}
}

type reducerFairnessWorkItem struct {
	workItemID     string
	scopeID        string
	generationID   string
	domain         string
	conflictDomain string
	conflictKey    string
	updatedAt      time.Time
}

func insertReducerFairnessWorkItem(t *testing.T, ctx context.Context, db *sql.DB, item reducerFairnessWorkItem) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, payload, created_at, updated_at
) VALUES ($1::text, $2, $3, 'reducer', $4, $5, $6, 'pending', 0,
    jsonb_build_object('entity_key', $1::text, 'reason', 'fairness', 'fact_id', $1::text, 'source_system', 'aws'),
    $7, $7)`,
		item.workItemID, item.scopeID, item.generationID, item.domain,
		item.conflictDomain, item.conflictKey, item.updatedAt); err != nil {
		t.Fatalf("insert fairness work item %q: %v", item.workItemID, err)
	}
}
