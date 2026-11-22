// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:env ESHU_PROJECTOR_ADMISSION_PROOF_DSN
//eshu:readiness:reason #10882 disposable PostgreSQL proof that a source-paced Fail retries without ever dead-lettering, increments only source_paced_waits (attempt_count stays a monotonic fence, admission_wait_attempts untouched), leaves the full projection and admission budgets afterwards, that an operator replay of a row with source_paced_waits > 0 gives exactly MaxAttempts projection claims (#11182 interaction), and that migration 333 applies cleanly with a NOT NULL DEFAULT 0 counter; isolated schema per test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
)

// sourcePacedProofRow reads the durable counters for the live work item.
func sourcePacedProofRow(t *testing.T, ctx context.Context, db *sql.DB) (status string, attempts, admissionWaits, pacedWaits int) {
	t.Helper()

	if err := db.QueryRowContext(ctx, `
SELECT status, attempt_count, admission_wait_attempts, source_paced_waits
FROM fact_work_items WHERE work_item_id = 'work-live'
`).Scan(&status, &attempts, &admissionWaits, &pacedWaits); err != nil {
		t.Fatalf("read live work item: %v", err)
	}
	return status, attempts, admissionWaits, pacedWaits
}

// TestProjectorQueueSourcePacedLiveCycle proves the #10882 fork-1 contract
// on live Postgres. A source paced for many more cycles than both budgets
// combined never dead-letters, each paced Fail increments only
// source_paced_waits, and after release the item still owns its full
// projection budget (a generic failure retries, MaxAttempts-1 times) and
// its full admission budget (an admission timeout retries).
func TestProjectorQueueSourcePacedLiveCycle(t *testing.T) {
	db := liveAdmissionProofDB(t)
	provisionLivenessSchema(t, db, admissionProofSeedSQL)
	ctx := context.Background()

	now := time.Date(2026, time.November, 22, 1, 0, 0, 0, time.UTC)
	queue := NewProjectorQueue(SQLDB{DB: db}, "live-proof", time.Minute)
	queue.MaxAttempts = 3
	queue.AdmissionMaxAttempts = 2
	queue.Now = func() time.Time { return now }
	advance := func() { now = now.Add(2 * time.Hour) }

	pacedCause := productionWrapAdmissionError(sourcePacedTestError{})
	const pacedCycles = 25 // far past MaxAttempts (3) + AdmissionMaxAttempts (2)
	for cycle := 1; cycle <= pacedCycles; cycle++ {
		if cycle > 1 {
			advance()
		}
		work, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("cycle %d: Claim() = (%v, %v), want a claim", cycle, ok, err)
		}
		if work.AttemptCount != cycle || work.SourcePacedWaits != cycle-1 {
			t.Fatalf("cycle %d: claimed counters (attempt %d, paced %d), want (%d, %d)",
				cycle, work.AttemptCount, work.SourcePacedWaits, cycle, cycle-1)
		}
		if err := queue.Fail(ctx, work, pacedCause); err != nil {
			t.Fatalf("cycle %d: paced Fail() error = %v, want nil", cycle, err)
		}
		status, attempts, admissionWaits, pacedWaits := sourcePacedProofRow(t, ctx, db)
		if status != "retrying" || attempts != cycle || admissionWaits != 0 || pacedWaits != cycle {
			t.Fatalf("cycle %d: row = (%q, attempt %d, admission %d, paced %d), want (retrying, %d, 0, %d)",
				cycle, status, attempts, admissionWaits, pacedWaits, cycle, cycle)
		}
	}

	// Released. Genuine failures keep the whole projection budget:
	// MaxAttempts-1 generic failures retry, the next dead-letters.
	for genuine := 1; genuine <= queue.MaxAttempts; genuine++ {
		advance()
		work, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("genuine %d: Claim() = (%v, %v), want a claim", genuine, ok, err)
		}
		if err := queue.Fail(ctx, work, &retryableTestError{message: "graph write timed out"}); err != nil {
			t.Fatalf("genuine %d: Fail() error = %v, want nil", genuine, err)
		}
		status, _, _, _ := sourcePacedProofRow(t, ctx, db)
		want := "retrying"
		if genuine == queue.MaxAttempts {
			want = "dead_letter"
		}
		if status != want {
			t.Fatalf("genuine failure %d: status = %q, want %q (full projection budget intact after pacing)", genuine, status, want)
		}
	}
}

// TestProjectorQueueSourcePacedKeepsAdmissionBudgetLive proves pacing leaves
// the admission budget to genuine admission timeouts: after many paced
// cycles the first admission timeout still retries (budget 2: wait 1 of 2
// retries, wait 2 dead-letters) and only it increments
// admission_wait_attempts.
func TestProjectorQueueSourcePacedKeepsAdmissionBudgetLive(t *testing.T) {
	db := liveAdmissionProofDB(t)
	provisionLivenessSchema(t, db, admissionProofSeedSQL)
	ctx := context.Background()

	now := time.Date(2026, time.November, 22, 1, 0, 0, 0, time.UTC)
	queue := NewProjectorQueue(SQLDB{DB: db}, "live-proof", time.Minute)
	queue.MaxAttempts = 3
	queue.AdmissionMaxAttempts = 2
	queue.Now = func() time.Time { return now }
	advance := func() { now = now.Add(2 * time.Hour) }

	failWith := func(cause error) {
		t.Helper()
		advance()
		work, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("Claim() = (%v, %v), want a claim", ok, err)
		}
		if err := queue.Fail(ctx, work, cause); err != nil {
			t.Fatalf("Fail() error = %v, want nil", err)
		}
	}
	for i := 0; i < 10; i++ {
		failWith(productionWrapAdmissionError(sourcePacedTestError{}))
	}
	admission := productionWrapAdmissionError(&admissionTimeoutBudgetTestError{message: "reducer admission wait timed out"})
	failWith(admission)
	if status, _, waits, paced := sourcePacedProofRow(t, ctx, db); status != "retrying" || waits != 1 || paced != 10 {
		t.Fatalf("after first admission timeout = (%q, admission %d, paced %d), want (retrying, 1, 10)", status, waits, paced)
	}
	failWith(admission)
	if status, _, _, _ := sourcePacedProofRow(t, ctx, db); status != "dead_letter" {
		t.Fatalf("second admission timeout (budget 2) status = %q, want dead_letter", status)
	}
}

// sourcePacedColumnShape reads the type, nullability, and default of
// fact_work_items.source_paced_waits in schemaName.
func sourcePacedColumnShape(t *testing.T, ctx context.Context, db *sql.DB, schemaName string) (dataType, nullable, columnDefault string) {
	t.Helper()
	if err := db.QueryRowContext(ctx, `
SELECT data_type, is_nullable, COALESCE(column_default, '')
FROM information_schema.columns
WHERE table_schema = $1 AND table_name = 'fact_work_items' AND column_name = 'source_paced_waits'`,
		schemaName).Scan(&dataType, &nullable, &columnDefault); err != nil {
		t.Fatalf("read source_paced_waits column shape in %s: %v", schemaName, err)
	}
	return dataType, nullable, columnDefault
}

// TestProjectorQueueSourcePacedMigration333AppliesLive proves the shipped
// migration applies cleanly on live Postgres: the column lands bigint NOT
// NULL DEFAULT 0, existing rows read 0, a second apply is a no-op, and the
// shape matches the hand-written proof-schema DDL the cycle tests build on
// (so those tests cannot pass against a column production does not have).
// The schemas carry a UnixNano suffix, like provisionLivenessSchema, so a run
// killed before cleanup cannot leave a fixed-name schema behind for the next.
func TestProjectorQueueSourcePacedMigration333AppliesLive(t *testing.T) {
	db := liveAdmissionProofDB(t)
	ctx := context.Background()

	suffix := time.Now().UnixNano()
	migrationSchema := fmt.Sprintf("migration_333_proof_%d", suffix)
	helperSchema := fmt.Sprintf("migration_333_helper_%d", suffix)
	for _, name := range []string{migrationSchema, helperSchema} {
		if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+name); err != nil {
			t.Fatalf("create proof schema %s: %v", name, err)
		}
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+name+" CASCADE")
		})
	}

	if _, err := db.ExecContext(ctx, "SET search_path TO "+migrationSchema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE fact_work_items (work_item_id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create minimal work table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_work_items (work_item_id) VALUES ('pre-existing')`); err != nil {
		t.Fatalf("insert pre-existing row: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := db.ExecContext(ctx, MigrationSQL("projector_source_paced_waits")); err != nil {
			t.Fatalf("apply migration 333 (pass %d): %v", i, err)
		}
	}
	var waits int64
	if err := db.QueryRowContext(ctx, `SELECT source_paced_waits FROM fact_work_items WHERE work_item_id = 'pre-existing'`).Scan(&waits); err != nil {
		t.Fatalf("read default counter: %v", err)
	}
	if waits != 0 {
		t.Fatalf("pre-existing row counter = %d, want 0", waits)
	}
	dataType, nullable, columnDefault := sourcePacedColumnShape(t, ctx, db, migrationSchema)
	if dataType != "bigint" || nullable != "NO" || columnDefault != "0" {
		t.Fatalf("migration 333 column = (%s, nullable %s, default %q), want (bigint, NO, \"0\")", dataType, nullable, columnDefault)
	}

	// Helper-vs-333: the hand-written proof schema must declare the same shape.
	if _, err := db.ExecContext(ctx, "SET search_path TO "+helperSchema); err != nil {
		t.Fatalf("set helper search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, generationLivenessProofSchemaSQL); err != nil {
		t.Fatalf("create helper proof tables: %v", err)
	}
	hType, hNullable, hDefault := sourcePacedColumnShape(t, ctx, db, helperSchema)
	if hType != dataType || hNullable != nullable || hDefault != columnDefault {
		t.Fatalf("proof-schema helper column = (%s, nullable %s, default %q), migration 333 = (%s, nullable %s, default %q): the cycle tests would prove a different column",
			hType, hNullable, hDefault, dataType, nullable, columnDefault)
	}
}

// TestProjectorQueueReplayAfterSourcePacedWaitsGivesThreeClaimsLive is the
// RB-1 proof for the #10882 and #11182 interaction on live Postgres. A row
// paced for several cycles, then dead-lettered by genuine failures, carries
// source_paced_waits > 0. An operator replay (the shipped
// queuestore.ReplayRetryAllowanceAssignments, applied here as the carriers
// apply it) must give it the retries of a new row: exactly MaxAttempts
// projection claims. With source_paced_waits left set the projection count
// attempt_count - admission_wait_attempts - source_paced_waits -
// replay_attempt_baseline goes negative and the row would get extra retries.
func TestProjectorQueueReplayAfterSourcePacedWaitsGivesThreeClaimsLive(t *testing.T) {
	db := liveAdmissionProofDB(t)
	provisionLivenessSchema(t, db, admissionProofSeedSQL)
	ctx := context.Background()

	now := time.Date(2026, time.November, 22, 1, 0, 0, 0, time.UTC)
	queue := NewProjectorQueue(SQLDB{DB: db}, "live-proof", time.Minute)
	queue.MaxAttempts = 3
	queue.Now = func() time.Time { return now }
	advance := func() { now = now.Add(2 * time.Hour) }

	pacedCause := productionWrapAdmissionError(sourcePacedTestError{})
	for cycle := 1; cycle <= 7; cycle++ {
		advance()
		work, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("paced cycle %d: Claim() = (%v, %v), want a claim", cycle, ok, err)
		}
		if err := queue.Fail(ctx, work, pacedCause); err != nil {
			t.Fatalf("paced cycle %d: Fail() error = %v", cycle, err)
		}
	}
	for genuine := 1; genuine <= queue.MaxAttempts; genuine++ {
		advance()
		work, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("genuine %d: Claim() = (%v, %v), want a claim", genuine, ok, err)
		}
		if err := queue.Fail(ctx, work, &retryableTestError{message: "graph write timed out"}); err != nil {
			t.Fatalf("genuine %d: Fail() error = %v", genuine, err)
		}
	}
	if status, _, _, paced := sourcePacedProofRow(t, ctx, db); status != "dead_letter" || paced != 7 {
		t.Fatalf("before replay: status %q, source_paced_waits %d, want dead_letter with 7", status, paced)
	}

	// The replay carriers' effect on the counters: attempt_count is kept (the
	// claim fence), the shipped fragment resets the rest.
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items
SET status = 'pending', attempt_count = GREATEST(attempt_count, 1), lease_owner = NULL, claim_until = NULL,
    visible_at = $1, next_attempt_at = NULL, failure_class = NULL, reopened_at = $1, `+queuestore.ReplayRetryAllowanceAssignments+`
WHERE work_item_id = 'work-live'`, now); err != nil {
		t.Fatalf("replay the dead letter: %v", err)
	}
	if _, _, _, paced := sourcePacedProofRow(t, ctx, db); paced != 0 {
		t.Fatalf("source_paced_waits after replay = %d, want 0 (the replay gives the new-row allowance)", paced)
	}

	claims := 0
	for ; claims < 10; claims++ {
		advance()
		work, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("after replay, claim %d: Claim() = (%v, %v), want a claim", claims+1, ok, err)
		}
		if err := queue.Fail(ctx, work, &retryableTestError{message: "graph write timed out"}); err != nil {
			t.Fatalf("after replay, claim %d: Fail() error = %v", claims+1, err)
		}
		if status, _, _, _ := sourcePacedProofRow(t, ctx, db); status == "dead_letter" {
			claims++
			break
		}
	}
	if claims != queue.MaxAttempts {
		t.Fatalf("replayed row got %d projection claims before dead-lettering, want exactly MaxAttempts (%d)", claims, queue.MaxAttempts)
	}
}

// releaseSeedSQL seeds two sources' projector work: two older items for
// source-a (the paced source) and one newer for source-b, so the claim pool
// hands out source-a first.
const releaseSeedSQL = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES
  ('scope-a1', 'repository', 'source-a', 'proof/a1', 'git', 'proof/a1', now(), now(), 'active', 'gen-a1'),
  ('scope-a2', 'repository', 'source-a', 'proof/a2', 'git', 'proof/a2', now(), now(), 'active', 'gen-a2'),
  ('scope-b1', 'repository', 'source-b', 'proof/b1', 'git', 'proof/b1', now(), now(), 'active', 'gen-b1');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES
  ('gen-a1', 'scope-a1', 'push', now() - interval '1 hour', now() - interval '1 hour', 'pending', NULL),
  ('gen-a2', 'scope-a2', 'push', now() - interval '1 hour', now() - interval '1 hour', 'pending', NULL),
  ('gen-b1', 'scope-b1', 'push', now() - interval '1 hour', now() - interval '1 hour', 'pending', NULL);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, admission_wait_attempts, source_paced_waits, lease_owner, claim_until,
    visible_at, payload, created_at, updated_at
) VALUES
  ('work-a1', 'scope-a1', 'gen-a1', 'projector', 'source_local', 'pending', 0, 0, 0, NULL, NULL, NULL, '{}'::jsonb, now() - interval '3 hour', now() - interval '3 hour'),
  ('work-a2', 'scope-a2', 'gen-a2', 'projector', 'source_local', 'pending', 0, 0, 0, NULL, NULL, NULL, '{}'::jsonb, now() - interval '2 hour', now() - interval '2 hour'),
  ('work-b1', 'scope-b1', 'gen-b1', 'projector', 'source_local', 'pending', 0, 0, 0, NULL, NULL, NULL, '{}'::jsonb, now() - interval '1 hour', now() - interval '1 hour');
`

// releaseSourcePacer reports only source-a slowed, with a 45s grant delay.
type releaseSourcePacer struct{}

func (releaseSourcePacer) SourcePaced(_ context.Context, source string) (bool, time.Duration) {
	return source == "source-a", 45 * time.Second
}

// releaseResumeBound is the release age the pacer reports as the resume bound.
const releaseResumeBound = 15 * time.Minute

// ResumeBound lets the service stamp the resume bound on the release cause.
func (releaseSourcePacer) ResumeBound() time.Duration { return releaseResumeBound }

// releaseWriteCounters records every projection write path a worker reaches.
type releaseWriteCounters struct {
	mu       sync.Mutex
	loads    map[string]int
	projects map[string]int
	marks    map[string]int
	acks     map[string]int
}

func (c *releaseWriteCounters) bump(m map[string]int, key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m[key]++
}

func newReleaseWriteCounters() *releaseWriteCounters {
	return &releaseWriteCounters{
		loads: map[string]int{}, projects: map[string]int{}, marks: map[string]int{}, acks: map[string]int{},
	}
}

func (c *releaseWriteCounters) LoadProjectionFacts(_ context.Context, w projector.ScopeGenerationWork) (projector.ProjectionFacts, error) {
	c.bump(c.loads, w.Scope.SourceSystem)
	return projector.ProjectionFacts{}, nil
}

func (c *releaseWriteCounters) Project(_ context.Context, sc scope.IngestionScope, _ scope.ScopeGeneration, _ []facts.Envelope) (runtime.Result, error) {
	c.bump(c.projects, sc.SourceSystem)
	return runtime.Result{}, nil
}

func (c *releaseWriteCounters) MarkProjectionWriteStarted(_ context.Context, w projector.ScopeGenerationWork) error {
	c.bump(c.marks, w.Scope.SourceSystem)
	return nil
}

func (c *releaseWriteCounters) ReadDeltaBaseline(context.Context, projector.ScopeGenerationWork) (projector.DeltaBaselineState, error) {
	// A found, non-delta generation: the preflight, which runs before the
	// pacing gate, lets it through.
	return projector.DeltaBaselineState{TargetFound: true}, nil
}

func (c *releaseWriteCounters) RefuseDeltaBaseline(context.Context, projector.ScopeGenerationWork, projector.DeltaBaselineRefusal) error {
	return nil
}

// releaseSink is the real queue for Claim and Fail; Ack is recorded only, so
// the proof needs no full acknowledgement schema.
type releaseSink struct {
	ProjectorQueue
	counters *releaseWriteCounters
}

func (s releaseSink) Ack(_ context.Context, w projector.ScopeGenerationWork, _ runtime.Result) error {
	s.counters.bump(s.counters.acks, w.Scope.SourceSystem)
	return nil
}

// TestProjectorQueueSourcePacedReleaseHoldsNoWorkerLive is the #10882 F8
// live proof. With source-a slowed and source-b clear, one real projector
// service pass over the real queue: every source-a claim is released through
// the source-paced Fail before any fact load, write marker, project, or ack
// (zero writer calls for source-a); the released rows hold no lease, sit at
// least the grant delay out, consume no attempt budget and no dead letter;
// and source-b's item is claimed and projected in the same pass, so a paced
// source cannot hold the workers. A second pass after the delay re-releases
// the same items with the counters advanced, never dead-lettered.
func TestProjectorQueueSourcePacedReleaseHoldsNoWorkerLive(t *testing.T) {
	db := liveAdmissionProofDB(t)
	provisionLivenessSchema(t, db, releaseSeedSQL)
	ctx := context.Background()

	now := time.Date(2026, time.November, 22, 1, 0, 0, 0, time.UTC)
	queue := NewProjectorQueue(SQLDB{DB: db}, "live-proof", 30*time.Minute)
	queue.MaxAttempts = 3
	queue.AdmissionMaxAttempts = 2
	queue.RetryDelay = 30 * time.Second
	queue.MaxRetryDelay = time.Hour
	queue.JitterFraction = 0.1
	queue.Now = func() time.Time { return now }

	counters := newReleaseWriteCounters()
	service := projector.Service{
		WorkSource:         queue,
		FactStore:          counters,
		Runner:             counters,
		WorkSink:           releaseSink{ProjectorQueue: queue, counters: counters},
		DeltaBaselineFence: counters,
		WriteMarker:        counters,
		SourcePacer:        releaseSourcePacer{},
		Wait:               func(context.Context, time.Duration) error { return context.Canceled },
	}
	rowOf := func(id string) (status string, attempts, admission, paced int, leased bool, visibleAt time.Time) {
		var lease sql.NullString
		var visible sql.NullTime
		if err := db.QueryRowContext(ctx, `
SELECT status, attempt_count, admission_wait_attempts, source_paced_waits, lease_owner, visible_at
FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&status, &attempts, &admission, &paced, &lease, &visible); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return status, attempts, admission, paced, lease.Valid, visible.Time
	}

	// Nine passes: past the cycle count where the unbounded exponential would
	// reach the 1 h retry cap (30s * 2^7 = 64 min), so the resume bound is what
	// the visible_at assertions below prove (#10882 R4-4).
	for pass := 1; pass <= 9; pass++ {
		if err := service.Run(ctx); err != nil {
			t.Fatalf("pass %d: Run() error = %v, want nil", pass, err)
		}
		for _, id := range []string{"work-a1", "work-a2"} {
			status, attempts, admission, paced, leased, visibleAt := rowOf(id)
			if status != "retrying" || leased || attempts != pass || admission != 0 || paced != pass {
				var failureMessage sql.NullString
				_ = db.QueryRowContext(ctx, `SELECT failure_message FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&failureMessage)
				t.Fatalf("pass %d %s = (%s, leased %v, attempt %d, admission %d, paced %d), want (retrying, no lease, %d, 0, %d); failure_message=%q",
					pass, id, status, leased, attempts, admission, paced, pass, pass, failureMessage.String)
			}
			if visibleAt.Sub(now) < 45*time.Second {
				t.Fatalf("pass %d %s visible_at is %s out, want at least the 45s grant delay", pass, id, visibleAt.Sub(now))
			}
			if out := visibleAt.Sub(now); out > releaseResumeBound+3*time.Second {
				t.Fatalf("pass %d %s visible_at is %s out, want at most the %s resume bound plus jitter", pass, id, out, releaseResumeBound)
			}
			if out := visibleAt.Sub(now); pass >= 8 && out < releaseResumeBound {
				t.Fatalf("pass %d %s visible_at is %s out, want the exponential to have reached the %s bound", pass, id, out, releaseResumeBound)
			}
		}
		if counters.loads["source-a"] != 0 || counters.projects["source-a"] != 0 ||
			counters.marks["source-a"] != 0 || counters.acks["source-a"] != 0 {
			t.Fatalf("pass %d: source-a reached writers: load=%d project=%d mark=%d ack=%d, want 0",
				pass, counters.loads["source-a"], counters.projects["source-a"], counters.marks["source-a"], counters.acks["source-a"])
		}
		if counters.projects["source-b"] < 1 {
			t.Fatalf("pass %d: source-b projected %d times, want it claimed and projected while source-a is paced", pass, counters.projects["source-b"])
		}
		now = now.Add(2 * time.Hour)
	}
	var deadLetters int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fact_work_items WHERE status = 'dead_letter'`).Scan(&deadLetters); err != nil {
		t.Fatalf("count dead letters: %v", err)
	}
	if deadLetters != 0 {
		t.Fatalf("dead letters = %d, want 0 after repeated paced releases", deadLetters)
	}
}
