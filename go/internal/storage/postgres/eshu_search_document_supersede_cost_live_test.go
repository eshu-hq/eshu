// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/eshusearch"
)

// supersedeCostEnv opts into the #7458 cost proof. It needs a Postgres that
// preloads pg_stat_statements, so it never runs by default.
const supersedeCostEnv = "ESHU_7458_COST_PROOF"

const (
	supersedeCostFiles  = 4000
	supersedeCostRounds = 5
)

// seedSupersedeCostFixture reuses the interleave proof's scope and generation
// names but seeds supersedeCostFiles content_files of about 0.5 KiB each: the
// 4,000-file burst size, which the 256-row file page splits into 16 pages.
func seedSupersedeCostFixture(ctx context.Context, t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	if _, err := sqlDB.ExecContext(ctx, supersedeProofSeed); err != nil {
		t.Fatalf("seed proof fixture: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `DELETE FROM content_files WHERE repo_id = 'repo-7458'`); err != nil {
		t.Fatalf("clear seeded files: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
SELECT 'repo-7458', 'src/f' || lpad(i::text, 5, '0') || '.go',
       'package f' || i || E'\n' || repeat(E'func Handler() { return }\n', 20), md5(i::text), 21, 'go', now()
  FROM generate_series(1, $1::int) AS i`, supersedeCostFiles); err != nil {
		t.Fatalf("seed cost files: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, "ANALYZE content_files"); err != nil {
		t.Fatalf("analyze content_files: %v", err)
	}
}

// resetSupersedeCostGenerations drops both generations (cascading every
// derived row) and re-creates G active and H pending, so each arm starts from
// the same storage state.
func resetSupersedeCostGenerations(ctx context.Context, t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	if _, err := sqlDB.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = 'scope-7458';
DELETE FROM fact_work_items WHERE scope_id = 'scope-7458';
DELETE FROM scope_generations WHERE scope_id = 'scope-7458';
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ('gen-7458-g', 'scope-7458', 'snapshot', now() - interval '2 hours', now() - interval '2 hours', 'active', now() - interval '2 hours');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-7458-h', 'scope-7458', 'snapshot', now() - interval '1 hour', now() - interval '1 hour', 'pending');
UPDATE ingestion_scopes SET active_generation_id = 'gen-7458-g' WHERE scope_id = 'scope-7458';`); err != nil {
		t.Fatalf("reset generations: %v", err)
	}
}

// costArm is one measured handler run.
type costArm struct {
	wall, afterSupersede time.Duration
	statements           int64
	execMS               float64
	freshCalls           int64
	freshExecMS          float64
	freshMaxMS           float64
	checkLatency         []time.Duration
	status               reducercontract.ResultStatus
	docsWritten          int
	deleteStatements     int64
}

func resetStatementStats(ctx context.Context, t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	if _, err := sqlDB.ExecContext(ctx, "SELECT pg_stat_statements_reset()"); err != nil {
		t.Fatalf("pg_stat_statements_reset: %v", err)
	}
}

// readStatementStats totals every statement recorded since the last reset,
// excluding the harness's own pg_stat_statements reads.
func readStatementStats(ctx context.Context, t *testing.T, sqlDB *sql.DB, arm *costArm) {
	t.Helper()
	if err := sqlDB.QueryRowContext(ctx, `
SELECT COALESCE(sum(calls), 0)::bigint, COALESCE(sum(total_exec_time), 0),
       COALESCE(sum(calls) FILTER (WHERE query ILIKE 'DELETE FROM%'), 0)::bigint,
       COALESCE(sum(calls) FILTER (WHERE query ILIKE '%intent_is_newer%'), 0)::bigint,
       COALESCE(sum(total_exec_time) FILTER (WHERE query ILIKE '%intent_is_newer%'), 0),
       COALESCE(max(max_exec_time) FILTER (WHERE query ILIKE '%intent_is_newer%'), 0)
FROM pg_stat_statements WHERE query NOT ILIKE '%pg_stat_statements%'`).Scan(
		&arm.statements, &arm.execMS, &arm.deleteStatements,
		&arm.freshCalls, &arm.freshExecMS, &arm.freshMaxMS); err != nil {
		t.Fatalf("read pg_stat_statements: %v", err)
	}
}

// runSupersedeCostArm runs one handler pass. fenced=false injects an
// always-current check, which is the pre-fix handler's database behavior (no
// freshness statement); fenced=true uses the real freshness check. supersede
// activates H after page 1 and restarts the statement window at that point.
func runSupersedeCostArm(ctx context.Context, t *testing.T, sqlDB *sql.DB, fenced, supersede bool) costArm {
	t.Helper()
	resetSupersedeCostGenerations(ctx, t, sqlDB)
	database := SQLDB{DB: sqlDB}
	var arm costArm
	check := func(context.Context, string, string) (bool, error) { return true, nil }
	if fenced {
		real := NewGenerationFreshnessCheck(database)
		check = func(c context.Context, scopeID, generationID string) (bool, error) {
			started := time.Now()
			current, err := real(c, scopeID, generationID)
			arm.checkLatency = append(arm.checkLatency, time.Since(started))
			return current, err
		}
	}
	var supersededAt time.Time
	loader := &activatingLoader{inner: NewEshuSearchDocumentSourceLoader(database)}
	if supersede {
		loader.onFirstPage = func() error {
			if err := activateSupersedeProofGenerationH(ctx, sqlDB); err != nil {
				return err
			}
			resetStatementStats(ctx, t, sqlDB)
			supersededAt = time.Now()
			return nil
		}
	}
	handler := eshusearch.EshuSearchDocumentHandler{
		Loader: loader,
		Writer: eshusearch.PostgresEshuSearchDocumentWriter{
			DB: database, ProjectionState: NewEshuSearchDocumentProjectionStateStore(database),
		},
		GenerationCheck: check,
	}
	if !supersede {
		resetStatementStats(ctx, t, sqlDB)
	}
	started := time.Now()
	result, err := handler.Handle(ctx, reducercontract.Intent{
		IntentID: "cost-g", ScopeID: supersedeProofScope, GenerationID: supersedeProofGenG,
		SourceSystem: "git", Domain: eshusearch.DomainEshuSearchDocument,
	})
	finished := time.Now()
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	arm.wall = finished.Sub(started)
	if supersede {
		arm.afterSupersede = finished.Sub(supersededAt)
	}
	arm.status = result.Status
	arm.docsWritten = result.CanonicalWrites
	readStatementStats(ctx, t, sqlDB, &arm)
	return arm
}

func (a costArm) String() string {
	return fmt.Sprintf("wall=%.1fms after_supersede=%.1fms statements=%d exec=%.1fms deletes=%d fresh_calls=%d fresh_exec=%.3fms fresh_max=%.3fms status=%s docs=%d",
		float64(a.wall.Microseconds())/1000, float64(a.afterSupersede.Microseconds())/1000,
		a.statements, a.execMS, a.deleteStatements, a.freshCalls, a.freshExecMS, a.freshMaxMS, a.status, a.docsWritten)
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted))*p+0.999999) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// interleavedCostRounds runs supersedeCostRounds rounds of before/after with
// the first mover alternating each round, logging every arm.
func interleavedCostRounds(ctx context.Context, t *testing.T, sqlDB *sql.DB, label string, supersede bool) (before, after []costArm) {
	t.Helper()
	for round := 0; round < supersedeCostRounds; round++ {
		order := []bool{false, true} // fenced=false is "before"
		if round%2 == 1 {
			order = []bool{true, false}
		}
		for _, fenced := range order {
			arm := runSupersedeCostArm(ctx, t, sqlDB, fenced, supersede)
			name := "before"
			if fenced {
				name = "after"
				after = append(after, arm)
			} else {
				before = append(before, arm)
			}
			t.Logf("%s round=%d first=%s arm=%s %s", label, round, map[bool]string{false: "before", true: "after"}[order[0]], name, arm)
		}
	}
	return before, after
}

func medianFloat(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}

func armMedians(arms []costArm, pick func(costArm) float64) float64 {
	values := make([]float64, 0, len(arms))
	for _, arm := range arms {
		values = append(values, pick(arm))
	}
	return medianFloat(values)
}

// TestEshuSearchDocumentSupersedeCostLive measures #7458 proofs 3b and 3c on a
// 4,000-file fixture against a Postgres that preloads pg_stat_statements:
//
//	ESHU_7458_COST_PROOF=1 ESHU_POSTGRES_TEST_DSN=postgresql://.../eshu \
//	go test ./internal/storage/postgres -run TestEshuSearchDocumentSupersedeCostLive -count=1 -v
func TestEshuSearchDocumentSupersedeCostLive(t *testing.T) {
	if os.Getenv(supersedeCostEnv) != "1" {
		t.Skipf("set %s=1 (and ESHU_POSTGRES_TEST_DSN to a pg_stat_statements database) to run the #7458 cost proof", supersedeCostEnv)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	sqlDB, _ := openServiceLineageSchemaLive(ctx, t, "eshu_7458_cost")
	if err := ApplyBootstrap(ctx, SQLDB{DB: sqlDB}); err != nil {
		t.Fatalf("ApplyBootstrap: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements WITH SCHEMA public"); err != nil {
		t.Fatalf("pg_stat_statements: %v", err)
	}
	seedSupersedeCostFixture(ctx, t, sqlDB)
	runSupersedeCostArm(ctx, t, sqlDB, true, false) // warm caches and plans

	t.Run("3b non-superseded overhead", func(t *testing.T) {
		before, after := interleavedCostRounds(ctx, t, sqlDB, "3b", false)
		var lat []time.Duration
		for _, arm := range after {
			lat = append(lat, arm.checkLatency...)
		}
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		wallB := armMedians(before, func(a costArm) float64 { return float64(a.wall.Microseconds()) / 1000 })
		wallA := armMedians(after, func(a costArm) float64 { return float64(a.wall.Microseconds()) / 1000 })
		stmtB := armMedians(before, func(a costArm) float64 { return float64(a.statements) })
		stmtA := armMedians(after, func(a costArm) float64 { return float64(a.statements) })
		freshMS := armMedians(after, func(a costArm) float64 { return a.freshExecMS })
		t.Logf("3b SUMMARY median wall before=%.1fms after=%.1fms delta=%.1fms; median statements before=%.0f after=%.0f added=%.0f; freshness exec per item=%.3fms (%.4f%% of item wall)",
			wallB, wallA, wallA-wallB, stmtB, stmtA, stmtA-stmtB, freshMS, 100*freshMS/wallB)
		t.Logf("3b SUMMARY per-check client latency over %d checks p50=%s p99=%s max=%s",
			len(lat), percentile(lat, 0.5), percentile(lat, 0.99), lat[len(lat)-1])
		if stmtA-stmtB != 17 {
			t.Errorf("added statements = %.0f, want pages+1 = 17", stmtA-stmtB)
		}
	})

	t.Run("3c superseded saving", func(t *testing.T) {
		before, after := interleavedCostRounds(ctx, t, sqlDB, "3c", true)
		pick := map[string]func(costArm) float64{
			"statements after supersede": func(a costArm) float64 { return float64(a.statements) },
			"DELETE statements after":    func(a costArm) float64 { return float64(a.deleteStatements) },
			"statement exec ms after":    func(a costArm) float64 { return a.execMS },
			"wall ms after supersede":    func(a costArm) float64 { return float64(a.afterSupersede.Microseconds()) / 1000 },
			"handler wall ms":            func(a costArm) float64 { return float64(a.wall.Microseconds()) / 1000 },
			"documents written":          func(a costArm) float64 { return float64(a.docsWritten) },
		}
		keys := make([]string, 0, len(pick))
		for k := range pick {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			t.Logf("3c SUMMARY median %-28s before=%.1f after=%.1f", k, armMedians(before, pick[k]), armMedians(after, pick[k]))
		}
		if after[0].status != reducercontract.ResultStatusSuperseded || before[0].status != reducercontract.ResultStatusSucceeded {
			t.Errorf("statuses before=%s after=%s, want succeeded / superseded", before[0].status, after[0].status)
		}
	})

	t.Run("3c scope-key wait", func(t *testing.T) {
		for round := 0; round < supersedeCostRounds; round++ {
			order := []bool{false, true}
			if round%2 == 1 {
				order = []bool{true, false}
			}
			for _, fenced := range order {
				measureSupersedeScopeKeyWait(ctx, t, sqlDB, round, fenced)
			}
		}
	})

	t.Run("per-check latency loop", func(t *testing.T) {
		// Alternates the real check with a trivial control round trip
		// (SELECT 1) on the same pool, so host and scheduler noise shows up
		// in both series and the check's own cost is the difference.
		check := NewGenerationFreshnessCheck(SQLDB{DB: sqlDB})
		resetSupersedeCostGenerations(ctx, t, sqlDB)
		checks := make([]time.Duration, 0, 5000)
		controls := make([]time.Duration, 0, 5000)
		for i := 0; i < 5000; i++ {
			started := time.Now()
			if _, err := check(ctx, supersedeProofScope, supersedeProofGenG); err != nil {
				t.Fatalf("check: %v", err)
			}
			checks = append(checks, time.Since(started))
			started = time.Now()
			var one int
			if err := sqlDB.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
				t.Fatalf("control: %v", err)
			}
			controls = append(controls, time.Since(started))
		}
		for name, lat := range map[string][]time.Duration{"check": checks, "control SELECT 1": controls} {
			sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
			t.Logf("LOOP SUMMARY %-16s n=%d p50=%s p90=%s p99=%s max=%s", name, len(lat),
				percentile(lat, 0.5), percentile(lat, 0.9), percentile(lat, 0.99), lat[len(lat)-1])
		}
	})
}

// measureSupersedeScopeKeyWait drives the real reducer queue: G is claimed and
// run, H is enqueued when it activates after page 1, and a poller claims for H.
// H shares G's scope conflict key, so its claim is refused until G acks; the
// reported waits are activation to H's claim and G's ack to H's claim.
func measureSupersedeScopeKeyWait(ctx context.Context, t *testing.T, sqlDB *sql.DB, round int, fenced bool) {
	t.Helper()
	resetSupersedeCostGenerations(ctx, t, sqlDB)
	database := SQLDB{DB: sqlDB}
	queueG := NewReducerQueue(database, "owner-g", time.Minute)
	queueH := NewReducerQueue(database, "owner-h", time.Minute)
	enqueue := func(gen string) {
		if _, err := queueG.Enqueue(ctx, []projectorruntime.ReducerIntent{{
			ScopeID: supersedeProofScope, GenerationID: gen, Domain: eshusearch.DomainEshuSearchDocument,
			EntityKey: "eshu_search_document:" + supersedeProofScope, Reason: "cost proof", SourceSystem: "git",
		}}); err != nil {
			t.Fatalf("enqueue %s: %v", gen, err)
		}
	}
	enqueue(supersedeProofGenG)
	gIntent, ok, err := queueG.Claim(ctx)
	if err != nil || !ok || gIntent.GenerationID != supersedeProofGenG {
		t.Fatalf("claim G = %+v ok=%v err=%v", gIntent, ok, err)
	}

	check := func(context.Context, string, string) (bool, error) { return true, nil }
	if fenced {
		check = NewGenerationFreshnessCheck(database)
	}
	type claimed struct {
		at      time.Time
		refused int
	}
	claimedH := make(chan claimed, 1)
	var activatedAt time.Time
	loader := &activatingLoader{inner: NewEshuSearchDocumentSourceLoader(database)}
	loader.onFirstPage = func() error {
		if err := activateSupersedeProofGenerationH(ctx, sqlDB); err != nil {
			return err
		}
		activatedAt = time.Now()
		enqueue(supersedeProofGenH)
		go func() {
			refused := 0
			for {
				intent, ok, err := queueH.Claim(ctx)
				if err == nil && ok && intent.GenerationID == supersedeProofGenH {
					claimedH <- claimed{at: time.Now(), refused: refused}
					return
				}
				refused++
				select {
				case <-ctx.Done():
					return
				case <-time.After(20 * time.Millisecond):
				}
			}
		}()
		return nil
	}
	handler := eshusearch.EshuSearchDocumentHandler{
		Loader: loader,
		Writer: eshusearch.PostgresEshuSearchDocumentWriter{
			DB: database, ProjectionState: NewEshuSearchDocumentProjectionStateStore(database),
		},
		GenerationCheck: check,
	}
	result, err := handler.Handle(ctx, gIntent)
	if err != nil {
		t.Fatalf("Handle G: %v", err)
	}
	if err := queueG.Ack(ctx, gIntent, result); err != nil {
		t.Fatalf("Ack G: %v", err)
	}
	ackedAt := time.Now()
	select {
	case c := <-claimedH:
		t.Logf("3c-wait round=%d fenced=%v G_status=%s activation_to_H_claim=%.0fms G_ack_to_H_claim=%.0fms refused_polls_before_claim=%d",
			round, fenced, result.Status, float64(c.at.Sub(activatedAt).Milliseconds()),
			float64(c.at.Sub(ackedAt).Milliseconds()), c.refused)
	case <-time.After(30 * time.Second):
		t.Fatal("H was never claimed after G acked")
	}
}
