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
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"

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
		database:      SQLDB{DB: db},
		LeaseOwner:    "fairness-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return starvedBase.Add(2 * time.Hour) },
		ClaimDomains:  []reducercontract.Domain{busyDomain, starvedDomain},
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
		database:      SQLDB{DB: db},
		LeaseOwner:    "fairness-p1-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now.Add(time.Hour) },
		ClaimDomains:  []reducercontract.Domain{busyDomain, starvedDomain},
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

// reducerFairnessProofOptIn routes the disposable opt-in for the DSN the
// caller resolved through reducerDomainFairnessDSN (or, for one enrolled
// proof, ESHU_POSTGRES_TEST_DSN directly): the fairness family's own
// opt-in, fail-closed when the family DSN is set without it, else the
// generic fallback the local pair sets. An opt-in other than "1" skips in
// openDisposablePublicBootstrapDB: a proof that cannot isolate itself must
// not run half-isolated.
func reducerFairnessProofOptIn(t *testing.T, dsn string) string {
	t.Helper()
	if family := strings.TrimSpace(os.Getenv("ESHU_REDUCER_FAIRNESS_PROOF_DSN")); family != "" && dsn == family {
		if os.Getenv("ESHU_REDUCER_FAIRNESS_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_REDUCER_FAIRNESS_PROOF_DSN is set without ESHU_REDUCER_FAIRNESS_PROOF_DISPOSABLE=1")
		}
		return "1"
	}
	return os.Getenv("ESHU_POSTGRES_DISPOSABLE")
}

func openReducerFairnessDB(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	return db
}

// openReducerFairnessDBWithSchema bootstraps a fresh disposable database on
// the public schema and returns the owning handle plus a DSN targeting that
// database, so concurrency proofs can open additional independent
// connections against the SAME database (see openReducerFairnessClaimerDB),
// which is required to exercise real concurrent claim statements rather
// than serializing behind one pooled connection.
//
// Database-level isolation (not an isolated schema): the lane's 241a/241b
// authority migrations are public-only, so ApplyBootstrapWithOptions skips
// them on an isolated schema and the later 242a definition then fails with
// a missing eshu_security schema. A fresh database keeps the full install
// and stays private to the proof.
//
// The layout derives from ApplyBootstrapWithoutContentSearchIndexes (#6680):
// the previous hand-picked definition list silently fell behind the
// production schema on every migration a claim or Ack path depended on
// (provenance_edge_identity_upgrade_required, projector_scope_claim_fences,
// delta_baseline_commit_sha, ...).
// TestReducerFairnessIsolatedSchemaDerivesFromBootstrap guards the
// derivation against the same production bootstrap.
func openReducerFairnessDBWithSchema(t *testing.T, ctx context.Context, dsn string) (*sql.DB, string) {
	t.Helper()
	admin, err := postgresproof.WithDatabase(dsn, "postgres")
	if err != nil {
		t.Fatalf("retarget fairness proof DSN: %v", err)
	}
	_, database := postgresproof.OpenDisposableBootstrapDatabase(t, admin,
		reducerFairnessProofOptIn(t, dsn), 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrapWithoutContentSearchIndexes(ctx, SQLDB{DB: database})
		})
	target, err := postgresproof.WithDatabase(admin, currentProofDatabase(ctx, t, database))
	if err != nil {
		t.Fatalf("target fairness proof database: %v", err)
	}
	return database, target
}

// currentProofDatabase returns the database a proof handle is connected to.
func currentProofDatabase(ctx context.Context, t *testing.T, db *sql.DB) string {
	t.Helper()
	var database string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&database); err != nil {
		t.Fatalf("read proof database: %v", err)
	}
	return database
}

// openReducerFairnessClaimerDB opens an independent single-connection handle
// on the fairness proof's disposable database. Concurrency proofs give each
// concurrent claimer its own handle so that N claimers hold N live Postgres
// connections and their claim statements truly interleave at the database;
// sharing one handle (which caps at a single pooled connection) would serialize
// them and make a fence/atomicity proof vacuous. targetDSN is the second
// value openReducerFairnessDBWithSchema returns.
func openReducerFairnessClaimerDB(t *testing.T, ctx context.Context, targetDSN string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", targetDSN)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
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
	sourceSystem   string
	updatedAt      time.Time
}

func insertReducerFairnessWorkItem(t *testing.T, ctx context.Context, db *sql.DB, item reducerFairnessWorkItem) {
	t.Helper()
	sourceSystem := strings.TrimSpace(item.sourceSystem)
	if sourceSystem == "" {
		sourceSystem = "aws"
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, payload, created_at, updated_at
) VALUES ($1::text, $2, $3, 'reducer', $4, $5, $6, 'pending', 0,
    jsonb_build_object('entity_key', $1::text, 'reason', 'fairness', 'fact_id', $1::text, 'source_system', $7::text),
    $8, $8)`,
		item.workItemID, item.scopeID, item.generationID, item.domain,
		item.conflictDomain, item.conflictKey, sourceSystem, item.updatedAt); err != nil {
		t.Fatalf("insert fairness work item %q: %v", item.workItemID, err)
	}
}
