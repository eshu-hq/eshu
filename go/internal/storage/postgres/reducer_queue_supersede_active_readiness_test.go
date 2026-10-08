// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// supersedeActiveReadinessStaleRows is the scaled-down production shape from
// #7664: a few hundred pending gated rows on a superseded generation whose
// readiness phases exist only under the scope's active generation.
const supersedeActiveReadinessStaleRows = 300

// TestReducerClaimSupersedesStaleWorkWhenReadinessMetUnderActiveGeneration is
// the TDD regression for issue #7664: the supersede CTE gates a stale row on
// its own dead generation's readiness rows, so a stale row whose
// graph_projection_phase_state rows exist only under the scope's active
// generation is neither claimable nor supersedable. The fix widens the sweep
// gate to the active generation while leaving the claim candidate gate on the
// row's own generation, so no live row becomes claimable early.
//
// The test seeds a scope with gen-old active, 300 pending gated
// aws_relationship_materialization rows on gen-old, one negative-control gated
// row on gen-old with no phase row anywhere, then activates gen-new, adds one
// gated row on gen-new, and publishes the required canonical_nodes_committed
// phases under gen-new only. One Claim must supersede all 300 stale rows,
// leave the control pending (the #4445/A2 protection still holds when
// readiness is met under neither generation), and claim — never supersede —
// the live gen-new row.
//
// It executes against a live Postgres; it is skipped unless a DSN is provided
// so the package unit suite stays hermetic.
func TestReducerClaimSupersedesStaleWorkWhenReadinessMetUnderActiveGeneration(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the supersede-vs-readiness proof")
	}

	ctx := context.Background()
	db := openReducerSupersedeReadinessDB(t, ctx, dsn)

	const (
		scopeID    = "scope-supersede-active-readiness"
		genOld     = "gen-supersede-active-old"
		genNew     = "gen-supersede-active-new"
		domain     = reducer.DomainAWSRelationshipMaterialization
		activeID   = "sup-act-active-1"
		controlID  = "sup-act-control-1"
		activeKey  = "aws_resource_materialization:aws:123456789012:us-east-1:active"
		controlKey = "aws_resource_materialization:aws:123456789012:us-east-1:control"
	)

	oldIngested := time.Date(2026, time.July, 2, 8, 0, 0, 0, time.UTC)
	newIngested := oldIngested.Add(time.Hour)

	insertReducerSupersedeReadinessScope(t, ctx, db, scopeID, genOld, oldIngested)
	insertReducerSupersedeReadinessGeneration(t, ctx, db, genOld, scopeID, oldIngested, "active", oldIngested, nil)

	staleKeys := make([]string, 0, supersedeActiveReadinessStaleRows)
	for i := 0; i < supersedeActiveReadinessStaleRows; i++ {
		entityKey := fmt.Sprintf("aws_resource_materialization:aws:123456789012:us-east-1:stale-%04d", i)
		staleKeys = append(staleKeys, entityKey)
		insertReducerSupersedeReadinessWorkItem(t, ctx, db, reducerSupersedeReadinessWorkItem{
			workItemID:   fmt.Sprintf("sup-act-stale-%04d", i),
			scopeID:      scopeID,
			generationID: genOld,
			domain:       string(domain),
			entityKey:    entityKey,
			updatedAt:    oldIngested.Add(time.Minute),
		})
	}
	insertReducerSupersedeReadinessWorkItem(t, ctx, db, reducerSupersedeReadinessWorkItem{
		workItemID:   controlID,
		scopeID:      scopeID,
		generationID: genOld,
		domain:       string(domain),
		entityKey:    controlKey,
		updatedAt:    oldIngested.Add(time.Minute),
	})

	activateReducerSupersedeReadinessGeneration(t, ctx, db, scopeID, genOld, genNew, newIngested)

	insertReducerSupersedeReadinessWorkItem(t, ctx, db, reducerSupersedeReadinessWorkItem{
		workItemID:   activeID,
		scopeID:      scopeID,
		generationID: genNew,
		domain:       string(domain),
		entityKey:    activeKey,
		updatedAt:    newIngested.Add(time.Minute),
	})

	// Publish readiness ONLY under the active generation: the #7664 shape.
	// The stale rows' own generation has no phase rows at all.
	for _, entityKey := range staleKeys {
		insertReducerSupersedeReadinessPhaseState(t, ctx, db, scopeID, entityKey, genNew)
	}
	insertReducerSupersedeReadinessPhaseState(t, ctx, db, scopeID, activeKey, genNew)

	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "supersede-active-readiness-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return newIngested.Add(2 * time.Minute) },
	}

	intent, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !claimed {
		t.Fatalf("Claim() claimed nothing; the live gen-new row's readiness is satisfied and must be claimable")
	}
	if intent.IntentID != activeID {
		t.Fatalf("Claim() claimed %q, want the live row %q (stale rows must never be claimed)", intent.IntentID, activeID)
	}

	var superseded int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM fact_work_items
WHERE scope_id = $1 AND generation_id = $2 AND domain = $3
  AND work_item_id LIKE 'sup-act-stale-%' AND status = 'superseded'`,
		scopeID, genOld, string(domain)).Scan(&superseded); err != nil {
		t.Fatalf("count superseded stale rows: %v", err)
	}
	if superseded != supersedeActiveReadinessStaleRows {
		t.Fatalf("superseded stale rows = %d, want %d: rows whose readiness exists only under the active generation must retire",
			superseded, supersedeActiveReadinessStaleRows)
	}

	var failureClasses []string
	rows, err := db.QueryContext(ctx, `
SELECT DISTINCT failure_class
FROM fact_work_items
WHERE scope_id = $1 AND generation_id = $2 AND work_item_id LIKE 'sup-act-stale-%'`,
		scopeID, genOld)
	if err != nil {
		t.Fatalf("distinct stale failure classes: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var class string
		if err := rows.Scan(&class); err != nil {
			t.Fatalf("scan failure class: %v", err)
		}
		failureClasses = append(failureClasses, class)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}
	if len(failureClasses) != 1 || failureClasses[0] != "reducer_superseded_by_newer_active_generation" {
		t.Fatalf("stale failure classes = %q, want only [reducer_superseded_by_newer_active_generation]", failureClasses)
	}

	if status, _ := readReducerSupersedeReadinessWorkItemStatus(t, ctx, db, controlID); status != "pending" {
		t.Fatalf("negative-control row %q status = %q, want pending: readiness met under neither generation must still hold the row (#4445/A2)",
			controlID, status)
	}
	if status, _ := readReducerSupersedeReadinessWorkItemStatus(t, ctx, db, activeID); status == "superseded" {
		t.Fatalf("live row %q was superseded; the sweep must never retire the active generation's work", activeID)
	}
}

// seedSupersedeActiveReadinessScope seeds one scope in the #7664 shape: staleN
// pending gated rows on a superseded generation, one gated row on the active
// generation, and the required phases published only under the active
// generation. Each scope is its own conflict key (conflict_key = scope_id), so
// live rows on different scopes are claimable concurrently while the #3558
// fence still partitions each scope.
func seedSupersedeActiveReadinessScope(
	t *testing.T, ctx context.Context, db *sql.DB,
	scopeID, genOld, genNew, idPrefix string, staleN int,
	oldIngested, newIngested time.Time,
) {
	t.Helper()
	const domain = reducer.DomainAWSRelationshipMaterialization

	insertReducerSupersedeReadinessScope(t, ctx, db, scopeID, genOld, oldIngested)
	insertReducerSupersedeReadinessGeneration(t, ctx, db, genOld, scopeID, oldIngested, "active", oldIngested, nil)

	staleKeys := make([]string, 0, staleN)
	for i := 0; i < staleN; i++ {
		entityKey := fmt.Sprintf("aws_resource_materialization:aws:123456789012:us-east-1:%s-stale-%04d", idPrefix, i)
		staleKeys = append(staleKeys, entityKey)
		insertReducerSupersedeReadinessWorkItem(t, ctx, db, reducerSupersedeReadinessWorkItem{
			workItemID:   fmt.Sprintf("sup-conc-%s-stale-%04d", idPrefix, i),
			scopeID:      scopeID,
			generationID: genOld,
			domain:       string(domain),
			entityKey:    entityKey,
			updatedAt:    oldIngested.Add(time.Minute),
		})
	}

	activateReducerSupersedeReadinessGeneration(t, ctx, db, scopeID, genOld, genNew, newIngested)

	// Phase rows reference their generation, so they are published only now
	// that gen-new exists — and only under gen-new, the #7664 shape.
	for _, entityKey := range staleKeys {
		insertReducerSupersedeReadinessPhaseState(t, ctx, db, scopeID, entityKey, genNew)
	}

	activeKey := fmt.Sprintf("aws_resource_materialization:aws:123456789012:us-east-1:%s-active", idPrefix)
	insertReducerSupersedeReadinessWorkItem(t, ctx, db, reducerSupersedeReadinessWorkItem{
		workItemID:   fmt.Sprintf("sup-conc-%s-active", idPrefix),
		scopeID:      scopeID,
		generationID: genNew,
		domain:       string(domain),
		entityKey:    activeKey,
		updatedAt:    newIngested.Add(time.Minute),
	})
	insertReducerSupersedeReadinessPhaseState(t, ctx, db, scopeID, activeKey, genNew)
}

// TestReducerSupersedeActiveReadinessConcurrentClaims proves the widened sweep
// does not race with live claims: claimers draining the #7664 shape on two
// scopes at once must each see no error, retire every stale row exactly once,
// and claim each live row exactly once (attempt_count 1, no double-claim, no
// live row superseded, no deadlock). Run with -race to catch data races in
// the concurrent claim drivers.
func TestReducerSupersedeActiveReadinessConcurrentClaims(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the supersede-vs-readiness proof")
	}

	ctx := context.Background()
	db, schemaName := openReducerSupersedeReadinessDBWithSchema(t, ctx, dsn)

	const (
		stalePerScope = 25
		claimers      = 2
	)

	oldIngested := time.Date(2026, time.July, 2, 10, 0, 0, 0, time.UTC)
	newIngested := oldIngested.Add(time.Hour)

	seedSupersedeActiveReadinessScope(t, ctx, db,
		"scope-supersede-active-conc-a", "gen-supersede-active-conc-a-old", "gen-supersede-active-conc-a-new",
		"a", stalePerScope, oldIngested, newIngested)
	seedSupersedeActiveReadinessScope(t, ctx, db,
		"scope-supersede-active-conc-b", "gen-supersede-active-conc-b-old", "gen-supersede-active-conc-b-new",
		"b", stalePerScope, oldIngested, newIngested)

	// Each claimer gets its OWN Postgres connection to the shared schema so
	// their claim statements truly interleave at the database — sharing one
	// pooled connection would serialize them and make this proof vacuous.
	queues := make([]ReducerQueue, claimers)
	for i := range queues {
		claimerDB := openReducerFairnessClaimerDB(t, ctx, dsn, schemaName)
		queues[i] = ReducerQueue{
			database:      SQLDB{DB: claimerDB},
			LeaseOwner:    fmt.Sprintf("supersede-concurrent-%d", i),
			LeaseDuration: time.Minute,
			Now:           func() time.Time { return newIngested.Add(2 * time.Minute) },
		}
	}

	start := make(chan struct{})
	errs := make(chan error, claimers)
	var wg sync.WaitGroup
	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(q ReducerQueue) {
			defer wg.Done()
			<-start
			for j := 0; j < 2*stalePerScope+claimers; j++ {
				_, claimed, err := q.Claim(ctx)
				if err != nil {
					errs <- fmt.Errorf("%s Claim() error = %v", q.LeaseOwner, err)
					return
				}
				if !claimed {
					return
				}
			}
			errs <- fmt.Errorf("%s claimed past the drain cap", q.LeaseOwner)
		}(queues[i])
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	var superseded int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM fact_work_items
WHERE work_item_id LIKE 'sup-conc-%-stale-%' AND status = 'superseded'`).Scan(&superseded); err != nil {
		t.Fatalf("count superseded stale rows: %v", err)
	}
	if superseded != 2*stalePerScope {
		t.Fatalf("superseded stale rows = %d, want %d", superseded, 2*stalePerScope)
	}

	var misClaimed int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM fact_work_items
WHERE work_item_id LIKE 'sup-conc-%-active'
  AND (status = 'superseded' OR attempt_count <> 1)`).Scan(&misClaimed); err != nil {
		t.Fatalf("count mis-claimed live rows: %v", err)
	}
	if misClaimed != 0 {
		t.Fatalf("%d live rows were superseded or not claimed exactly once; want 0", misClaimed)
	}
}

// TestSupersedeSweepReadinessGateCoversActiveGeneration is the hermetic guard
// for the #7664 sweep predicate: the live proofs above are DSN-gated and SKIP
// in CI, so this test pins the active-generation OR arm in the shipped sweep
// CTE text. If the OR branch is dropped, CI fails here instead of staying
// green on skipped tests.
func TestSupersedeSweepReadinessGateCoversActiveGeneration(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"supersede_active_readiness_req",
		"supersede_active_readiness_phase",
		"scope.active_generation_id",
	} {
		if !strings.Contains(supersedeInactiveReducerGenerationsCTE, want) {
			t.Fatalf("supersedeInactiveReducerGenerationsCTE does not contain %q; the active-generation OR arm (#7664) is missing", want)
		}
	}
}

// TestClaimReadinessGateDelegatesToGenerationPinnedForm pins the #7664
// refactor: the claim-time gate must render byte-identical SQL to the
// generation-pinned form evaluated at the row's own generation, so every
// pre-existing caller keeps its exact predicate and only the sweep gains the
// active-generation arm.
func TestClaimReadinessGateDelegatesToGenerationPinnedForm(t *testing.T) {
	t.Parallel()
	got := reducerClaimReadinessGateSQL("work", "req", "phase")
	want := reducerClaimReadinessGateForGenerationSQL("work", "work.generation_id", "req", "phase")
	if got != want {
		t.Fatalf("reducerClaimReadinessGateSQL drifted from the generation-pinned form:\n got: %q\nwant: %q", got, want)
	}
}

// supersedeActiveReadinessExplainDB wraps the claim path with a rolled-back
// EXPLAIN (ANALYZE, BUFFERS) of the real claim statement, following the
// ack-scale plan probe convention.
type supersedeActiveReadinessExplainDB struct {
	SQLDB
	t    *testing.T
	plan *string
}

func (database *supersedeActiveReadinessExplainDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if query == claimReducerWorkQuery && database.plan != nil && *database.plan == "" {
		tx, err := database.DB.BeginTx(ctx, nil)
		if err != nil {
			database.t.Fatalf("begin explain txn: %v", err)
		}
		var plan string
		explainErr := tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&plan)
		rollbackErr := tx.Rollback()
		if explainErr != nil || rollbackErr != nil {
			database.t.Fatalf("explain claim: query=%v rollback=%v", explainErr, rollbackErr)
		}
		*database.plan = plan
	}
	return database.SQLDB.QueryContext(ctx, query, args...)
}

// TestReducerSupersedeActiveReadinessClaimPlan captures the claim statement's
// plan over the #7664 shape in a rolled-back transaction. It asserts the
// sweep and its readiness gate are in the plan; the timings are plan evidence
// for the PR, not a latency gate.
func TestReducerSupersedeActiveReadinessClaimPlan(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the supersede-vs-readiness proof")
	}

	ctx := context.Background()
	db := openReducerSupersedeReadinessDB(t, ctx, dsn)

	const (
		scopeID = "scope-supersede-active-plan"
		genOld  = "gen-supersede-active-plan-old"
		genNew  = "gen-supersede-active-plan-new"
		domain  = reducer.DomainAWSRelationshipMaterialization
		staleN  = 10
	)

	oldIngested := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	newIngested := oldIngested.Add(time.Hour)

	insertReducerSupersedeReadinessScope(t, ctx, db, scopeID, genOld, oldIngested)
	insertReducerSupersedeReadinessGeneration(t, ctx, db, genOld, scopeID, oldIngested, "active", oldIngested, nil)

	for i := 0; i < staleN; i++ {
		entityKey := fmt.Sprintf("aws_resource_materialization:aws:123456789012:us-east-1:plan-stale-%04d", i)
		insertReducerSupersedeReadinessWorkItem(t, ctx, db, reducerSupersedeReadinessWorkItem{
			workItemID:   fmt.Sprintf("sup-plan-stale-%04d", i),
			scopeID:      scopeID,
			generationID: genOld,
			domain:       string(domain),
			entityKey:    entityKey,
			updatedAt:    oldIngested.Add(time.Minute),
		})
	}

	activateReducerSupersedeReadinessGeneration(t, ctx, db, scopeID, genOld, genNew, newIngested)

	for i := 0; i < staleN; i++ {
		entityKey := fmt.Sprintf("aws_resource_materialization:aws:123456789012:us-east-1:plan-stale-%04d", i)
		insertReducerSupersedeReadinessPhaseState(t, ctx, db, scopeID, entityKey, genNew)
	}

	var plan string
	probe := &supersedeActiveReadinessExplainDB{SQLDB: SQLDB{DB: db}, t: t, plan: &plan}
	queue := ReducerQueue{
		database:      probe,
		LeaseOwner:    "supersede-active-plan-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return newIngested.Add(2 * time.Minute) },
	}
	if _, _, err := queue.Claim(ctx); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if plan == "" {
		t.Fatalf("no plan captured for the claim statement")
	}
	for _, want := range []string{"superseded_stale_reducer_generations", "graph_projection_phase_state"} {
		if !strings.Contains(plan, want) {
			t.Fatalf("claim plan does not mention %q; the sweep or its gate is missing from the plan", want)
		}
	}
	t.Logf("7664 claim plan over the active-readiness shape (rolled back): %s", plan)
}
