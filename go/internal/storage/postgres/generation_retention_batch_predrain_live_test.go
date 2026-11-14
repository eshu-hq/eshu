// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10020 retention ScopeLockHold regression: 3 superseded generations (40,000 file facts each, all under BatchRowLimit) must drain pre-Begin so one pass prunes all three with ScopeLockHold <= 5s; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_POSTGRES_TEST_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestGenerationRetentionBatchPredrainKeepsScopeLockBoundedLive is the
// failing-first regression for the #10020 whole-prune hold breach: three
// normal (non-over-limit) superseded generations hold 120,000 facts, so the
// pre-Begin predrain must drain them in committed batches and the pass must
// prune all three with the scope lock held at most five seconds. Before the
// batch predrain the pass deletes the volume under the lock (ChunkPredeleteBatches
// stays 0 and the hold follows the cascade); after it the facts are gone
// before Begin and the hold covers only generation rows and small children.
func TestGenerationRetentionBatchPredrainKeepsScopeLockBoundedLive(t *testing.T) {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_GENERATION_RETENTION_PROOF_DSN is set without ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	database, ctx := openGenerationRetentionMigratedSchema(t)
	seedBatchPredrainFixture(t, ctx, database)

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	start := time.Now()
	result, err := store.PruneSupersededGenerations(ctx, GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         time.Hour,
		BatchGenerationLimit:     100,
		BatchRowLimit:            200_000,
		PolicyScope:              "global",
		PolicyRevision:           "batch-predrain-proof",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	t.Logf(
		"Performance Evidence: pruned_generations=%d fact_rows=%d predrain_batches=%d scope_lock_hold=%s duration=%s wall_time=%s",
		result.GenerationsPruned,
		result.RowsPruned["fact_records"],
		result.ChunkPredeleteBatches,
		result.ScopeLockHold,
		result.Duration,
		time.Since(start),
	)
	if result.GenerationsPruned != 3 {
		t.Fatalf("GenerationsPruned = %d, want 3 (the whole normal batch)", result.GenerationsPruned)
	}
	if result.ChunkPredeleteBatches == 0 {
		t.Fatalf("ChunkPredeleteBatches = %d, want >0 (the normal batch must drain pre-Begin)", result.ChunkPredeleteBatches)
	}
	if result.ScopeLockHold > 5*time.Second {
		t.Fatalf("ScopeLockHold = %s, want <=5s (facts drain pre-Begin, the lock covers only generation rows and small children)", result.ScopeLockHold)
	}
	if got := result.RowsPruned["fact_records"]; got != 120_000 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 120000 (every fact exactly once)", got)
	}
	if got := result.PredrainedOverLimitGenerations; got != 0 {
		t.Fatalf("PredrainedOverLimitGenerations = %d, want 0 (no over-limit generation: this is the normal-batch leg, not the chunk leg)", got)
	}
	if got, ok := result.PhaseDurations[GenerationRetentionPhaseLockedRecount]; !ok {
		t.Fatal("PhaseDurations has no locked_recount phase (the re-lock must recount post-drain)")
	} else if got > 2*time.Second {
		t.Fatalf("locked_recount = %s, want <=2s (post-drain recount sees near-zero facts)", got)
	}
}

// TestGenerationRetentionBatchPredrainCrashResumeKeepsExactCountsLive pins
// the crash-equivalence of a budget-stopped pass: the first pass commits
// two predrain batches under a tiny row cap, excludes the partially drained
// generation, and reports its rows without an event; the second pass
// finishes the drain and prunes whole. A crash between the predrain commit
// and Begin leaves exactly this state (committed rows, no in-memory
// result), so the two passes together must count every fact exactly once.
func TestGenerationRetentionBatchPredrainCrashResumeKeepsExactCountsLive(t *testing.T) {
	bridgeBatchPredrainProofDSN(t)
	ctx, database := openBatchPredrainDatabase(t)
	seedCrashResumeFixture(t, ctx, database)

	first := batchPredrainPolicy()
	first.MinSupersededGenerations = 0
	first.BatchRowLimit = 100
	first.ChunkPredeleteRowCap = 150
	firstResult, err := NewGenerationRetentionStore(SQLDB{DB: database}).PruneSupersededGenerations(ctx, first)
	if err != nil {
		t.Fatalf("first pass PruneSupersededGenerations() error = %v", err)
	}
	if firstResult.GenerationsPruned != 0 {
		t.Fatalf("first pass pruned = %d, want 0 (the generation stops mid-drain)", firstResult.GenerationsPruned)
	}
	if got := firstResult.RowsPruned["fact_records"]; got != 200 {
		t.Fatalf("first pass fact_records = %d, want 200 (two committed batches stay deleted)", got)
	}
	if got := firstResult.Skipped[generationRetentionChunkedProgressSkippedReason]; got != 1 {
		t.Fatalf("first pass Skipped[row_limit_chunked] = %d, want 1 (the partial drain waits)", got)
	}
	if got := firstResult.ChunkPredeleteBatches; got != 2 {
		t.Fatalf("first pass ChunkPredeleteBatches = %d, want 2 (third batch never starts past the cap)", got)
	}

	second := batchPredrainPolicy()
	second.MinSupersededGenerations = 0
	second.BatchRowLimit = 100
	secondResult, err := NewGenerationRetentionStore(SQLDB{DB: database}).PruneSupersededGenerations(ctx, second)
	if err != nil {
		t.Fatalf("second pass PruneSupersededGenerations() error = %v", err)
	}
	if secondResult.GenerationsPruned != 1 {
		t.Fatalf("second pass pruned = %d, want 1 (the resumed drain finishes and prunes whole)", secondResult.GenerationsPruned)
	}
	if total := firstResult.RowsPruned["fact_records"] + secondResult.RowsPruned["fact_records"]; total != 300 {
		t.Fatalf("both passes fact_records = %d, want 300 (every fact exactly once across the resume)", total)
	}
	var remaining int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM fact_records WHERE generation_id = 'gen-resume-old'").Scan(&remaining); err != nil {
		t.Fatalf("count remaining facts: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("remaining fact_records = %d, want 0 (the resumed pass prunes the rest)", remaining)
	}
}

// TestGenerationRetentionBatchPredrainDeltaFlipExcludesLive proves a
// generation drained and then flipped to delta-protected before the prune
// transaction opens is excluded, not pruned: the prune's own selection
// drops it, its predrained rows merge with the deferral skip, and no
// retention event is written for rows a protected generation still owns.
func TestGenerationRetentionBatchPredrainDeltaFlipExcludesLive(t *testing.T) {
	bridgeBatchPredrainProofDSN(t)
	ctx, database := openBatchPredrainDatabase(t)
	seedDeltaFlipFixture(t, ctx, database)

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.beforePruneTransaction = func() {
		// Supersede the old active first: the active-scope partial unique
		// index allows only one active generation per scope.
		mustExecBatchPredrain(t, ctx, database,
			`UPDATE scope_generations SET status = 'superseded', superseded_at = now() WHERE generation_id = 'gen-flip-active'`,
			`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, is_delta, activated_at)
VALUES ('gen-flip-delta', 'scope-flip', 'snapshot', now(), now(), 'active', true, now())`,
			`UPDATE ingestion_scopes SET active_generation_id = 'gen-flip-delta' WHERE scope_id = 'scope-flip'`,
		)
	}
	policy := batchPredrainPolicy()
	policy.MinSupersededGenerations = 0
	policy.BatchRowLimit = 50
	result, err := store.PruneSupersededGenerations(ctx, policy)
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 0 {
		t.Fatalf("GenerationsPruned = %d, want 0 (the flipped generation is delta-protected)", result.GenerationsPruned)
	}
	if got := result.RowsPruned["fact_records"]; got != 60 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 60 (the predrained rows merge without an event)", got)
	}
	if got := result.Skipped["delta_base"]; got != 1 {
		t.Fatalf("Skipped[delta_base] = %d, want 1 (the selection drops the flipped generation)", got)
	}
	if got := result.Skipped[generationRetentionChunkedProgressSkippedReason]; got != 1 {
		t.Fatalf("Skipped[row_limit_chunked] = %d, want 1 (its predrained rows merge with the deferral skip)", got)
	}
}

// TestGenerationRetentionBatchPredrainConcurrentPassDefersLive proves a
// second pass racing the predrain defers instead of blocking: the first
// batch holds the generation's advisory try-lock when the hook fires, so
// the second pass's own try-lock finds it busy and the pass ends without a
// batch, without waiting on the first pass's row locks and without
// deadlock. The try-lock, SKIP LOCKED reads, and 500ms batch lock_timeout
// are what bound the overlap; both passes must return nil errors.
func TestGenerationRetentionBatchPredrainConcurrentPassDefersLive(t *testing.T) {
	bridgeBatchPredrainProofDSN(t)
	ctx, database := openBatchPredrainDatabase(t)
	seedCrashResumeFixture(t, ctx, database)

	batchStarted := make(chan struct{})
	var batchRelease chan struct{}
	var once sync.Once
	first := NewGenerationRetentionStore(SQLDB{DB: database})
	first.beforeChunkPredrainPage = func() {
		once.Do(func() {
			batchRelease = make(chan struct{})
			close(batchStarted)
		})
		<-batchRelease
	}
	policy := batchPredrainPolicy()
	policy.MinSupersededGenerations = 0
	policy.BatchRowLimit = 100

	firstDone := make(chan GenerationRetentionResult, 1)
	firstErr := make(chan error, 1)
	go func() {
		result, err := first.PruneSupersededGenerations(ctx, policy)
		if err != nil {
			firstErr <- err
			return
		}
		firstDone <- result
	}()

	<-batchStarted
	secondStart := time.Now()
	secondResult, secondErr := NewGenerationRetentionStore(SQLDB{DB: database}).PruneSupersededGenerations(ctx, policy)
	secondElapsed := time.Since(secondStart)
	close(batchRelease)

	if secondErr != nil {
		t.Fatalf("second pass PruneSupersededGenerations() error = %v (must defer, not fail)", secondErr)
	}
	if secondResult.GenerationsPruned != 0 {
		t.Fatalf("second pass pruned = %d, want 0 (advisory busy: it must defer while the first batch holds the lock)", secondResult.GenerationsPruned)
	}
	if secondResult.ChunkPredeleteBatches != 0 {
		t.Fatalf("second pass ChunkPredeleteBatches = %d, want 0 (no batch starts on a busy lock)", secondResult.ChunkPredeleteBatches)
	}
	t.Logf("No-Regression Evidence: concurrent pass deferred without waiting: second_pass_wall_time=%s", secondElapsed)

	select {
	case err := <-firstErr:
		t.Fatalf("first pass PruneSupersededGenerations() error = %v", err)
	case firstResult := <-firstDone:
		if firstResult.GenerationsPruned != 1 {
			t.Fatalf("first pass pruned = %d, want 1 (the race must not lose the prune)", firstResult.GenerationsPruned)
		}
		if got := firstResult.RowsPruned["fact_records"]; got != 300 {
			t.Fatalf("first pass fact_records = %d, want 300", got)
		}
	case <-time.After(5 * time.Minute):
		t.Fatal("first pass did not finish (deadlock with the racing pass)")
	}
}

// bridgeBatchPredrainProofDSN maps the retention proof family DSN onto the
// shared migrated-schema helper, mirroring the large-fixture bridge.
func bridgeBatchPredrainProofDSN(t *testing.T) {
	t.Helper()
	if dsn := strings.TrimSpace(os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_GENERATION_RETENTION_PROOF_DSN is set without ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
}

// openBatchPredrainDatabase opens the migrated-schema disposable database
// the batch-predrain proofs prune against.
func openBatchPredrainDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	database, ctx := openGenerationRetentionMigratedSchema(t)
	return ctx, database
}

// batchPredrainPolicy is the shared proof policy: everything eligible,
// bounded batches.
func batchPredrainPolicy() GenerationRetentionPolicy {
	return GenerationRetentionPolicy{
		MinSupersededGenerations: 1,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     100,
		BatchRowLimit:            100_000,
		PolicyScope:              "global",
		PolicyRevision:           "batch-predrain-proof",
	}
}

// mustExecBatchPredrain runs each statement and fails the test on the first
// error.
func mustExecBatchPredrain(t *testing.T, ctx context.Context, database *sql.DB, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("exec %.120q: %v", statement, err)
		}
	}
}

// seedBatchPredrainFixture writes one active generation and three superseded
// generations with 40,000 file facts each (all under the 100,000
// BatchRowLimit, so none diverts as a chunk target), plus one work item per
// superseded generation for the lock ordering the prune relies on.
func seedBatchPredrainFixture(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	steps := []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ('scope-batch', 'repository', 'github', 'acme/scope-batch', 'git', 'acme/scope-batch', now(), now(), 'active', '{}'::jsonb)`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-batch-active', 'scope-batch', 'snapshot', now(), now(), 'active')`,
		`UPDATE ingestion_scopes SET active_generation_id = 'gen-batch-active' WHERE scope_id = 'scope-batch'`,
		`INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
SELECT
    'gen-batch-old-' || n::text,
    'scope-batch',
    'snapshot',
    now() - interval '9 days' - (n * interval '1 minute'),
    now() - interval '9 days' - (n * interval '1 minute'),
    'superseded',
    now() - interval '8 days' - (n * interval '1 minute')
FROM generate_series(1, 3) AS n`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT
    'fact-batch-' || g.n::text || '-' || f.n::text,
    'scope-batch',
    'gen-batch-old-' || g.n::text,
    'file',
    'k-batch-' || g.n::text || '-' || f.n::text,
    'git',
    'k-batch-' || g.n::text || '-' || f.n::text,
    now(),
    now(),
    jsonb_build_object('repo_id', 'repo-batch', 'relative_path', 'src/f_' || f.n::text || '.go')
FROM generate_series(1, 3) AS g(n)
CROSS JOIN generate_series(1, 40000) AS f(n)`,
		`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
SELECT 'work-batch-' || n::text, 'scope-batch', 'gen-batch-old-' || ((n - 1) % 3 + 1)::text, 'reducer', 'domain', 'succeeded', now(), now()
FROM generate_series(1, 3) AS n`,
	}
	for i, step := range steps {
		if _, err := database.ExecContext(ctx, step); err != nil {
			t.Fatalf("seed step %d: %v", i, err)
		}
	}
}

// seedCrashResumeFixture writes one active generation and one superseded
// generation with 300 file facts: small enough to drain in three bounded
// pages, large enough that a tiny row cap stops it mid-drain.
func seedCrashResumeFixture(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	mustExecBatchPredrain(t, ctx, database,
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ('scope-resume', 'repository', 'github', 'acme/scope-resume', 'git', 'acme/scope-resume', now(), now(), 'active', '{}'::jsonb)`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-resume-active', 'scope-resume', 'snapshot', now(), now(), 'active')`,
		`UPDATE ingestion_scopes SET active_generation_id = 'gen-resume-active' WHERE scope_id = 'scope-resume'`,
		`INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
SELECT
    'gen-resume-old',
    'scope-resume',
    'snapshot',
    now() - interval '9 days',
    now() - interval '9 days',
    'superseded',
    now() - interval '8 days'`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT
    'fact-resume-' || f.n::text,
    'scope-resume',
    'gen-resume-old',
    'file',
    'k-resume-' || f.n::text,
    'git',
    'k-resume-' || f.n::text,
    now(),
    now(),
    jsonb_build_object('repo_id', 'repo-resume', 'relative_path', 'src/f_' || f.n::text || '.go')
FROM generate_series(1, 300) AS f(n)`,
	)
}

// seedDeltaFlipFixture writes one active generation and one superseded
// generation with 60 file facts and an activated timestamp, so a test hook
// can flip the superseded generation to delta-protected after the predrain
// (newest non-delta activated row becomes the delta base, and the flipped
// row is that base).
func seedDeltaFlipFixture(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	mustExecBatchPredrain(t, ctx, database,
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ('scope-flip', 'repository', 'github', 'acme/scope-flip', 'git', 'acme/scope-flip', now(), now(), 'active', '{}'::jsonb)`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-flip-active', 'scope-flip', 'snapshot', now(), now(), 'active')`,
		`UPDATE ingestion_scopes SET active_generation_id = 'gen-flip-active' WHERE scope_id = 'scope-flip'`,
		`INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at, activated_at)
VALUES
    ('gen-flip-old', 'scope-flip', 'snapshot', now() - interval '9 days', now() - interval '9 days',
        'superseded', now() - interval '8 days', now() - interval '8 days')`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT
    'fact-flip-' || f.n::text,
    'scope-flip',
    'gen-flip-old',
    'file',
    'k-flip-' || f.n::text,
    'git',
    'k-flip-' || f.n::text,
    now(),
    now(),
    jsonb_build_object('repo_id', 'repo-flip', 'relative_path', 'src/f_' || f.n::text || '.go')
FROM generate_series(1, 60) AS f(n)`,
	)
}
