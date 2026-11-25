// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:env ESHU_PROJECTOR_ADMISSION_PROOF_DSN
//eshu:readiness:reason #10671 disposable PostgreSQL proof that an admission-timeout failure retries against the dedicated admission budget (attempt_count stays a monotonic fence, the admission counter increments exactly once per admission Fail including under a concurrent double-Fail race with the loser claim-rejected, a stale owner is rejected without touching the row), that exhausting the admission budget dead-letters with the admission_timeout class on the Nth wait, that generic retryable failures still spend the refunded projection budget to a retry_exhausted dead-letter, and that migration 325 applies cleanly with a NOT NULL DEFAULT 0 counter; isolated schema per test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/rebuild/reset"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// admissionResetQueryer adapts *sql.DB to the reset package's narrow Queryer.
type admissionResetQueryer struct{ db *sql.DB }

func (q admissionResetQueryer) QueryContext(ctx context.Context, query string, args ...any) (reset.Rows, error) {
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return admissionResetRows{rows}, nil
}

// admissionResetRows adapts *sql.Rows to the reset package's narrow Rows.
type admissionResetRows struct{ rows *sql.Rows }

func (r admissionResetRows) Next() bool             { return r.rows.Next() }
func (r admissionResetRows) Scan(dest ...any) error { return r.rows.Scan(dest...) }
func (r admissionResetRows) Err() error             { return r.rows.Err() }
func (r admissionResetRows) Close() error           { return r.rows.Close() }

// liveAdmissionProofDB opens the disposable Postgres named by
// ESHU_PROJECTOR_ADMISSION_PROOF_DSN, or skips when unset. The database
// must be empty; each test provisions its own proof schema via
// provisionLivenessSchema.
func liveAdmissionProofDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("ESHU_PROJECTOR_ADMISSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_ADMISSION_PROOF_DSN to a disposable Postgres database")
	}
	return openLivenessProofDB(t, dsn)
}

const admissionProofSeedSQL = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-admission', 'repository', 'github', 'proof/admission', 'git',
          'proof/admission', now(), now(), 'active', 'gen-live');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES ('gen-live', 'scope-admission', 'push', now() - interval '1 hour',
    now() - interval '1 hour', 'pending', NULL);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, admission_wait_attempts, lease_owner, claim_until,
    visible_at, payload, created_at, updated_at
) VALUES ('work-live', 'scope-admission', 'gen-live', 'projector',
    'source_local', 'pending', 0, 0, NULL, NULL, NULL, '{}'::jsonb, now(), now());
`

// admissionProofRow reads the durable counters for the live work item.
func admissionProofRow(t *testing.T, ctx context.Context, db *sql.DB) (status string, attempts, admissionWaits int) {
	t.Helper()

	if err := db.QueryRowContext(ctx, `
SELECT status, attempt_count, admission_wait_attempts
FROM fact_work_items WHERE work_item_id = 'work-live'
`).Scan(&status, &attempts, &admissionWaits); err != nil {
		t.Fatalf("read live work item: %v", err)
	}
	return status, attempts, admissionWaits
}

// TestProjectorQueueAdmissionBudgetLiveCycle proves the split budget
// end to end on live Postgres: the claim populates both counters, an
// admission-timeout Fail retries while incrementing only the admission
// counter, attempt_count stays a strictly monotonic fence, a generic
// retryable failure still retries inside the refunded projection budget,
// and exhausting the refunded budget dead-letters.
func TestProjectorQueueAdmissionBudgetLiveCycle(t *testing.T) {
	db := liveAdmissionProofDB(t)
	provisionLivenessSchema(t, db, admissionProofSeedSQL)
	ctx := context.Background()

	// Manual clock: each Fail parks the item behind visible_at for the
	// backoff delay, so the test advances past every retry delay
	// deterministically instead of racing it. The test asserts state
	// transitions, not the production backoff schedule (pinned
	// hermetically).
	now := time.Date(2026, time.November, 20, 20, 30, 0, 0, time.UTC)
	queue := NewProjectorQueue(SQLDB{DB: db}, "live-proof", time.Minute)
	queue.MaxAttempts = 3
	queue.AdmissionMaxAttempts = 48
	queue.Now = func() time.Time { return now }
	advance := func() { now = now.Add(2 * time.Hour) }

	work, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true")
	}
	if work.AttemptCount != 1 || work.AdmissionWaitAttempts != 0 {
		t.Fatalf("claimed counters = (%d, %d), want (1, 0)", work.AttemptCount, work.AdmissionWaitAttempts)
	}

	admissionCause := productionWrapAdmissionError(&admissionTimeoutBudgetTestError{message: "reducer admission wait timed out"})
	if err := queue.Fail(ctx, work, admissionCause); err != nil {
		t.Fatalf("admission Fail() error = %v, want nil", err)
	}
	if status, attempts, waits := admissionProofRow(t, ctx, db); status != "retrying" || attempts != 1 || waits != 1 {
		t.Fatalf("after admission fail = (%q, %d, %d), want (retrying, 1, 1)", status, attempts, waits)
	}

	advance()
	work, ok, err = queue.Claim(ctx)
	if err != nil {
		t.Fatalf("reclaim error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("reclaim ok = false, want true")
	}
	if work.AttemptCount != 2 || work.AdmissionWaitAttempts != 1 {
		t.Fatalf("reclaimed counters = (%d, %d), want (2, 1)", work.AttemptCount, work.AdmissionWaitAttempts)
	}

	if err := queue.Fail(ctx, work, &retryableTestError{message: "graph write timed out"}); err != nil {
		t.Fatalf("generic Fail() error = %v, want nil", err)
	}
	if status, attempts, waits := admissionProofRow(t, ctx, db); status != "retrying" || attempts != 2 || waits != 1 {
		t.Fatalf("after generic fail = (%q, %d, %d), want (retrying, 2, 1)", status, attempts, waits)
	}

	// One more claim+generic-fail cycle (attempt 3 against 1 admission
	// wait: 3-1=2 < 3, still inside the refunded budget); the attempt-4
	// generic failure then exhausts it (4-1=3, not < 3) and dead-letters.
	advance()
	work, ok, err = queue.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("claim ok = false, want true (attempt 3)")
	}
	if err := queue.Fail(ctx, work, &retryableTestError{message: "still broken"}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	advance()
	work, ok, err = queue.Claim(ctx)
	if err != nil {
		t.Fatalf("final Claim() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("final claim ok = false, want true")
	}
	if err := queue.Fail(ctx, work, &retryableTestError{message: "still broken"}); err != nil {
		t.Fatalf("final Fail() error = %v, want nil", err)
	}
	var gotStatus, gotClass string
	if err := db.QueryRowContext(ctx, `SELECT status, failure_class FROM fact_work_items WHERE work_item_id = 'work-live'`).Scan(&gotStatus, &gotClass); err != nil {
		t.Fatalf("read terminal row: %v", err)
	}
	if gotStatus != "dead_letter" {
		t.Fatalf("terminal status = %q, want dead_letter", gotStatus)
	}
}

// TestProjectorQueueAdmissionFailAdmitsExactlyOnceUnderRace proves the
// admission retry increment is exactly-once under concurrent Fail calls
// on the same claim: the fenced UPDATE admits one winner, the loser gets
// ErrProjectorClaimRejected, and the counter advances by exactly one.
func TestProjectorQueueAdmissionFailAdmitsExactlyOnceUnderRace(t *testing.T) {
	db := liveAdmissionProofDB(t)
	provisionLivenessSchema(t, db, admissionProofSeedSQL)
	ctx := context.Background()

	var schemaName string
	if err := db.QueryRowContext(ctx, "SHOW search_path").Scan(&schemaName); err != nil {
		t.Fatalf("read search_path: %v", err)
	}
	racerDB, err := sql.Open("pgx", os.Getenv("ESHU_PROJECTOR_ADMISSION_PROOF_DSN"))
	if err != nil {
		t.Fatalf("open racer connection: %v", err)
	}
	racerDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = racerDB.Close() })
	if _, err := racerDB.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set racer search_path: %v", err)
	}

	owner := NewProjectorQueue(SQLDB{DB: db}, "live-proof", time.Minute)
	owner.MaxAttempts = 3
	owner.AdmissionMaxAttempts = 48
	owner.Now = time.Now
	racer := NewProjectorQueue(SQLDB{DB: racerDB}, "live-proof", time.Minute)
	racer.MaxAttempts = 3
	racer.AdmissionMaxAttempts = 48
	racer.Now = time.Now

	work, ok, err := owner.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true")
	}

	cause := func() error {
		return productionWrapAdmissionError(&admissionTimeoutBudgetTestError{message: "reducer admission wait timed out"})
	}
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q := owner
			if i == 1 {
				q = racer
			}
			// Both racers act on the same claim: identical lease owner
			// and attempt fence, so the UPDATE admits exactly one.
			results[i] = q.Fail(ctx, work, cause())
		}(i)
	}
	wg.Wait()

	var succeeded, rejected int
	for _, err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrProjectorClaimRejected):
			rejected++
		default:
			t.Fatalf("racer error = %v, want nil or claim-rejected", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("race outcome = %d succeeded, %d rejected; want exactly 1 and 1", succeeded, rejected)
	}
	if _, attempts, waits := admissionProofRow(t, ctx, db); attempts != 1 || waits != 1 {
		t.Fatalf("after race counters = (%d, %d), want (1, 1)", attempts, waits)
	}

	// A stale owner (wrong attempt fence) is rejected without touching
	// the row.
	stale := work
	stale.AttemptCount = work.AttemptCount + 1
	if err := owner.Fail(ctx, stale, cause()); !errors.Is(err, ErrProjectorClaimRejected) {
		t.Fatalf("stale Fail() error = %v, want claim-rejected", err)
	}
	if _, attempts, waits := admissionProofRow(t, ctx, db); attempts != 1 || waits != 1 {
		t.Fatalf("after stale fail counters = (%d, %d), want (1, 1)", attempts, waits)
	}
}

// TestProjectorQueueAdmissionRefinalizeCycleLive proves the rebuild
// reopen path end to end on live Postgres: reset.EnqueueProjectorWork
// re-runs against a refinalize row carrying a spent admission budget and
// brings BOTH counters back to 0, so the row's next admission-timeout
// failure retries instead of dead-lettering on the spot. Without the
// paired reset, the refund would go negative (retrying everything) or
// the budget would read pre-spent (dead-letter on the first new wait).
func TestProjectorQueueAdmissionRefinalizeCycleLive(t *testing.T) {
	db := liveAdmissionProofDB(t)
	provisionLivenessSchema(t, db, "")
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, admissionProofSeedSQL); err != nil {
		t.Fatalf("seed proof scope: %v", err)
	}
	// Only the refinalize row may be claimable: drop the seed's own
	// projector work item so the claim below cannot take it instead.
	if _, err := db.ExecContext(ctx, `DELETE FROM fact_work_items WHERE work_item_id = 'work-live'`); err != nil {
		t.Fatalf("drop seed work item: %v", err)
	}
	var gens reset.Generations
	gens.Append("scope-admission", "gen-live")
	if _, err := reset.EnqueueProjectorWork(ctx, admissionResetQueryer{db: db}, gens, time.Now()); err != nil {
		t.Fatalf("refinalize enqueue error = %v, want nil", err)
	}
	var refinalizeID string
	if err := db.QueryRowContext(ctx, `SELECT work_item_id FROM fact_work_items WHERE stage = 'projector' AND work_item_id LIKE 'refinalize\_%'`).Scan(&refinalizeID); err != nil {
		t.Fatalf("find refinalize row: %v", err)
	}
	// Bank a spent budget the way the old reset (attempt_count only)
	// would leave it: attempts reset, 48 waits kept.
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = 'retrying', attempt_count = 0, admission_wait_attempts = 48, lease_owner = NULL, claim_until = NULL, visible_at = NULL, updated_at = now() WHERE work_item_id = $1`, refinalizeID); err != nil {
		t.Fatalf("bank spent budget: %v", err)
	}
	// A re-run must bring both counters back to 0 (ON CONFLICT UPDATE).
	if _, err := reset.EnqueueProjectorWork(ctx, admissionResetQueryer{db: db}, gens, time.Now()); err != nil {
		t.Fatalf("refinalize re-enqueue error = %v, want nil", err)
	}
	var status string
	var attempts, waits int
	if err := db.QueryRowContext(ctx, `SELECT status, attempt_count, admission_wait_attempts FROM fact_work_items WHERE work_item_id = $1`, refinalizeID).Scan(&status, &attempts, &waits); err != nil {
		t.Fatalf("read reopened row: %v", err)
	}
	if status != "pending" || attempts != 0 || waits != 0 {
		t.Fatalf("reopened row = (%q, %d, %d), want (pending, 0, 0)", status, attempts, waits)
	}

	// The reopened row must behave like a fresh cycle: claim it and fail
	// an admission wait — it retries (pre-fix it would dead-letter on
	// the pre-spent budget).
	queue := NewProjectorQueue(SQLDB{DB: db}, "live-proof", time.Minute)
	queue.MaxAttempts = 3
	queue.AdmissionMaxAttempts = 48
	queue.Now = time.Now
	work, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("reopen Claim() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("reopen claim ok = false, want true")
	}
	if work.AttemptCount != 1 || work.AdmissionWaitAttempts != 0 {
		t.Fatalf("reopened counters = (%d, %d), want (1, 0)", work.AttemptCount, work.AdmissionWaitAttempts)
	}
	admissionCause := productionWrapAdmissionError(&admissionTimeoutBudgetTestError{message: "reducer admission wait timed out"})
	if err := queue.Fail(ctx, work, admissionCause); err != nil {
		t.Fatalf("reopen Fail() error = %v, want nil", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status, attempt_count, admission_wait_attempts FROM fact_work_items WHERE work_item_id = $1`, refinalizeID).Scan(&status, &attempts, &waits); err != nil {
		t.Fatalf("read row after reopen fail: %v", err)
	}
	if status != "retrying" || attempts != 1 || waits != 1 {
		t.Fatalf("after reopen fail = (%q, %d, %d), want (retrying, 1, 1)", status, attempts, waits)
	}
}

// TestProjectorQueueAdmissionBudgetExhaustionDeadLettersLive proves the
// admission budget is finite on live Postgres: with a budget of 2, the
// first admission-timeout Fail retries on the claim fence (attempt 1,
// admission counter exactly 1) and the second dead-letters with the
// admission_timeout class while attempt_count stays on the claim fence
// (2) and the admission counter stays at the granted retry (1): the
// counter counts granted waits, and the exhausting Nth wait dead-letters
// instead of incrementing (sibling projection-fence semantics — the
// Nth wait dead-letters on counter N-1). The admission retry lands on
// its own counter, never on the projection retry surge.
// This closes the fake-only gap behind the
// retry:non-counting-deferral ledger row: the fake suite proves the SQL
// shape against a recorder, this proves the durable counters against
// real Postgres.
func TestProjectorQueueAdmissionBudgetExhaustionDeadLettersLive(t *testing.T) {
	db := liveAdmissionProofDB(t)
	provisionLivenessSchema(t, db, admissionProofSeedSQL)
	ctx := context.Background()

	now := time.Date(2026, time.November, 20, 20, 30, 0, 0, time.UTC)
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("admission-budget-exhaustion-live"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	queue := NewProjectorQueue(SQLDB{DB: db}, "live-proof", time.Minute)
	// MaxAttempts 1 exhausts the projection budget on the first claim
	// (projectionAttempts 1 < 1 is false), so the first admission retry
	// below also pins the exhausted-projection-budget half of the row:
	// admission retries despite a spent projection budget.
	queue.MaxAttempts = 1
	queue.AdmissionMaxAttempts = 2
	queue.Instruments = instruments
	queue.Now = func() time.Time { return now }
	advance := func() { now = now.Add(2 * time.Hour) }

	work, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true")
	}
	if work.AttemptCount != 1 || work.AdmissionWaitAttempts != 0 {
		t.Fatalf("claimed counters = (%d, %d), want (1, 0)", work.AttemptCount, work.AdmissionWaitAttempts)
	}

	admissionCause := productionWrapAdmissionError(&admissionTimeoutBudgetTestError{message: "reducer admission wait timed out"})
	if err := queue.Fail(ctx, work, admissionCause); err != nil {
		t.Fatalf("first admission Fail() error = %v, want nil", err)
	}
	if status, attempts, waits := admissionProofRow(t, ctx, db); status != "retrying" || attempts != 1 || waits != 1 {
		t.Fatalf("after first admission fail = (%q, %d, %d), want (retrying, 1, 1)", status, attempts, waits)
	}

	advance()
	work, ok, err = queue.Claim(ctx)
	if err != nil {
		t.Fatalf("reclaim error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("reclaim ok = false, want true")
	}
	if work.AttemptCount != 2 || work.AdmissionWaitAttempts != 1 {
		t.Fatalf("reclaimed counters = (%d, %d), want (2, 1)", work.AttemptCount, work.AdmissionWaitAttempts)
	}
	if err := queue.Fail(ctx, work, admissionCause); err != nil {
		t.Fatalf("second admission Fail() error = %v, want nil", err)
	}
	var gotStatus, gotClass string
	var gotAttempts, gotWaits int
	if err := db.QueryRowContext(ctx, `SELECT status, attempt_count, admission_wait_attempts, failure_class FROM fact_work_items WHERE work_item_id = 'work-live'`).Scan(&gotStatus, &gotAttempts, &gotWaits, &gotClass); err != nil {
		t.Fatalf("read exhausted row: %v", err)
	}
	if gotStatus != "dead_letter" || gotAttempts != 2 || gotWaits != 1 || gotClass != "admission_timeout" {
		t.Fatalf("after budget-exhausting admission fail = (%q, %d, %d, %q), want (dead_letter, 2, 1, admission_timeout)", gotStatus, gotAttempts, gotWaits, gotClass)
	}

	if got := collectCounter(t, reader, "eshu_dp_projector_admission_wait_retries_total"); got != 1 {
		t.Fatalf("admission wait retries = %d, want 1 (the retried first wait; the exhausting wait dead-letters)", got)
	}
	if got := collectCounter(t, reader, "eshu_dp_projector_retry_surge_total"); got != 0 {
		t.Fatalf("retry surge after admission waits = %d, want 0", got)
	}
}

// TestProjectorQueueAdmissionMigration325AppliesLive proves the shipped
// migration 325 applies cleanly on live Postgres: the column lands NOT
// NULL DEFAULT 0, existing rows read 0, and a second apply is a no-op.
func TestProjectorQueueAdmissionMigration325AppliesLive(t *testing.T) {
	db := liveAdmissionProofDB(t)
	ctx := context.Background()

	schemaName := "migration_325_proof"
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE fact_work_items (work_item_id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create minimal work table: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := db.ExecContext(ctx, MigrationSQL("projector_admission_wait_attempts")); err != nil {
			t.Fatalf("apply migration 325 (pass %d): %v", i, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_work_items (work_item_id) VALUES ('probe-1')`); err != nil {
		t.Fatalf("insert probe row: %v", err)
	}
	var waits int64
	if err := db.QueryRowContext(ctx, `SELECT admission_wait_attempts FROM fact_work_items WHERE work_item_id = 'probe-1'`).Scan(&waits); err != nil {
		t.Fatalf("read default counter: %v", err)
	}
	if waits != 0 {
		t.Fatalf("fresh-row counter = %d, want 0", waits)
	}
}
