// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// retentionProbeGenerations and retentionProbeFactsPerGeneration size one
// retention batch: 10 superseded generations of 5,000 content_entity facts.
const (
	retentionProbeGenerations        = 10
	retentionProbeFactsPerGeneration = 5000
)

// advisoryLocksHeldSQL counts granted advisory locks across all sessions.
const advisoryLocksHeldSQL = `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND granted`

// seedRetentionProbeRepo gives repo one active generation and
// retentionProbeGenerations superseded ones, each owning distinct infra
// entities that also exist in content_entities and in the read model.
// superseded_at is a month old so only this probe's generations fall outside
// the retention window; the other live tests leave superseded_at NULL.
func seedRetentionProbeRepo(t *testing.T, ctx context.Context, sqlDB *sql.DB, repo string) {
	t.Helper()
	scope := "scope-" + repo
	steps := []struct {
		sql  string
		args []any
	}{
		{`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, now(), now(), 'active', '{}'::jsonb)`, []any{scope}},
		{`
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1 || '-active', $1, 'snapshot', now(), now(), 'active')`, []any{scope}},
		{`UPDATE ingestion_scopes SET active_generation_id = $1 || '-active' WHERE scope_id = $1`, []any{scope}},
		{`
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, superseded_at)
SELECT $1 || '-old-' || g, $1, 'snapshot', now() - interval '31 days', now() - interval '31 days',
       'superseded', now() - interval '30 days' + g * interval '1 minute'
FROM generate_series(1, $2::int) g`, []any{scope, retentionProbeGenerations}},
		{
			`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT $1 || '/f' || g || '/' || e, $1, $1 || '-old-' || g, 'content_entity', $1 || '/f' || g || '/' || e,
       'git', $1 || '/f' || g || '/' || e, now(), now(),
       jsonb_build_object('repo_id', $4::text, 'entity_id', $4::text || '/e' || g || '-' || e)
FROM generate_series(1, $2::int) g, generate_series(1, $3::int) e`,
			[]any{scope, retentionProbeGenerations, retentionProbeFactsPerGeneration, repo},
		},
		{
			`
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, metadata, indexed_at)
SELECT $1::text || '/e' || g || '-' || e, $1::text, 'infra/f' || (e % 500) || '.tf', 'TerraformResource',
       'r' || g || '-' || e, 1, 20, repeat('x', 400), jsonb_build_object('provider', 'aws'), now()
FROM generate_series(1, $2::int) g, generate_series(1, $3::int) e`,
			[]any{repo, retentionProbeGenerations, retentionProbeFactsPerGeneration},
		},
	}
	for i, step := range steps {
		if _, err := sqlDB.ExecContext(ctx, step.sql, step.args...); err != nil {
			t.Fatalf("seed probe step %d: %v", i, err)
		}
	}
	if _, err := inventory.MirrorRepo(ctx, postgres.SQLDB{DB: sqlDB}, repo); err != nil {
		t.Fatalf("MirrorRepo(%s) error = %v", repo, err)
	}
	// The retention counts query is tuned for planned shapes (its arms were
	// landed with EXPLAIN ANALYZE evidence), and production always has
	// autovacuum stats. A freshly seeded probe database has none, so plan
	// the counts on analyzed tables instead of default estimates.
	if _, err := sqlDB.ExecContext(ctx, `ANALYZE ingestion_scopes, scope_generations, fact_records, content_entities, content_files, infra_resource_entities`); err != nil {
		t.Fatalf("analyze probe tables: %v", err)
	}
}

// watchAdvisoryLockHold polls pg_locks on its own connection until stop closes
// and returns the span during which a granted advisory lock was visible.
// Retention runs alone while it watches, so that span is retention's hold.
func watchAdvisoryLockHold(ctx context.Context, sqlDB *sql.DB, stop <-chan struct{}) (<-chan time.Duration, error) {
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	out := make(chan time.Duration, 1)
	go func() {
		var first, last time.Time
		defer func() {
			_ = conn.Close()
			out <- last.Sub(first)
		}()
		for {
			var held int
			if err := conn.QueryRowContext(ctx, advisoryLocksHeldSQL).Scan(&held); err == nil && held > 0 {
				now := time.Now()
				if first.IsZero() {
					first = now
				}
				last = now
			}
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Millisecond):
			}
		}
	}()
	return out, nil
}

func probeRetentionPolicy() postgres.GenerationRetentionPolicy {
	return postgres.GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         24 * time.Hour,
		BatchGenerationLimit:     retentionProbeGenerations,
		BatchRowLimit:            1_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "infra-inventory-lock-probe",
	}
}

// newRetentionProbeRoundContext gives each complete 50k-row round its own
// deadline; no round inherits the previous round's elapsed budget. The
// budget is deliberately generous for a correctness-only probe: measured
// -race rounds take ~30s on a fast host, but a slow scheduled runner needed
// more than 90s for one round (its counts query missed the deadline at
// exactly the 90s mark while sibling rounds took ~27s), failing
// TestRetentionLiveLockHoldAndDeriveWaitCost. 3m is ~6x the measured slow
// round yet stays well inside the shared go test -timeout=7m on the
// scheduled lane, so one wedged round fails with its RETENTION_PHASE
// traces instead of consuming the whole package timeout.
func newRetentionProbeRoundContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 3*time.Minute)
}

// retentionPhaseTrace records test-side phase boundaries against the original
// round context. Its logger only observes time and context state.
type retentionPhaseTrace struct {
	ctx          context.Context
	round        int
	roundStarted time.Time
	logf         func(string, ...any)
}

func (trace retentionPhaseTrace) mark(phase, boundary string, phaseStarted time.Time, outcome string) {
	trace.markAt(phase, boundary, phaseStarted, outcome, time.Now())
}

func (trace retentionPhaseTrace) markAt(phase, boundary string, phaseStarted time.Time, outcome string, now time.Time) {
	deadline, _ := trace.ctx.Deadline()
	trace.logf("RETENTION_PHASE round=%d phase=%s boundary=%s outcome=%s utc=%s round_elapsed=%s phase_elapsed=%s original_ctx_deadline_utc=%s original_ctx_remaining=%s ctx_err=%v",
		trace.round, phase, boundary, outcome, now.UTC().Format(time.RFC3339Nano),
		now.Sub(trace.roundStarted), now.Sub(phaseStarted),
		deadline.UTC().Format(time.RFC3339Nano), deadline.Sub(now), trace.ctx.Err())
}

func (trace retentionPhaseTrace) finish(activePhase string, failed bool) {
	outcome := "ok"
	if failed {
		outcome = "failed_in_" + activePhase
	}
	trace.mark("round", "end", trace.roundStarted, outcome)
}

// TestRetentionLiveLockHoldAndDeriveWaitCost measures what the #6793 retention
// locks cost the content writer. Each round prunes one
// batch of 10 superseded generations (50,000 content_entity facts, all infra
// rows of one repository) through the real PruneSupersededGenerations and logs:
//
//   - lock_hold: how long retention held the repository's derive lock,
//     sampled from pg_locks while retention runs alone;
//   - derive_wait: the wall time of a one-path derive of the same repository
//     that starts as soon as the lock is visible, against its idle baseline;
//   - other_repo: a one-path derive of an unrelated repository at the same
//     moment, which must not wait.
//
// It asserts correctness only (every pruned row leaves the read model), not a
// latency threshold. Run it alone against a database with no other
// advisory-lock traffic:
//
//	ESHU_POSTGRES_DSN=... go test ./internal/storage/postgres/infra/inventory \
//	  -run TestRetentionLiveLockHoldAndDeriveWaitCost -count=1 -v
func TestRetentionLiveLockHoldAndDeriveWaitCost(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	other := uniqueRepo(t)
	putContent(t, ctx, sqlDB, other, contentRow{"o1", "o.tf", "TerraformResource", "r.o1", `{}`})
	wantPruned := int64(retentionProbeGenerations * retentionProbeFactsPerGeneration)

	for round := 1; round <= 3; round++ {
		runRetentionProbeRound(t, sqlDB, other, round, wantPruned)
	}
}

// runRetentionProbeRound keeps all seeding, pruning, observation, and derive
// checks inside one independent bounded round and joins its worker on failure.
func runRetentionProbeRound(t *testing.T, sqlDB *sql.DB, other string, round int, wantPruned int64) {
	t.Helper()
	ctx, cancel := newRetentionProbeRoundContext()
	defer cancel()
	trace := retentionPhaseTrace{ctx: ctx, round: round, roundStarted: time.Now(), logf: t.Logf}
	activePhase := "round"
	trace.mark("round", "start", trace.roundStarted, "running")
	defer func() { trace.finish(activePhase, t.Failed() || ctx.Err() != nil) }()
	database := postgres.SQLDB{DB: sqlDB}
	store := postgres.NewGenerationRetentionStore(database)
	activePhase = "drain"
	phaseStarted := time.Now()
	trace.mark(activePhase, "start", phaseStarted, "running")
	drainRetention(t, ctx, store)
	trace.mark(activePhase, "end", phaseStarted, "ok")
	activePhase = "round"
	// Retention alone, to sample the lock hold.
	repo := uniqueRepo(t)
	activePhase = "seed_first"
	phaseStarted = time.Now()
	trace.mark(activePhase, "start", phaseStarted, "running")
	seedRetentionProbeRepo(t, ctx, sqlDB, repo)
	trace.mark(activePhase, "end", phaseStarted, "ok")
	activePhase = "round"
	stop := make(chan struct{})
	hold, err := watchAdvisoryLockHold(ctx, sqlDB, stop)
	if err != nil {
		t.Fatalf("watch locks: %v", err)
	}
	activePhase = "prune_first"
	phaseStarted = time.Now()
	trace.mark(activePhase, "start", phaseStarted, "running")
	result, err := store.PruneSupersededGenerations(ctx, probeRetentionPolicy())
	pruneEnded := time.Now()
	close(stop)
	lockHold := <-hold
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	trace.markAt(activePhase, "end", phaseStarted, outcome, pruneEnded)
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	activePhase = "check_first"
	phaseStarted = time.Now()
	trace.mark(activePhase, "start", phaseStarted, "running")
	if got := result.RowsPruned["infra_resource_entities"]; got != wantPruned {
		t.Fatalf("round %d infra_resource_entities pruned = %d, want %d", round, got, wantPruned)
	}
	trace.mark(activePhase, "end", phaseStarted, "ok")
	activePhase = "round"

	// A derive of the same repository racing retention.
	repo = uniqueRepo(t)
	activePhase = "seed_second"
	phaseStarted = time.Now()
	trace.mark(activePhase, "start", phaseStarted, "running")
	seedRetentionProbeRepo(t, ctx, sqlDB, repo)
	trace.mark(activePhase, "end", phaseStarted, "ok")
	activePhase = "baseline_derive"
	phaseStarted = time.Now()
	trace.mark(activePhase, "start", phaseStarted, "running")
	baseline := timedDerive(t, ctx, database, repo)
	trace.mark(activePhase, "end", phaseStarted, "ok")
	activePhase = "round"
	retentionDone := make(chan error, 1)
	go func() {
		started := time.Now()
		trace.mark("prune_second", "start", started, "running")
		_, err := store.PruneSupersededGenerations(ctx, probeRetentionPolicy())
		outcome := "ok"
		if err != nil {
			outcome = "error"
		}
		trace.mark("prune_second", "end", started, outcome)
		retentionDone <- err
	}()
	joined := false
	defer func() {
		if !joined {
			cancel()
			<-retentionDone
		}
	}()
	activePhase = "wait_lock"
	phaseStarted = time.Now()
	trace.mark(activePhase, "start", phaseStarted, "running")
	waitForAdvisoryLock(t, ctx, sqlDB)
	trace.mark(activePhase, "end", phaseStarted, "ok")
	activePhase = "other_derive"
	phaseStarted = time.Now()
	trace.mark(activePhase, "start", phaseStarted, "running")
	otherWait := timedDerive(t, ctx, database, other)
	trace.mark(activePhase, "end", phaseStarted, "ok")
	activePhase = "same_derive"
	phaseStarted = time.Now()
	trace.mark(activePhase, "start", phaseStarted, "running")
	deriveWait := timedDerive(t, ctx, database, repo)
	trace.mark(activePhase, "end", phaseStarted, "ok")
	activePhase = "join_prune_second"
	phaseStarted = time.Now()
	trace.mark(activePhase, "start", phaseStarted, "running")
	retentionErr := <-retentionDone
	trace.mark(activePhase, "end", phaseStarted, "ok")
	joined = true
	activePhase = "check_final"
	phaseStarted = time.Now()
	trace.mark(activePhase, "start", phaseStarted, "running")
	if retentionErr != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", retentionErr)
	}
	if rows := tableRows(t, ctx, sqlDB, repo); len(rows) != 0 {
		t.Fatalf("round %d: %d read-model rows survived retention, want 0", round, len(rows))
	}
	trace.mark(activePhase, "end", phaseStarted, "ok")
	activePhase = "round"
	t.Logf("No-Regression Evidence: round=%d retention_duration=%s lock_hold=%s derive_baseline=%s derive_wait=%s other_repo=%s",
		round, result.Duration, lockHold, baseline, deriveWait, otherWait)
}

// drainRetention prunes every eligible generation left by an earlier aborted
// probe run, so each measured batch is exactly this round's repository. Only
// this probe seeds superseded_at, so nothing else is eligible.
func drainRetention(t *testing.T, ctx context.Context, store postgres.GenerationRetentionStore) {
	t.Helper()
	for {
		result, err := store.PruneSupersededGenerations(ctx, probeRetentionPolicy())
		if err != nil {
			t.Fatalf("drain retention: %v", err)
		}
		if result.GenerationsPruned == 0 {
			return
		}
	}
}

// timedDerive runs a one-path derive of repo and returns its wall time.
func timedDerive(t *testing.T, ctx context.Context, database postgres.SQLDB, repo string) time.Duration {
	t.Helper()
	start := time.Now()
	if _, err := inventory.MirrorPaths(ctx, database, inventory.Target{RepoID: repo}, []string{"infra/f1.tf"}); err != nil {
		t.Fatalf("MirrorPaths(%s) error = %v", repo, err)
	}
	return time.Since(start)
}

// waitForAdvisoryLock blocks until some session holds a granted advisory lock.
func waitForAdvisoryLock(t *testing.T, ctx context.Context, sqlDB *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var held int
		if err := sqlDB.QueryRowContext(ctx, advisoryLocksHeldSQL).Scan(&held); err != nil {
			t.Fatalf("poll pg_locks: %v", err)
		}
		if held > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("retention never took its advisory lock")
}
