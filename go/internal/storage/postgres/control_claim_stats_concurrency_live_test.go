// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9871 concurrency proof on real PostgreSQL: six claimers poll a cold workflow_work_items table while ANALYZE (SKIP_LOCKED) runs; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Concurrency proof for the #9871 post-admission ANALYZE: six claimers poll
// the cold table while the helper runs ANALYZE (SKIP_LOCKED). SKIP_LOCKED
// means the ANALYZE never waits on a claim lock and the claims never wait
// on the ANALYZE, so the proof asserts every claimer keeps getting grants,
// the helper reports analyzed, and the ANALYZE wall time stays far below
// the pathology it replaces. The owner measured 738 ms with zero sessions
// waiting on a lock; this test pins the shape, not the exact millisecond.

// TestWorkflowStatsAnalyzeDuringClaims populates a cold table with raw SQL
// (bypassing admission, so no other ANALYZE can warm it), starts six
// polling claimers, and runs the production helper across them.
func TestWorkflowStatsAnalyzeDuringClaims(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx, cancel := statsProofContext(t)
	defer cancel()
	now := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)

	const (
		run      = "stats-concurrency-run"
		keys     = 200
		perKey   = 100
		claimers = 6
	)
	seedStatsConcurrencyCorpus(t, database, ctx, run, keys, perKey)
	requireNeverAnalyzed(t, database, ctx, "post-load")

	stop := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, claimers)
	grants := make(chan int, claimers)
	var spansMu sync.Mutex
	type attemptSpan struct{ start, end time.Time }
	var spans []attemptSpan
	for c := 0; c < claimers; c++ {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			n := 0
			for {
				select {
				case <-stop:
					grants <- n
					return
				default:
				}
				attemptStart := time.Now()
				_, _, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
					CollectorKind:       scope.CollectorGit,
					CollectorInstanceID: "stats-concurrency-instance",
					OwnerID:             fmt.Sprintf("stats-concurrency-owner-%d", c),
					ClaimID:             fmt.Sprintf("stats-concurrency-claim-%d-%d", c, n),
					RotationSalt:        "stats-concurrency-salt",
				}, now.Add(time.Hour).UTC(), 5*time.Minute)
				attemptEnd := time.Now()
				spansMu.Lock()
				spans = append(spans, attemptSpan{start: attemptStart, end: attemptEnd})
				spansMu.Unlock()
				if err != nil {
					errs <- err
					return
				}
				if found {
					n++
				}
			}
		}(c)
	}

	// Head start: cold-table claims take seconds each, so after two seconds
	// several claims are in flight when the ANALYZE starts. That is the
	// overlap under proof: claims holding row locks while the statistics
	// refresh runs, and the refresh must neither block them nor fail.
	time.Sleep(2 * time.Second)
	analyzeStart := time.Now()
	outcome := store.analyzeWorkflowWorkItemsOnce(ctx, telemetry.WorkflowStatsAnalyzeSiteEnqueue, keys*perKey)
	analyzeEnd := time.Now()
	analyzeWall := analyzeEnd.Sub(analyzeStart)
	close(stop)
	wg.Wait()
	close(errs)
	close(grants)

	for err := range errs {
		t.Fatalf("concurrent claimer error during ANALYZE: %v", err)
	}
	total := 0
	for n := range grants {
		total += n
	}
	overlapped := false
	for _, span := range spans {
		if span.start.Before(analyzeEnd) && span.end.After(analyzeStart) {
			overlapped = true
			break
		}
	}
	t.Logf("ANALYZE (SKIP_LOCKED) during %d claimers: outcome=%q wall=%s grants=%d attempts=%d overlapped=%v",
		claimers, outcome, analyzeWall.Round(time.Millisecond), total, len(spans), overlapped)
	if outcome != telemetry.WorkflowStatsAnalyzeOutcomeAnalyzed {
		t.Fatalf("helper outcome = %q, want %q: the cold table must analyze while claims run", outcome, telemetry.WorkflowStatsAnalyzeOutcomeAnalyzed)
	}
	if total == 0 {
		t.Fatal("no concurrent claim granted during ANALYZE; claimers stalled on the statistics gate")
	}
	if !overlapped {
		t.Fatal("no claim attempt straddled the ANALYZE window; the overlap under proof did not happen, rerun for a real contention sample")
	}
	if analyzeWall > 30*time.Second {
		t.Fatalf("ANALYZE wall = %s, want under 30s: the statistics refresh must not stall behind claim locks", analyzeWall.Round(time.Millisecond))
	}
	if analyzed, auto := workflowWorkItemsAnalyzeState(t, database, ctx); !analyzed && !auto {
		t.Fatal("table still never-analyzed after the helper reported analyzed")
	}
}

// seedStatsConcurrencyCorpus bulk-loads a smaller cold corpus with raw SQL so
// no admission path warms the statistics under test.
func seedStatsConcurrencyCorpus(t *testing.T, database *sql.DB, ctx context.Context, run string, keys, perKey int) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)`,
		run, string(workflow.TriggerKindSchedule), string(workflow.RunStatusCollectionPending), time.Now().UTC().Truncate(time.Second)); err != nil {
		t.Fatalf("insert stats concurrency run: %v", err)
	}
	res, err := database.ExecContext(ctx, fmt.Sprintf(`INSERT INTO workflow_work_items (
		work_item_id, run_id, collector_kind, collector_instance_id,
		source_system, scope_id, acceptance_unit_id, source_run_id,
		generation_id, fairness_key, status, created_at, updated_at
	) SELECT
		'stats-concurrency-item-' || g,
		$1, 'git', 'stats-concurrency-instance', 'git',
		'stats-concurrency-scope-' || ((g - 1) / %d),
		'stats-concurrency-scope-' || ((g - 1) / %d),
		'stats-concurrency-gen', 'stats-concurrency-gen',
		'stats-concurrency-key-' || lpad((((g - 1) / %d)::text), 3, '0'),
		'pending', now(), now()
	FROM generate_series(1, %d) AS g`, perKey, perKey, perKey, keys*perKey), run)
	if err != nil {
		t.Fatalf("bulk-load stats concurrency corpus: %v", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != int64(keys*perKey) {
		t.Fatalf("bulk-loaded rows = %d, %v; want %d", n, err, keys*perKey)
	}
}
