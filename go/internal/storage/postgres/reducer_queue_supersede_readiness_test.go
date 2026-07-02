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

// TestReducerClaimDoesNotSupersedeReadinessGatedPendingWorkBehindNewerGeneration
// is the TDD regression for issue #4445 (A2): the reducer's
// supersedeInactiveReducerGenerationsCTE (reducer_generation_filter_sql.go)
// terminalizes older-generation pending/retrying rows purely by generation
// ordering (stale.generation_id <> scope.active_generation_id plus
// ingested_at ordering) with NO knowledge of the readiness gate
// (graph_projection_phase_state / reducer_claim_readiness_requirements). The
// candidate CTE in reducer_queue_claim_query.go applies the readiness gate
// AFTER the supersede CTE has already run in the same statement, so a
// readiness-gated domain (e.g. aws_relationship_materialization) whose gen N
// work is still waiting on its required canonical_nodes_committed phase gets
// marked 'superseded' the instant gen N+1 becomes the scope's active
// generation — even though gen N's readiness-gated work was never given a
// chance to become claimable. 'superseded' is a terminal, unreplayable status
// (reopenSucceededReducerWorkQuery / replaySucceededReducerDomainQuery only
// reopen status='succeeded'), so this permanently drops the materialization
// intent and produces incomplete graph output (missing edges) for gen N's
// domain.
//
// The test seeds:
//   - scope with gen-old ACTIVE and a readiness-gated
//     aws_relationship_materialization work item still pending (its
//     canonical_nodes_committed phase for the required cloud_resource_uid
//     keyspace is deliberately NOT published, so it is not yet claimable).
//   - gen-new is then activated for the same scope (gen-old becomes stale by
//     ingested_at ordering), which is the exact trigger condition the
//     supersede CTE keys off.
//
// It then runs one reducer Claim() (which executes
// supersedeInactiveReducerGenerationsCTE as part of the same statement) and
// asserts the gen-old readiness-gated row's status is NOT 'superseded'. Before
// the fix this assertion fails (status = 'superseded'); after the fix the row
// must still be in a replayable state (pending/retrying) because its domain's
// readiness requirement was never satisfied while it was live.
//
// It executes against a live Postgres; it is skipped unless a DSN is provided
// so the package unit suite stays hermetic.
func TestReducerClaimDoesNotSupersedeReadinessGatedPendingWorkBehindNewerGeneration(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the supersede-vs-readiness proof")
	}

	ctx := context.Background()
	db := openReducerSupersedeReadinessDB(t, ctx, dsn)

	const (
		scopeID     = "scope-supersede-readiness"
		genOld      = "gen-supersede-old"
		genNew      = "gen-supersede-new"
		domain      = reducer.DomainAWSRelationshipMaterialization
		workItemID  = "supersede-readiness-work-1"
		entityKey   = "aws_resource_materialization:aws:123456789012:us-east-1:lambda"
		cloudResUID = "aws:123456789012:us-east-1:lambda"
	)

	oldIngested := time.Date(2026, time.July, 1, 8, 0, 0, 0, time.UTC)
	newIngested := oldIngested.Add(time.Hour)

	// Seed the scope with gen-old as the active generation, matching the
	// state at the moment the readiness-gated work item was enqueued.
	insertReducerSupersedeReadinessScope(t, ctx, db, scopeID, genOld, oldIngested)
	insertReducerSupersedeReadinessGeneration(t, ctx, db, genOld, scopeID, oldIngested, "active", oldIngested, nil)

	// Readiness-gated pending work item for gen-old. Its required
	// graph_projection_phase_state row (cloud_resource_uid /
	// canonical_nodes_committed) is deliberately absent, so the readiness
	// gate keeps it un-claimable — this is the "unmet readiness" precondition
	// from the issue's TDD guidance.
	insertReducerSupersedeReadinessWorkItem(t, ctx, db, reducerSupersedeReadinessWorkItem{
		workItemID:   workItemID,
		scopeID:      scopeID,
		generationID: genOld,
		domain:       string(domain),
		entityKey:    entityKey,
		updatedAt:    oldIngested.Add(time.Minute),
	})

	// Now activate gen-new for the same scope. This is the exact trigger the
	// supersede CTE keys off: gen-old's ingested_at is now older than the
	// scope's active generation, and gen-old is no longer scope.active_generation_id.
	activateReducerSupersedeReadinessGeneration(t, ctx, db, scopeID, genOld, genNew, newIngested)

	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "supersede-readiness-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return newIngested.Add(time.Minute) },
	}

	// Run a claim. No row is claimable (readiness unmet, and the only
	// candidate row belongs to the now-inactive generation), but the
	// supersede CTE still executes as part of the same claim statement.
	if _, claimed, err := queue.Claim(ctx); err != nil {
		t.Fatalf("Claim() error = %v", err)
	} else if claimed {
		t.Fatalf("Claim() unexpectedly claimed work; readiness was never satisfied")
	}

	status, failureClass := readReducerSupersedeReadinessWorkItemStatus(t, ctx, db, workItemID)
	if status == "superseded" {
		t.Fatalf(
			"work item %q was superseded (failure_class=%q) by the newer generation "+
				"while its readiness gate (%s/canonical_nodes_committed for %s) was never "+
				"satisfied; issue #4445: supersession must not run ahead of the readiness gate",
			workItemID, failureClass, cloudResUID, domain,
		)
	}
	if status != "pending" && status != "retrying" {
		t.Fatalf("work item %q status = %q, want pending/retrying (replayable)", workItemID, status)
	}
}

// TestReducerClaimSupersedesReadinessGatedWorkOnceReadinessIsSatisfied proves
// the fix does not over-broaden protection: once a readiness-gated domain's
// required canonical-node phase IS visible, a gen-old row for that domain
// must still be superseded by a newer active generation exactly like today —
// the fix only defers supersession while readiness is unmet, it does not
// exempt readiness-gated domains from supersession forever.
func TestReducerClaimSupersedesReadinessGatedWorkOnceReadinessIsSatisfied(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the supersede-vs-readiness proof")
	}

	ctx := context.Background()
	db := openReducerSupersedeReadinessDB(t, ctx, dsn)

	const (
		scopeID    = "scope-supersede-readiness-ready"
		genOld     = "gen-supersede-old-ready"
		genNew     = "gen-supersede-new-ready"
		domain     = reducer.DomainAWSRelationshipMaterialization
		workItemID = "supersede-readiness-ready-work-1"
		// The bounded readiness requirement for aws_relationship_materialization
		// uses acceptance_unit_source='payload_entity_key' (reducer_queue_readiness_sql.go),
		// so the graph_projection_phase_state acceptance_unit_id must match the
		// work item's raw payload entity_key verbatim, not a derived resource id.
		entityKey = "aws_resource_materialization:aws:123456789012:us-east-1:lambda-ready"
	)

	oldIngested := time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC)
	newIngested := oldIngested.Add(time.Hour)

	insertReducerSupersedeReadinessScope(t, ctx, db, scopeID, genOld, oldIngested)
	insertReducerSupersedeReadinessGeneration(t, ctx, db, genOld, scopeID, oldIngested, "active", oldIngested, nil)

	insertReducerSupersedeReadinessWorkItem(t, ctx, db, reducerSupersedeReadinessWorkItem{
		workItemID:   workItemID,
		scopeID:      scopeID,
		generationID: genOld,
		domain:       string(domain),
		entityKey:    entityKey,
		updatedAt:    oldIngested.Add(time.Minute),
	})

	// Publish the required canonical_nodes_committed phase for this work
	// item's acceptance unit (its raw payload entity_key) and generation
	// BEFORE the newer generation activates, so readiness is satisfied at the
	// moment of claim.
	insertReducerSupersedeReadinessPhaseState(t, ctx, db, scopeID, entityKey, genOld)

	activateReducerSupersedeReadinessGeneration(t, ctx, db, scopeID, genOld, genNew, newIngested)

	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "supersede-readiness-ready-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return newIngested.Add(time.Minute) },
	}

	if _, claimed, err := queue.Claim(ctx); err != nil {
		t.Fatalf("Claim() error = %v", err)
	} else if claimed {
		t.Fatalf("Claim() unexpectedly claimed gen-old work; gen-old is inactive and must not be claimable")
	}

	status, _ := readReducerSupersedeReadinessWorkItemStatus(t, ctx, db, workItemID)
	if status != "superseded" {
		t.Fatalf(
			"work item %q status = %q, want superseded: readiness was satisfied so the newer "+
				"generation must still terminalize the gen-old row (fix must not over-broaden protection)",
			workItemID, status,
		)
	}
}

// TestReducerClaimSupersedesNonReadinessGatedWorkBehindNewerGeneration proves
// the fix preserves today's behavior for reducer domains that have no
// readiness requirement row at all (most domains): the readiness-gate NOT
// EXISTS is vacuously true for them, so pure generation-ordering supersession
// still applies unchanged.
func TestReducerClaimSupersedesNonReadinessGatedWorkBehindNewerGeneration(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the supersede-vs-readiness proof")
	}

	ctx := context.Background()
	db := openReducerSupersedeReadinessDB(t, ctx, dsn)

	const (
		scopeID      = "scope-supersede-non-gated"
		genOld       = "gen-supersede-old-non-gated"
		genNew       = "gen-supersede-new-non-gated"
		nonGatedDom  = reducer.DomainSupplyChainImpact
		workItemID   = "supersede-non-gated-work-1"
		entityKeyVal = "supply-chain-impact-entity-1"
	)

	oldIngested := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)
	newIngested := oldIngested.Add(time.Hour)

	insertReducerSupersedeReadinessScope(t, ctx, db, scopeID, genOld, oldIngested)
	insertReducerSupersedeReadinessGeneration(t, ctx, db, genOld, scopeID, oldIngested, "active", oldIngested, nil)

	insertReducerSupersedeReadinessWorkItem(t, ctx, db, reducerSupersedeReadinessWorkItem{
		workItemID:   workItemID,
		scopeID:      scopeID,
		generationID: genOld,
		domain:       string(nonGatedDom),
		entityKey:    entityKeyVal,
		updatedAt:    oldIngested.Add(time.Minute),
	})

	activateReducerSupersedeReadinessGeneration(t, ctx, db, scopeID, genOld, genNew, newIngested)

	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "supersede-non-gated-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return newIngested.Add(time.Minute) },
	}

	if _, claimed, err := queue.Claim(ctx); err != nil {
		t.Fatalf("Claim() error = %v", err)
	} else if claimed {
		t.Fatalf("Claim() unexpectedly claimed gen-old work for non-gated domain")
	}

	status, _ := readReducerSupersedeReadinessWorkItemStatus(t, ctx, db, workItemID)
	if status != "superseded" {
		t.Fatalf(
			"work item %q status = %q, want superseded: non-readiness-gated domain %q has no "+
				"requirement row, so generation-ordering supersession must apply unchanged",
			workItemID, status, nonGatedDom,
		)
	}
}

// insertReducerSupersedeReadinessPhaseState publishes the canonical_nodes_committed
// readiness phase for the cloud_resource_uid keyspace, matching the bounded
// readiness requirement rows aws_relationship_materialization depends on
// (reducer_queue_readiness_sql.go).
func insertReducerSupersedeReadinessPhaseState(
	t *testing.T, ctx context.Context, db *sql.DB, scopeID, acceptanceUnitID, generationID string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO graph_projection_phase_state (
    scope_id, acceptance_unit_id, source_run_id, generation_id,
    keyspace, phase, committed_at, updated_at
) VALUES ($1, $2, $3, $3, 'cloud_resource_uid', 'canonical_nodes_committed', $4, $4)`,
		scopeID, acceptanceUnitID, generationID, time.Now().UTC()); err != nil {
		t.Fatalf("insert graph projection phase state for %q: %v", acceptanceUnitID, err)
	}
}

func reducerSupersedeReadinessDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_REDUCER_SUPERSEDE_READINESS_DSN")); dsn != "" {
		return dsn
	}
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

func openReducerSupersedeReadinessDB(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	schemaName := fmt.Sprintf("reducer_supersede_readiness_%d", time.Now().UnixNano())

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create supersede-readiness schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	for _, stmt := range []string{
		MigrationSQL("ingestion_scopes"),
		MigrationSQL("scope_generations"),
		MigrationSQL("fact_work_items"),
		graphProjectionPhaseStateSchemaSQL,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply supersede-readiness schema: %v", err)
		}
	}
	return db
}

func insertReducerSupersedeReadinessScope(
	t *testing.T, ctx context.Context, db *sql.DB, scopeID, activeGenerationID string, now time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'cloud', 'aws', $1, NULL, 'aws', $1, $2, $2, 'active', $3, '{}'::jsonb)`,
		scopeID, now, activeGenerationID); err != nil {
		t.Fatalf("insert supersede-readiness scope: %v", err)
	}
}

func insertReducerSupersedeReadinessGeneration(
	t *testing.T, ctx context.Context, db *sql.DB,
	generationID, scopeID string, ingestedAt time.Time, status string, activatedAt time.Time, supersededAt *time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ($1, $2, 'snapshot', $1, $3, $3, $4, $5, $6, '{}'::jsonb)`,
		generationID, scopeID, ingestedAt, status, activatedAt, supersededAt); err != nil {
		t.Fatalf("insert supersede-readiness generation %q: %v", generationID, err)
	}
}

// activateReducerSupersedeReadinessGeneration inserts a new generation for the
// scope, marks it active, and flips the previously-active generation to
// 'superseded' (satisfying scope_generations_active_scope_idx, which allows at
// most one active generation per scope) while keeping its earlier ingested_at
// so the supersede CTE's ordering predicate matches.
func activateReducerSupersedeReadinessGeneration(
	t *testing.T, ctx context.Context, db *sql.DB, scopeID, oldGenerationID, newGenerationID string, newIngestedAt time.Time,
) {
	t.Helper()
	// scope_generations_active_scope_idx allows at most one active generation
	// per scope, so the prior generation must flip to 'superseded' before the
	// new active row is inserted, and the scope's active_generation_id
	// pointer must move in the same statement ordering to avoid violating the
	// index at either step.
	if _, err := db.ExecContext(ctx, `
UPDATE scope_generations
SET status = 'superseded', superseded_at = $1
WHERE generation_id = $2`, newIngestedAt, oldGenerationID); err != nil {
		t.Fatalf("supersede prior generation %q: %v", oldGenerationID, err)
	}
	insertReducerSupersedeReadinessGeneration(t, ctx, db, newGenerationID, scopeID, newIngestedAt, "active", newIngestedAt, nil)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes
SET active_generation_id = $1
WHERE scope_id = $2`, newGenerationID, scopeID); err != nil {
		t.Fatalf("activate generation %q for scope %q: %v", newGenerationID, scopeID, err)
	}
}

type reducerSupersedeReadinessWorkItem struct {
	workItemID   string
	scopeID      string
	generationID string
	domain       string
	entityKey    string
	updatedAt    time.Time
}

func insertReducerSupersedeReadinessWorkItem(
	t *testing.T, ctx context.Context, db *sql.DB, item reducerSupersedeReadinessWorkItem,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, payload, created_at, updated_at
) VALUES ($1::text, $2, $3, 'reducer', $4, 'scope', $2, 'pending', 0,
    jsonb_build_object('entity_key', $5::text, 'reason', 'supersede-readiness-proof', 'fact_id', $1::text, 'source_system', 'aws'),
    $6, $6)`,
		item.workItemID, item.scopeID, item.generationID, item.domain, item.entityKey, item.updatedAt); err != nil {
		t.Fatalf("insert supersede-readiness work item %q: %v", item.workItemID, err)
	}
}

func readReducerSupersedeReadinessWorkItemStatus(
	t *testing.T, ctx context.Context, db *sql.DB, workItemID string,
) (status string, failureClass string) {
	t.Helper()
	row := db.QueryRowContext(ctx, `
SELECT status, COALESCE(failure_class, '')
FROM fact_work_items
WHERE work_item_id = $1`, workItemID)
	if err := row.Scan(&status, &failureClass); err != nil {
		t.Fatalf("read work item %q status: %v", workItemID, err)
	}
	return status, failureClass
}
