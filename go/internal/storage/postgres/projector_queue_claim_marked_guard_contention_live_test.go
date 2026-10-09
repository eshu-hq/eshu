// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"sort"
	"sync"
	"testing"
	"time"
)

// markGuardContentionScopes is the contention benchmark's width: one ready
// generation per scope, with no marked rows, so the before and after claim
// texts behave identically and the benchmark isolates the guard predicate's
// cost under contention.
const markGuardContentionScopes = 512

// markGuardContentionWorkers is the benchmark's worker count: sixteen
// concurrent claimers across the scopes.
const markGuardContentionWorkers = 16

// markGuardContentionRounds is the benchmark's round count per variant.
const markGuardContentionRounds = 5

func markGuardSeedContention(t *testing.T, database *sql.DB) {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
		 SELECT 'bench-'||i, 'repository','git','bench-'||i,'git','bench-'||i, now(), now(), 'active' FROM generate_series(1,512) i`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
		 SELECT 'bench-'||i||'-g1','bench-'||i,'push', now()-interval '1 hour', now()-interval '1 hour','pending' FROM generate_series(1,512) i`,
		`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, visible_at, payload, created_at, updated_at)
		 SELECT 'projector_bench-'||i||'_g1','bench-'||i,'bench-'||i||'-g1','projector','source_local','pending',0, now()-interval '1 hour','{}'::jsonb, now()-interval '1 hour', now()-interval '1 hour' FROM generate_series(1,512) i`,
		`VACUUM ANALYZE fact_work_items`,
	}
	for _, stmt := range stmts {
		if _, err := database.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed contention: %v", err)
		}
	}
}

// markGuardRefillContention resets every bench work row to pending so the
// next round can claim a full window. Callers run it outside the timed
// section.
func markGuardRefillContention(t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.Exec(`
UPDATE fact_work_items
SET status = 'pending', attempt_count = 0, lease_owner = NULL, claim_until = NULL,
    visible_at = now() - interval '1 hour', next_attempt_at = NULL,
    failure_class = NULL, failure_message = NULL, failure_details = NULL
WHERE stage = 'projector' AND scope_id LIKE 'bench-%'`); err != nil {
		t.Fatalf("refill contention: %v", err)
	}
}

// markGuardClaimOnce runs one claim statement directly and reports whether it
// claimed a row. Both benchmark variants share this path, so the Go wrapper
// cannot skew the comparison.
func markGuardClaimOnce(ctx context.Context, database *sql.DB, statement string, at time.Time) (bool, error) {
	var generation string
	err := database.QueryRowContext(ctx, statement, at, "bench", at.Add(time.Minute), "").Scan(
		new(string), new(string), new(string), new(string), new(string), new(bool), new(string), new(string),
		&generation, new(int), new(time.Time), new(time.Time), new(string), new(string), new(string), new([]byte),
	)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// markGuardContentionRound runs one timed window: every worker claims an
// equal share of the refilled rows. Every claim must win exactly one row:
// with 512 unlocked rows for 16 workers, a miss means the statement skipped
// claimable work.
func markGuardContentionRound(t *testing.T, database *sql.DB, statement string) time.Duration {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	var wg sync.WaitGroup
	errs := make(chan error, markGuardContentionScopes)
	missed := make(chan struct{}, markGuardContentionScopes)
	start := make(chan struct{})
	for w := 0; w < markGuardContentionWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < markGuardContentionScopes/markGuardContentionWorkers; i++ {
				ok, err := markGuardClaimOnce(ctx, database, statement, now)
				if err != nil {
					errs <- err
					return
				}
				if !ok {
					missed <- struct{}{}
				}
			}
		}()
	}
	began := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(began)
	close(errs)
	close(missed)
	for err := range errs {
		t.Fatalf("contention claim error = %v", err)
	}
	if n := len(missed); n != 0 {
		t.Fatalf("contention round missed %d of %d claims", n, markGuardContentionScopes)
	}
	return elapsed
}

// TestProjectorClaimMarkedGuardContention is the #7469 contention benchmark:
// sixteen workers claim 512 single-generation scopes per round, five
// interleaved rounds per variant, alternating before and after. The before
// variant runs the pre-#7469 text derived from the shipped constant; both
// variants behave identically on this unmarked shape. It logs medians as
// No-Regression Evidence; it asserts only correctness (no error, no miss, no
// deadlock), never timing.
func TestProjectorClaimMarkedGuardContention(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, markGuardContentionWorkers+8)
	markGuardSeedContention(t, database)
	before, counts := markGuardClaimBeforeText(claimProjectorWorkQuery)
	for name, count := range counts {
		if count != 1 {
			t.Fatalf("before text reverses %s %d times, want 1", name, count)
		}
	}
	deadlocksBefore := serverDeadlockCount(t, database)
	samples := map[string][]time.Duration{}
	for round := 0; round < markGuardContentionRounds; round++ {
		order := []struct {
			name      string
			statement string
		}{{"before", before}, {"after", claimProjectorWorkQuery}}
		if round%2 == 1 {
			order = []struct {
				name      string
				statement string
			}{{"after", claimProjectorWorkQuery}, {"before", before}}
		}
		for _, variant := range order {
			markGuardRefillContention(t, database)
			samples[variant.name] = append(samples[variant.name],
				markGuardContentionRound(t, database, variant.statement))
		}
	}
	if delta := serverDeadlockCount(t, database) - deadlocksBefore; delta != 0 {
		t.Fatalf("server recorded %d deadlocks, want 0", delta)
	}
	median := func(v []time.Duration) time.Duration {
		s := append([]time.Duration(nil), v...)
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		return s[len(s)/2]
	}
	mean := func(v []time.Duration) time.Duration {
		var sum time.Duration
		for _, d := range v {
			sum += d
		}
		return sum / time.Duration(len(v))
	}
	b, a := samples["before"], samples["after"]
	bMed, aMed := median(b), median(a)
	bMean, aMean := mean(b), mean(a)
	perClaim := func(d time.Duration) time.Duration { return d / markGuardContentionScopes }
	t.Logf("No-Regression Evidence: contention rounds=%d workers=%d scopes=%d claims=%d "+
		"before_median=%s after_median=%s delta=%+.2f%% before_mean=%s after_mean=%s per_claim_before=%s per_claim_after=%s",
		markGuardContentionRounds, markGuardContentionWorkers, markGuardContentionScopes,
		markGuardContentionScopes*markGuardContentionRounds,
		bMed, aMed, (float64(aMed)/float64(bMed)-1)*100, bMean, aMean,
		perClaim(bMean), perClaim(aMean))
}
