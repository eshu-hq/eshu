// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:env ESHU_PROJECTOR_ADMISSION_PROOF_DSN
//eshu:readiness:reason #10882 disposable PostgreSQL proof that a source-paced Fail retries without ever dead-lettering, increments only source_paced_waits (attempt_count stays a monotonic fence, admission_wait_attempts untouched), leaves the full projection and admission budgets afterwards, and that migration 333 applies cleanly with a NOT NULL DEFAULT 0 counter; isolated schema per test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
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
