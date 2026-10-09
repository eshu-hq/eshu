// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// TestGenerationRetentionPrescreenSkipsOverFactGenerationsLive is #7334 fix
// 2 through the real store: ps-over (150 own facts against a limit of 100)
// is skipped with row_limit_own_rows and survives, while ps-under (50 facts)
// is counted and pruned. The pre-screen SQL is also queried directly to pin
// its per-generation counts.
func TestGenerationRetentionPrescreenSkipsOverFactGenerationsLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	old := now.Add(-10 * 24 * time.Hour)

	seedPrescreenGeneration(t, ctx, database, "ps-over", old, 150)
	seedPrescreenGeneration(t, ctx, database, "ps-under", old, 50)

	got := map[string]int64{}
	rows, err := database.QueryContext(ctx, generationRetentionPrescreenQuery,
		[]string{"ps-over-g0", "ps-under-g0", "ps-missing-g0"})
	if err != nil {
		t.Fatalf("pre-screen query: %v", err)
	}
	for rows.Next() {
		var generationID string
		var count int64
		if err := rows.Scan(&generationID, &count); err != nil {
			_ = rows.Close()
			t.Fatalf("pre-screen scan: %v", err)
		}
		got[generationID] = count
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("pre-screen query: %v", err)
	}
	if got["ps-over-g0"] != 150 || got["ps-under-g0"] != 50 {
		t.Fatalf("pre-screen counts = %v, want ps-over-g0 150 and ps-under-g0 50", got)
	}

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(ctx, GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "7334-prescreen-live",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1 (ps-under only)", result.GenerationsPruned)
	}
	if got, want := result.Skipped["row_limit_own_rows"], 1; got != want {
		t.Fatalf("Skipped[row_limit_own_rows] = %d, want %d (ps-over)", got, want)
	}
	remaining := remainingScopeGenerations(t, ctx, database, []string{"ps-over-g0", "ps-under-g0"})
	if len(remaining) != 1 || !remaining["ps-over-g0"] {
		t.Fatalf("remaining generations = %v, want only ps-over-g0", remaining)
	}
}

// seedPrescreenGeneration seeds scopeID with one superseded generation
// carrying facts own fact rows.
func seedPrescreenGeneration(t *testing.T, ctx context.Context, database *sql.DB, scopeID string, old time.Time, facts int) {
	t.Helper()
	testfixtures.SeedScope(t, ctx, database, scopeID)
	generationID := scopeID + "-g0"
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, generationID, old)
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT 'prescreen/' || $1 || '/' || i, $1, $2, 'repository', 'k' || i, 'git', 'k' || i, now(), now(), '{}'::jsonb
FROM generate_series(1, $3) AS i`,
		scopeID, generationID, facts,
	); err != nil {
		t.Fatalf("seed facts: %v", err)
	}
}

// generationRetentionPrescreenSeededSeqScan is a guard-sensitivity fixture
// only, never shipped: it mechanically replaces the pre-screen's indexed
// (scope_id, generation_id) join with an unindexed payload filter, so the
// fact_records access must become a sequential scan. Derived from the shipped
// constant by one exact substring replacement, not hand-copied, so it tracks
// the production query's other text.
var generationRetentionPrescreenSeededSeqScan = mustSeedPrescreenSeqScan(generationRetentionPrescreenQuery)

func mustSeedPrescreenSeqScan(shipped string) string {
	seeded := strings.Replace(shipped, `ON row.scope_id = generation.scope_id
 AND row.generation_id = candidate.generation_id`, `ON row.payload::text LIKE '%seeded-prescreen-violation%'`, 1)
	if seeded == shipped {
		panic("mustSeedPrescreenSeqScan: fact_records join text not found in the shipped query")
	}
	return seeded
}

// TestGenerationRetentionPrescreenProbesFactIndexLive pins the pre-screen's
// access path: every fact_records scan in its plan must be index-backed, so
// the pre-screen stays a cheap probe at any table size. The seeded
// payload-filter variant must make the same guard fail, proving the guard
// can see the sequential scan it exists to catch.
func TestGenerationRetentionPrescreenProbesFactIndexLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	old := time.Now().UTC().Add(-10 * 24 * time.Hour)

	seedPrescreenGeneration(t, ctx, database, "ps-plan", old, 2000)
	if _, err := database.ExecContext(ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	plan := retentionExplainJSON(t, ctx, database,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+generationRetentionPrescreenQuery, []string{"ps-plan-g0"})
	if violations := retentionFactRecordsSeqScanViolations(plan); len(violations) != 0 {
		t.Fatalf("pre-screen plan scans fact_records without an index: %v", violations)
	}

	seeded := retentionExplainJSON(t, ctx, database,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+generationRetentionPrescreenSeededSeqScan, []string{"ps-plan-g0"})
	if violations := retentionFactRecordsSeqScanViolations(seeded); len(violations) == 0 {
		t.Fatal("seeded payload-filter pre-screen shows no sequential scan; the guard cannot see the defect it exists to catch")
	}
}

// retentionFactRecordsSeqScanViolations walks plan and reports every scan
// node on fact_records that is not index-backed.
func retentionFactRecordsSeqScanViolations(plan map[string]any) []string {
	var violations []string
	walkRetentionPlanNodes(plan, func(node map[string]any) {
		relation, _ := node["Relation Name"].(string)
		if relation != "fact_records" {
			return
		}
		nodeType, _ := node["Node Type"].(string)
		if !strings.Contains(nodeType, "Index") {
			violations = append(violations, "fact_records "+nodeType+", want an index-backed scan")
		}
	})
	return violations
}

// generationRetentionTargetedSeededUnordered is a guard-sensitivity fixture
// only, never shipped: it mechanically strips the targeted lock's ORDER BY,
// so the order-clause guard must flag it. Derived from the shipped constant
// by one exact substring replacement, not hand-copied.
var generationRetentionTargetedSeededUnordered = mustSeedTargetedUnordered(generationRetentionTargetedCandidateQuery)

func mustSeedTargetedUnordered(shipped string) string {
	seeded := strings.Replace(shipped, "ORDER BY generation.scope_id, generation.generation_id\n", "", 1)
	if seeded == shipped {
		panic("mustSeedTargetedUnordered: ORDER BY text not found in the shipped query")
	}
	return seeded
}

// generationRetentionTargetedSeededNoLock is a guard-sensitivity fixture
// only, never shipped: it mechanically strips the targeted lock's FOR
// UPDATE, so the plan must lose its LockRows node. Derived from the shipped
// constant by one exact substring replacement, not hand-copied.
var generationRetentionTargetedSeededNoLock = mustSeedTargetedNoLock(generationRetentionTargetedCandidateQuery)

func mustSeedTargetedNoLock(shipped string) string {
	seeded := strings.Replace(shipped, "FOR UPDATE OF generation, scope SKIP LOCKED\n", "", 1)
	if seeded == shipped {
		panic("mustSeedTargetedNoLock: FOR UPDATE text not found in the shipped query")
	}
	return seeded
}

// TestGenerationRetentionTargetedLockPlanShapeLive pins the re-lock's two
// lock-ordering clauses: ORDER BY (scope_id, generation_id), so two
// overlapping passes lock rows in the same sequence however the planner
// satisfies the order (today an ordered scan on
// scope_generations_scope_generation_idx, no Sort node), and FOR UPDATE ..
// SKIP LOCKED, which must surface as a LockRows plan node. Each seeded
// variant must make its own guard fail.
func TestGenerationRetentionTargetedLockPlanShapeLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	old := now.Add(-10 * 24 * time.Hour)
	cutoff := now.Add(-7 * 24 * time.Hour)
	hardCutoff := now.Add(-90 * 24 * time.Hour)

	for i := 0; i < 3; i++ {
		scopeID := retentionSelectionScopeID("rlock", i)
		testfixtures.SeedScope(t, ctx, database, scopeID)
		testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, scopeID+"-g0", old)
	}
	args := []any{
		cutoff,
		0,
		[]string{},
		[]string{"rlock-00", "rlock-01", "rlock-02"},
		[]string{"rlock-00-g0", "rlock-01-g0", "rlock-02-g0"},
		hardCutoff,
	}

	if violations := targetedLockOrderViolations(generationRetentionTargetedCandidateQuery); len(violations) != 0 {
		t.Fatalf("targeted lock order clauses: %v", violations)
	}
	if violations := targetedLockOrderViolations(generationRetentionTargetedSeededUnordered); len(violations) == 0 {
		t.Fatal("seeded unordered targeted lock passes the order guard; the guard cannot see the defect it exists to catch")
	}

	plan := retentionExplainJSON(t, ctx, database,
		"EXPLAIN (FORMAT JSON) "+generationRetentionTargetedCandidateQuery, args...)
	if !retentionPlanHasNodeType(plan, "LockRows") {
		t.Fatal("targeted lock plan has no LockRows node; FOR UPDATE is gone")
	}
	seeded := retentionExplainJSON(t, ctx, database,
		"EXPLAIN (FORMAT JSON) "+generationRetentionTargetedSeededNoLock, args...)
	if retentionPlanHasNodeType(seeded, "LockRows") {
		t.Fatal("seeded unlocked targeted lock still plans a LockRows node; the guard cannot see the defect it exists to catch")
	}
}

// targetedLockOrderViolations reports the lock-ordering clauses missing from
// sql: the deterministic ORDER BY and the SKIP LOCKED lock clause.
func targetedLockOrderViolations(sql string) []string {
	var violations []string
	if !strings.Contains(sql, "ORDER BY generation.scope_id, generation.generation_id") {
		violations = append(violations, "no ORDER BY generation.scope_id, generation.generation_id; concurrent passes can lock rows in different orders")
	}
	if !strings.Contains(sql, "FOR UPDATE OF generation, scope SKIP LOCKED") {
		violations = append(violations, "no FOR UPDATE OF generation, scope SKIP LOCKED; the re-lock can block or miss its rows")
	}
	return violations
}

// retentionPlanHasNodeType reports whether plan contains a node of nodeType.
func retentionPlanHasNodeType(plan map[string]any, nodeType string) bool {
	found := false
	walkRetentionPlanNodes(plan, func(node map[string]any) {
		found = found || node["Node Type"] == nodeType
	})
	return found
}

// walkRetentionPlanNodes visits node and every descendant in an EXPLAIN
// (FORMAT JSON) plan tree.
func walkRetentionPlanNodes(node map[string]any, visit func(map[string]any)) {
	visit(node)
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		if childNode, ok := child.(map[string]any); ok {
			walkRetentionPlanNodes(childNode, visit)
		}
	}
}

// retentionExplainJSON runs an EXPLAIN (FORMAT JSON) statement and decodes
// its plan tree.
func retentionExplainJSON(t *testing.T, ctx context.Context, database *sql.DB, statement string, args ...any) map[string]any {
	t.Helper()
	var raw []byte
	if err := database.QueryRowContext(ctx, statement, args...).Scan(&raw); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var root []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &root); err != nil || len(root) != 1 {
		t.Fatalf("decode plan: %v", err)
	}
	return root[0].Plan
}

// generationRetentionTargetedQueryBlockingLock derives, from the shipped
// constant, the targeted re-lock's exact lock clause with SKIP LOCKED
// removed, so the race case below forces the EvalPlanQual recheck
// deterministically (the same technique as
// generationRetentionCandidateQueryBlockingLock in the P5 proof).
var generationRetentionTargetedQueryBlockingLock = strings.Replace(
	generationRetentionTargetedCandidateQuery,
	"FOR UPDATE OF generation, scope SKIP LOCKED",
	"FOR UPDATE OF generation, scope",
	1,
)

func init() {
	if generationRetentionTargetedQueryBlockingLock == generationRetentionTargetedCandidateQuery {
		panic("generationRetentionTargetedQueryBlockingLock: shipped query text changed, the derived blocking-lock mirror no longer differs")
	}
}

// TestGenerationRetentionTargetedLockEvalPlanQualDropsRacedMembersLive is the
// multi-row targeted re-lock's EvalPlanQual coverage: a member that is
// deleted-and-committed, or reactivated-and-committed, by another session
// between the re-lock's snapshot and its row lock must be dropped, while an
// untouched bystander member of the same set is still locked.
func TestGenerationRetentionTargetedLockEvalPlanQualDropsRacedMembersLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	cutoff := now.Add(-7 * 24 * time.Hour)
	hardCutoff := now.Add(-90 * 24 * time.Hour)
	old := now.Add(-10 * 24 * time.Hour)

	t.Run("pruned-and-committed-by-another-session", func(t *testing.T) {
		scopeID, racedID, bystanderID := "epq-rlock-del", "epq-rlock-del-g0", "epq-rlock-del-g1"
		seedTargetedRaceSet(t, ctx, database, scopeID, racedID, bystanderID, old)

		locked := runTargetedLockRaceCase(t, ctx, database, scopeID, racedID, bystanderID, cutoff, hardCutoff, func(holder *sql.Tx) {
			if _, err := holder.ExecContext(context.Background(), `DELETE FROM scope_generations WHERE generation_id = $1`, racedID); err != nil {
				t.Fatalf("concurrent delete: %v", err)
			}
		})
		if len(locked) != 1 || locked[0] != bystanderID {
			t.Fatalf("locked rows = %v, want [%s]: the deleted member must be dropped by EvalPlanQual", locked, bystanderID)
		}
	})

	t.Run("reactivated-by-another-session", func(t *testing.T) {
		scopeID, racedID, bystanderID := "epq-rlock-react", "epq-rlock-react-g0", "epq-rlock-react-g1"
		seedTargetedRaceSet(t, ctx, database, scopeID, racedID, bystanderID, old)

		// 'pending', not 'active': scope_generations_active_scope_idx allows
		// only one 'active' row per scope (the P5 proof's own note).
		locked := runTargetedLockRaceCase(t, ctx, database, scopeID, racedID, bystanderID, cutoff, hardCutoff, func(holder *sql.Tx) {
			if _, err := holder.ExecContext(context.Background(), `UPDATE scope_generations SET status = 'pending' WHERE generation_id = $1`, racedID); err != nil {
				t.Fatalf("concurrent reactivate: %v", err)
			}
		})
		if len(locked) != 1 || locked[0] != bystanderID {
			t.Fatalf("locked rows = %v, want [%s]: the reactivated member must be dropped by EvalPlanQual", locked, bystanderID)
		}
	})
}

// seedTargetedRaceSet seeds scopeID with two old superseded generations: the
// raced member and an untouched bystander.
func seedTargetedRaceSet(t *testing.T, ctx context.Context, database *sql.DB, scopeID, racedID, bystanderID string, old time.Time) {
	t.Helper()
	testfixtures.SeedScope(t, ctx, database, scopeID)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, racedID, old)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, bystanderID, old.Add(-time.Hour))
}

// runTargetedLockRaceCase holds racedID's row FOR UPDATE from a second
// connection, starts the blocking-lock mirror of the targeted re-lock over
// the (raced, bystander) set (which must then block waiting for that row),
// lets it settle into the wait, applies race (the concurrent write) and
// commits the holder, then waits for the mirror query and returns the
// generation ids it ended up locking.
func runTargetedLockRaceCase(t *testing.T, ctx context.Context, database *sql.DB, scopeID, racedID, bystanderID string, cutoff, hardCutoff time.Time, race func(holder *sql.Tx)) []string {
	t.Helper()
	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	if _, err := holder.ExecContext(ctx, `SELECT generation_id FROM scope_generations WHERE generation_id = $1 FOR UPDATE`, racedID); err != nil {
		t.Fatalf("holder lock: %v", err)
	}

	var (
		wg      sync.WaitGroup
		locked  []string
		lockErr error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			lockErr = err
			return
		}
		defer func() { _ = tx.Rollback() }()
		rows, err := tx.QueryContext(ctx, generationRetentionTargetedQueryBlockingLock,
			cutoff, 0, []string{}, []string{scopeID}, []string{racedID, bystanderID}, hardCutoff) // blocks on the holder
		if err != nil {
			lockErr = err
			return
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var scope, gotGenerationID, scopeKind string
			var supersededAt, observedAt time.Time
			if err := rows.Scan(&scope, &gotGenerationID, &scopeKind, &supersededAt, &observedAt); err != nil {
				lockErr = err
				return
			}
			locked = append(locked, gotGenerationID)
		}
		lockErr = rows.Err()
	}()

	// Poll for the mirror's own backend actually waiting on the holder's row
	// lock: a fixed sleep could pass without the mirror ever reaching its
	// blocking lock, and the assertions would then pass without exercising
	// the EvalPlanQual recheck (the P5 proof's own note).
	waitForMirrorLockWait(t, ctx, database)
	race(holder)
	if err := holder.Commit(); err != nil {
		t.Fatalf("holder commit: %v", err)
	}

	wg.Wait()
	if lockErr != nil {
		t.Fatalf("mirror query: %v", lockErr)
	}
	return locked
}
