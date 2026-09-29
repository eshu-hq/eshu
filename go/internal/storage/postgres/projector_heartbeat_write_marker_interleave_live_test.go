// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
)

// writeMarkerInterleaveSeed is the #7389 interleave fixture on top of
// heartbeatProofDB's scope-hb and its active gen-pub: gen-il is the running
// generation under test and gen-il-newer is the newer pending generation that
// makes the heartbeat want to supersede it.
const writeMarkerInterleaveSeed = `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ('gen-il', 'scope-hb', 'push', now() - interval '30 minutes',
          now() - interval '30 minutes', 'pending'),
         ('gen-il-newer', 'scope-hb', 'push', now() - interval '20 minutes',
          now() - interval '20 minutes', 'pending');
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('projector_scope-hb_gen-il', 'scope-hb', 'gen-il', 'projector',
          'source_local', 'running', 1, 'proof-worker',
          now() + interval '1 hour', now(), '{}'::jsonb, now(), now());
`

// resetWriteMarkerInterleave puts gen-il and its work row back to "running,
// no marker, not superseded" before each iteration.
const resetWriteMarkerInterleave = `
UPDATE scope_generations
SET status = 'pending', projection_write_started_at = NULL, superseded_at = NULL
WHERE generation_id = 'gen-il';
UPDATE fact_work_items
SET status = 'running', lease_owner = 'proof-worker', attempt_count = 1,
    claim_until = now() + interval '1 hour', visible_at = NULL,
    next_attempt_at = NULL, failure_class = NULL, failure_message = NULL,
    failure_details = NULL
WHERE work_item_id = 'projector_scope-hb_gen-il';
`

// readWriteMarkerInterleaveState is "generation status,marker set,work status".
const readWriteMarkerInterleaveState = `
SELECT generation.status || ',' || (generation.projection_write_started_at IS NOT NULL)::text
       || ',' || work.status
FROM scope_generations AS generation
JOIN fact_work_items AS work ON work.generation_id = generation.generation_id
WHERE generation.generation_id = 'gen-il'
`

// writeMarkerInterleaveIterations is the ruling's N.
const writeMarkerInterleaveIterations = 1000

// TestProjectorHeartbeatWriteMarkerInterleave is the #7389 exclusion proof: the
// write-start marker and the heartbeat supersede race on one generation from
// two sessions, 1,000 times, simultaneously or with either side delayed by a
// seeded random 0-400us. Exactly one may win each race. A marker that commits
// must leave the generation pending with its marker set and its work running;
// a supersede that wins must leave it superseded with no marker. Both winning
// ("class both") is the double win that lets a superseded generation keep
// writing: the one-predicate gate on the unlocked joined generation row lost
// 341 of 1,000 races, because EvalPlanQual rechecks only the locked rows.
func TestProjectorHeartbeatWriteMarkerInterleave(t *testing.T) {
	dsn := supersessionProofDSN(t)
	control := heartbeatProofDB(t, writeMarkerInterleaveSeed)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	markerDB := openSessionOnProofSchema(ctx, t, control, dsn)
	supersedeDB := openSessionOnProofSchema(ctx, t, control, dsn)
	markerQueue := NewProjectorQueue(SQLDB{DB: markerDB}, "proof-worker", time.Minute)
	supersedeQueue := NewProjectorQueue(SQLDB{DB: supersedeDB}, "proof-worker", time.Minute)
	work := heartbeatProofWork("gen-il")

	deadlocksBefore := proofDatabaseDeadlocks(ctx, t, control)
	classes := map[string]int{}
	finals := map[string]int{}
	errs := map[string]int{}
	rng := rand.New(rand.NewSource(7389))
	for i := 0; i < writeMarkerInterleaveIterations; i++ {
		if _, err := control.ExecContext(ctx, resetWriteMarkerInterleave); err != nil {
			t.Fatalf("iteration %d: reset: %v", i, err)
		}
		mode := i % 3 // 0 simultaneous, 1 marker delayed, 2 supersede delayed
		delay := time.Duration(rng.Intn(400)) * time.Microsecond
		start := make(chan struct{})
		var wg sync.WaitGroup
		var markerErr, supersedeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if mode == 1 {
				time.Sleep(delay)
			}
			markerErr = markerQueue.MarkProjectionWriteStarted(ctx, work)
		}()
		go func() {
			defer wg.Done()
			<-start
			if mode == 2 {
				time.Sleep(delay)
			}
			supersedeErr = supersedeQueue.supersedeRunningWork(ctx, work, time.Now().UTC())
		}()
		close(start)
		wg.Wait()

		markerWon, markerProblem := classifyInterleaveMarker(markerErr)
		supersedeWon, supersedeProblem := classifyInterleaveSupersede(supersedeErr)
		if markerProblem != "" {
			errs["marker "+markerProblem]++
		}
		if supersedeProblem != "" {
			errs["supersede "+supersedeProblem]++
		}
		class := fmt.Sprintf("marker=%d supersede=%d", boolInt(markerWon), boolInt(supersedeWon))
		classes[class]++
		var state string
		if err := control.QueryRowContext(ctx, readWriteMarkerInterleaveState).Scan(&state); err != nil {
			t.Fatalf("iteration %d: read state: %v", i, err)
		}
		finals[class+" -> "+state]++
	}
	flushProofSessionStats(ctx, t, markerDB, supersedeDB)
	deadlocksAfter := proofDatabaseDeadlocks(ctx, t, control)

	t.Logf("interleave iterations=%d deadlocks before=%d after=%d delta=%d",
		writeMarkerInterleaveIterations, deadlocksBefore, deadlocksAfter, deadlocksAfter-deadlocksBefore)
	for _, line := range sortedCounts("class", classes) {
		t.Log(line)
	}
	for _, line := range sortedCounts("final", finals) {
		t.Log(line)
	}
	for _, line := range sortedCounts("error", errs) {
		t.Log(line)
	}

	if both := classes["marker=1 supersede=1"]; both != 0 {
		t.Errorf("class both = %d of %d, want 0: the marker and the heartbeat supersede both won a race",
			both, writeMarkerInterleaveIterations)
	}
	if delta := deadlocksAfter - deadlocksBefore; delta != 0 {
		t.Errorf("pg_stat_database.deadlocks delta = %d, want 0", delta)
	}
	if len(errs) != 0 {
		t.Errorf("unexpected statement errors: %v", errs)
	}
	allowed := map[string]bool{
		"marker=1 supersede=0 -> pending,true,running":        true,
		"marker=0 supersede=1 -> superseded,false,superseded": true,
	}
	for final, count := range finals {
		if !allowed[final] {
			t.Errorf("final state %q seen %d times, want only (pending, marker set, running) or (superseded, no marker, superseded)", final, count)
		}
	}
}

// classifyInterleaveMarker maps a marker result to "won" and an error label.
// ErrWorkSuperseded is the expected loss; anything else is a problem.
func classifyInterleaveMarker(err error) (bool, string) {
	switch {
	case err == nil:
		return true, ""
	case errors.Is(err, failure.ErrWorkSuperseded):
		return false, ""
	default:
		return false, sqlStateLabel(err)
	}
}

// classifyInterleaveSupersede maps a heartbeat supersede result to "won" and
// an error label. nil means the supersede changed nothing.
func classifyInterleaveSupersede(err error) (bool, string) {
	switch {
	case err == nil:
		return false, ""
	case errors.Is(err, failure.ErrWorkSuperseded):
		return true, ""
	default:
		return false, sqlStateLabel(err)
	}
}

func sqlStateLabel(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return "ERR:" + err.Error()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func sortedCounts(prefix string, counts map[string]int) []string {
	lines := make([]string, 0, len(counts))
	for key, count := range counts {
		lines = append(lines, fmt.Sprintf("%s %s : %d", prefix, key, count))
	}
	sort.Strings(lines)
	return lines
}

// openSessionOnProofSchema opens a one-connection pool whose search_path is
// control's proof schema, so a second session sees the same fixture.
func openSessionOnProofSchema(ctx context.Context, t *testing.T, control *sql.DB, dsn string) *sql.DB {
	t.Helper()
	var searchPath string
	if err := control.QueryRowContext(ctx, "SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatalf("read proof search_path: %v", err)
	}
	session := openLivenessProofDB(t, dsn)
	if _, err := session.ExecContext(ctx, "SET search_path TO "+searchPath); err != nil {
		t.Fatalf("set session search_path: %v", err)
	}
	return session
}

// proofDatabaseDeadlocks reads the database's deadlock counter on a fresh
// statistics snapshot.
func proofDatabaseDeadlocks(ctx context.Context, t *testing.T, database *sql.DB) int64 {
	t.Helper()
	if _, err := database.ExecContext(ctx, "SELECT pg_stat_clear_snapshot()"); err != nil {
		t.Fatalf("clear stats snapshot: %v", err)
	}
	var deadlocks int64
	if err := database.QueryRowContext(ctx,
		"SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()").Scan(&deadlocks); err != nil {
		t.Fatalf("read pg_stat_database.deadlocks: %v", err)
	}
	return deadlocks
}

// flushProofSessionStats makes each session publish its pending statistics,
// including a deadlock it detected, before the counter is read.
func flushProofSessionStats(ctx context.Context, t *testing.T, sessions ...*sql.DB) {
	t.Helper()
	for _, session := range sessions {
		if _, err := session.ExecContext(ctx, "SELECT pg_stat_force_next_flush()"); err != nil {
			t.Fatalf("force stats flush: %v", err)
		}
		if _, err := session.ExecContext(ctx, "SELECT 1"); err != nil {
			t.Fatalf("flush stats: %v", err)
		}
	}
}

// supersessionProofDSN returns the disposable proof database DSN the projector
// supersession live tests share, or skips the test when it is unset.
func supersessionProofDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}
	return dsn
}
