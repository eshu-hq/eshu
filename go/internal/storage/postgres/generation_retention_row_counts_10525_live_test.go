// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10525 disposable PostgreSQL proof that the retention row-count fact_work_items arm and the pre-delete work-item lock stay on the migration 294 generation index (custom and forced-generic plans, seeded drop-index REDs, executed lock buffers tracking the batch), that the arm counts exactly the scope_generations cascade envelope including a planted scope-mismatched row, that a missing, INVALID or partial-only index refuses the cycle, and that a foreign FOR UPDATE hold on a retained work row neither blocks the cycle nor loses its row; bridges ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN.
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// seedRetention10525WorkItems writes perGen succeeded work rows on every
// plan generation (the #10512 corpus plus its filler), so fact_work_items
// holds roughly 50k rows over 224 generations and the 12-generation batch
// is a small slice of the table, as on QA (288,824 rows; the measured
// batch held 1,521). Succeeded rows keep the candidate query's live_work
// exclusion out of this test's way; the count arm counts every status.
func seedRetention10525WorkItems(t *testing.T, ctx context.Context, database *sql.DB, perGen int) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
SELECT generation.generation_id || '-work-' || n, generation.scope_id, generation.generation_id,
    'reducer', 'domain', 'succeeded', now(), now()
FROM scope_generations AS generation CROSS JOIN generate_series(1, $1::int) AS n
WHERE generation.scope_id LIKE 'plan-scope-%' OR generation.scope_id LIKE 'plan-fill-scope-%'`, perGen); err != nil {
		t.Fatalf("seed work items: %v", err)
	}
}

// executeRetentionLockAnalyzed runs the pre-delete work-item lock under
// EXPLAIN (ANALYZE, BUFFERS) in a rolled-back transaction and returns the
// locked row count and the plan root's shared buffers (hit plus read).
func executeRetentionLockAnalyzed(t *testing.T, ctx context.Context, database *sql.DB, candidates []string) (int64, int64) {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	var plan []byte
	if err := tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+lockPrunedGenerationWorkItemsQuery, candidates).Scan(&plan); err != nil {
		t.Fatalf("explain analyze lock: %v", err)
	}
	var root []struct {
		Plan struct {
			ActualRows float64 `json:"Actual Rows"`
			SharedHit  int64   `json:"Shared Hit Blocks"`
			SharedRead int64   `json:"Shared Read Blocks"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal(plan, &root); err != nil || len(root) != 1 {
		t.Fatalf("decode analyzed lock plan: %v\n%s", err, plan)
	}
	return int64(root[0].Plan.ActualRows), root[0].Plan.SharedHit + root[0].Plan.SharedRead
}

// TestGenerationRetentionWorkItemsPlansStayOnGenerationIndexLive fails
// when the full row-count query or the pre-delete work-item lock reads
// fact_work_items by a path whose cost grows with the table, under
// custom and forced-generic plans (#10525, migration 294). The row-count
// statement accesses fact_work_items twice, so every access must be a
// bound generation probe, not only one of them. Seeded
// drop-index REDs prove the guard sees a lost probe, and the executed
// lock must touch buffers that track the batch while the filler
// generations hold far more rows outside the batch.
func TestGenerationRetentionWorkItemsPlansStayOnGenerationIndexLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	// The full row-count statement also probes the #10512 guarded tables,
	// and the shared guard flags a sequential scan of any of them, so this
	// fixture seeds and vacuums them exactly as the #10512 guard does.
	guarded := []string{"fact_work_items", "shared_projection_intents", "package_manifest_consumption_keys", "package_registry_identity_keys"}
	for _, table := range guarded {
		if _, err := database.ExecContext(ctx, "ALTER TABLE "+table+" SET (autovacuum_enabled = false)"); err != nil {
			t.Fatalf("disable autovacuum on %s: %v", table, err)
		}
	}
	seedFactKeyPlanCorpus(t, ctx, database)
	seedRetention10512FillerGenerations(t, ctx, database)
	const perGen = 250
	seedRetention10525WorkItems(t, ctx, database, perGen)
	var candidates []string
	for s := 1; s <= factKeyPlanScopes; s++ {
		for g := 1; g <= 2; g++ {
			candidates = append(candidates, fmt.Sprintf("plan-scope-%d-gen-%d", s, g))
		}
	}
	seedRetention10512Intents(t, ctx, database, candidates, 25, 75)
	// Plain VACUUM, never ANALYZE, before the cold checks, for the
	// visibility-map reason recorded in the #10512 guard of the same shape.
	for _, table := range guarded {
		if _, err := database.ExecContext(ctx, "VACUUM "+table); err != nil {
			t.Fatalf("vacuum %s: %v", table, err)
		}
	}
	const index = "fact_work_items_generation_idx"
	statements := []retentionStatement{
		{"row_counts", generationRetentionRowCountsQuery, []string{index}},
		{"lock_work_items", lockPrunedGenerationWorkItemsQuery, []string{index}},
	}
	check := func(t *testing.T, condition string, explain func(string) []byte) {
		t.Helper()
		for _, statement := range statements {
			plan := explain(statement.sql)
			// Per-access accounting for fact_work_items (#10525): the
			// row-count statement accesses it twice, so one bound probe
			// must not cover a scanning second access.
			violations := append(
				retentionGuardedPlanViolations(t, plan, statement.required),
				retentionWorkItemsPlanViolations10525(t, plan)...)
			if len(violations) > 0 {
				t.Errorf("%s %s plan leaves its generation index: %s\n%s", condition, statement.name, strings.Join(violations, "; "), plan)
			}
		}
	}
	t.Run("cold-custom", func(t *testing.T) {
		check(t, "cold custom", func(statement string) []byte {
			return explainRetentionCustom(t, ctx, database, statement, candidates)
		})
	})
	t.Run("cold-generic", func(t *testing.T) {
		check(t, "cold generic", func(statement string) []byte {
			return explainRetentionGeneric(t, ctx, database, statement, candidates)
		})
	})
	for _, table := range []string{"fact_work_items", "shared_projection_intents", "package_manifest_consumption_keys", "package_registry_identity_keys", "fact_records", "scope_generations"} {
		if _, err := database.ExecContext(ctx, "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
	t.Run("analyzed-generic", func(t *testing.T) {
		check(t, "analyzed generic", func(statement string) []byte {
			return explainRetentionGeneric(t, ctx, database, statement, candidates)
		})
	})
	t.Run("executed-lock-buffers-track-the-batch", func(t *testing.T) {
		rows, buffers := executeRetentionLockAnalyzed(t, ctx, database, candidates)
		wantRows := int64(perGen * len(candidates))
		if rows != wantRows || buffers > 4*rows+64 {
			t.Errorf("work lock locked %d rows (want %d) with %d buffers, want buffers <= 4*rows+64", rows, wantRows, buffers)
		}
		t.Logf("work lock: %d rows, %d buffers", rows, buffers)
	})
	for _, statement := range statements {
		t.Run("seeded-red-missing-index-"+statement.name, func(t *testing.T) {
			conn, err := database.Conn(ctx)
			if err != nil {
				t.Fatalf("open connection: %v", err)
			}
			defer func() { _ = conn.Close() }()
			if _, err := conn.ExecContext(ctx, "BEGIN; DROP INDEX "+index); err != nil {
				t.Fatalf("drop %s: %v", index, err)
			}
			defer func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") }()
			var plan []byte
			if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+statement.sql, candidates).Scan(&plan); err != nil {
				t.Fatalf("explain without %s: %v", index, err)
			}
			violations := append(
				retentionGuardedPlanViolations(t, plan, statement.required),
				retentionWorkItemsPlanViolations10525(t, plan)...)
			if joined := strings.Join(violations, "; "); !strings.Contains(joined, "never probes "+index) {
				t.Fatalf("plan guard accepted %s with no %s (violations: %s):\n%s", statement.name, index, joined, plan)
			}
		})
	}
}

// Boundary markers for deriving the fact_work_items arm from the shipped
// row-count constant (#10525): the first arm's SELECT ends the CTE head,
// and the work arm runs from its SELECT to the next UNION ALL. The
// derivation never hand-copies arm SQL, so it cannot drift from
// production; TestRetentionWorkArmDerivedFromShippedConstant pins that
// the head is a byte-prefix of the constant and the arm a byte-substring
// after it (eshu-postgres-rigor derivation rule).
const (
	retentionRowCountsFirstArmMarker = "\nSELECT candidate.generation_id, 'fact_records' AS table_name"
	retentionWorkArmMarker           = "SELECT candidate.generation_id, 'fact_work_items' AS table_name"
)

// derivedRetentionWorkArmParts splits the shipped constant into its CTE
// head and its fact_work_items arm, or reports false when either marker
// is gone (the shipped shape changed under this test).
func derivedRetentionWorkArmParts() (string, string, bool) {
	prefixEnd := strings.Index(generationRetentionRowCountsQuery, retentionRowCountsFirstArmMarker)
	armStart := strings.Index(generationRetentionRowCountsQuery, retentionWorkArmMarker)
	if prefixEnd < 0 || armStart <= prefixEnd {
		return "", "", false
	}
	rest := generationRetentionRowCountsQuery[armStart:]
	armEnd := strings.Index(rest, "\nUNION ALL")
	if armEnd < 0 {
		return "", "", false
	}
	return generationRetentionRowCountsQuery[:prefixEnd], rest[:armEnd], true
}

// scopeJoinedWorkCounts10525 is the rejected scope-join shape, kept only
// as the exactness counterexample: it misses work rows whose scope_id
// disagrees with their generation's scope, which the generation cascade
// still deletes.
const scopeJoinedWorkCounts10525 = `
WITH candidates AS (SELECT unnest($1::text[]) AS generation_id)
SELECT candidate.generation_id, 'fact_work_items' AS table_name, COUNT(row.generation_id) AS row_count
FROM candidates AS candidate
LEFT JOIN scope_generations AS generation ON generation.generation_id = candidate.generation_id
LEFT JOIN fact_work_items AS row ON row.scope_id = generation.scope_id AND row.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
`

// TestGenerationRetentionWorkCountsMatchCascadeEnvelopeLive proves the
// #10525 exactness contract: the shipped fact_work_items arm, the same
// arm derived from the shipped constant and run alone, and the direct
// cascade-envelope count agree per generation, including a planted
// scope-mismatched work row the cascade deletes on generation_id alone.
// The rejected scope-join shape under-counts that generation by exactly
// the planted row, which is why migration 294 backs the generation-only
// probe instead.
func TestGenerationRetentionWorkCountsMatchCascadeEnvelopeLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scopeA, scopeB = "work-10525-a", "work-10525-b"
	const oldGen, otherGen = "work-10525-old", "work-10525-other"
	testfixtures.SeedScope(t, ctx, database, scopeA)
	testfixtures.SeedScope(t, ctx, database, scopeB)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeA, oldGen, now.Add(-10*24*time.Hour))
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeA, otherGen, now.Add(-9*24*time.Hour))
	insertWork := `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
SELECT $1 || '-' || $2 || '-' || n, $2, $1, 'reducer', 'domain', 'succeeded', now(), now() FROM generate_series(1, $3::int) AS n`
	for _, seed := range []struct {
		gen, scope string
		n          int
	}{{oldGen, scopeA, 3}, {oldGen, scopeB, 1}, {otherGen, scopeA, 2}} {
		if _, err := database.ExecContext(ctx, insertWork, seed.gen, seed.scope, seed.n); err != nil {
			t.Fatalf("seed work rows %s/%s: %v", seed.gen, seed.scope, err)
		}
	}
	candidates := []string{oldGen, otherGen}
	want := map[string]int64{oldGen: 4, otherGen: 2}

	full := retentionCountsWith(t, ctx, database, generationRetentionRowCountsQuery, candidates)
	prefix, arm, ok := derivedRetentionWorkArmParts()
	if !ok {
		t.Fatal("derive work arm from shipped constant: markers not found")
	}
	derived := retentionCountsWith(t, ctx, database, prefix+"\n"+arm, candidates)
	scopeJoined := retentionCountsWith(t, ctx, database, scopeJoinedWorkCounts10525, candidates)
	for _, gen := range candidates {
		key := gen + "|fact_work_items"
		if full[key] != want[gen] {
			t.Errorf("shipped %s = %d, want cascade envelope %d", key, full[key], want[gen])
		}
		if derived[key] != full[key] {
			t.Errorf("derived-alone %s = %d, want shipped %d", key, derived[key], full[key])
		}
	}
	if got := full[oldGen+"|fact_work_items"] - scopeJoined[oldGen+"|fact_work_items"]; got != 1 {
		t.Errorf("scope-join under-count on planted mismatch = %d, want exactly 1", got)
	}
	if scopeJoined[otherGen+"|fact_work_items"] != want[otherGen] {
		t.Errorf("scope-join on clean generation = %d, want %d", scopeJoined[otherGen+"|fact_work_items"], want[otherGen])
	}
}

// TestGenerationRetentionRefusesWithoutValidWorkItemsGenerationIndexLive
// extends the #7279 refusal to migration 294 (#10525): the count arm and
// pre-delete lock probe fact_work_items by generation_id, so a dropped
// index, an INVALID one (what a failed concurrent build leaves) and a
// partial index of the right name each refuse the cycle, name the index,
// and prune nothing. Restoring the index lets the next cycle run.
func TestGenerationRetentionRefusesWithoutValidWorkItemsGenerationIndexLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scope, oldGen = "work-refuse", "work-refuse-old"
	testfixtures.SeedScope(t, ctx, database, scope)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scope, oldGen, now.Add(-10*24*time.Hour))
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT md5($2 || '-' || n), $1, $2, 'content_entity', 'k-' || n, 'git', 'k-' || n, now(), now(), '{}'::jsonb
FROM generate_series(1, 50) AS n`, scope, oldGen); err != nil {
		t.Fatalf("seed facts: %v", err)
	}
	// Duplicate generation ids on work rows make the seeded unique
	// concurrent build below fail, leaving indisvalid = false.
	if _, err := database.ExecContext(ctx, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
SELECT $2 || '-work-' || n, $1, $2, 'reducer', 'domain', 'succeeded', now(), now()
FROM generate_series(1, 5) AS n`, scope, oldGen); err != nil {
		t.Fatalf("seed work rows: %v", err)
	}
	const index = "fact_work_items_generation_idx"
	restore := `CREATE INDEX CONCURRENTLY ` + index + ` ON fact_work_items (generation_id)`
	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	for _, tc := range []struct {
		name  string
		setup []string
	}{
		{"work-dropped", []string{"DROP INDEX " + index}},
		{"work-invalid", []string{
			"DROP INDEX " + index,
			`CREATE UNIQUE INDEX CONCURRENTLY ` + index + ` ON fact_work_items (generation_id)`,
		}},
		{"work-partial-only", []string{
			"DROP INDEX " + index,
			`CREATE INDEX ` + index + ` ON fact_work_items (generation_id) WHERE status = 'succeeded'`,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, statement := range tc.setup {
				_, err := database.ExecContext(ctx, statement)
				expectedFailure := tc.name == "work-invalid" && i == 1
				if err != nil && !expectedFailure {
					t.Fatalf("setup %q: %v", statement, err)
				}
			}
			_, err := store.PruneSupersededGenerations(ctx, chunkDrainPolicy())
			if !errors.Is(err, ErrGenerationRetentionKeyIndexUnavailable) || !strings.Contains(err.Error(), index) {
				t.Fatalf("PruneSupersededGenerations() error = %v, want ErrGenerationRetentionKeyIndexUnavailable naming %s", err, index)
			}
			if remaining := remainingScopeGenerations(t, ctx, database, []string{oldGen}); !remaining[oldGen] {
				t.Fatal("refused cycle pruned the superseded generation")
			}
			for _, statement := range []string{"DROP INDEX IF EXISTS " + index, restore} {
				if _, err := database.ExecContext(ctx, statement); err != nil {
					t.Fatalf("restore %s: %v", index, err)
				}
			}
		})
	}
	result, err := store.PruneSupersededGenerations(ctx, chunkDrainPolicy())
	if err != nil || result.GenerationsPruned != 1 {
		t.Fatalf("PruneSupersededGenerations() after restoring the index = %d pruned, %v; want 1, nil", result.GenerationsPruned, err)
	}
}

// TestGenerationRetentionWorkIndexCycleIgnoresHeldForeignWorkLockLive is
// the #10525 concurrency proof: the count takes no fact_work_items row
// lock and the pre-delete lock covers only the pruned generations, so a
// foreign session holding FOR UPDATE on a retained generation's work row
// (the reducer claim shape) neither blocks the cycle nor loses its row,
// and the pruned generation's work rows still cascade away exactly.
func TestGenerationRetentionWorkIndexCycleIgnoresHeldForeignWorkLockLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scope, oldGen, keptGen = "work-conc", "work-conc-old", "work-conc-kept"
	testfixtures.SeedScope(t, ctx, database, scope)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scope, oldGen, now.Add(-10*24*time.Hour))
	testfixtures.SeedSupersededGeneration(t, ctx, database, scope, keptGen, now.Add(-time.Hour))
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT md5($2 || '-' || n), $1, $2, 'content_entity', 'k-' || n, 'git', 'k-' || n, now(), now(), '{}'::jsonb
FROM generate_series(1, 50) AS n`, scope, oldGen); err != nil {
		t.Fatalf("seed facts: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at) VALUES
('work-conc-old-1', $1, $2, 'reducer', 'domain', 'succeeded', now(), now()),
('work-conc-kept-1', $1, $3, 'reducer', 'domain', 'running', now(), now())`, scope, oldGen, keptGen); err != nil {
		t.Fatalf("seed work rows: %v", err)
	}
	holder, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open holder connection: %v", err)
	}
	defer func() { _ = holder.Close() }()
	if _, err := holder.ExecContext(ctx, `BEGIN; SELECT work_item_id FROM fact_work_items WHERE work_item_id = 'work-conc-kept-1' FOR UPDATE`); err != nil {
		t.Fatalf("hold foreign work lock: %v", err)
	}
	defer func() { _, _ = holder.ExecContext(context.Background(), "ROLLBACK") }()

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	type outcome struct {
		result GenerationRetentionResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := store.PruneSupersededGenerations(ctx, chunkDrainPolicy())
		done <- outcome{result, err}
	}()
	select {
	case got := <-done:
		if got.err != nil || got.result.GenerationsPruned != 1 {
			t.Fatalf("PruneSupersededGenerations() under held foreign lock = %d pruned, %v; want 1, nil", got.result.GenerationsPruned, got.err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("retention cycle blocked on a foreign fact_work_items row lock")
	}
	var kept, pruned int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE work_item_id = 'work-conc-kept-1'`).Scan(&kept); err != nil {
		t.Fatalf("count kept work row: %v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE work_item_id = 'work-conc-old-1'`).Scan(&pruned); err != nil {
		t.Fatalf("count pruned work row: %v", err)
	}
	if kept != 1 || pruned != 0 {
		t.Fatalf("work rows after prune: kept = %d (want 1), pruned-generation = %d (want 0)", kept, pruned)
	}
}
