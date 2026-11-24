// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// FindingWriteStopTrigger is the fact_records trigger that refuses every INSERT or UPDATE
// of a reducer_supply_chain_impact_finding row (#10880 PR-C2 k, migration 357). Proof
// databases carry it ARMED, as production does: a forgotten disarm fails loudly with
// SQLSTATE 55000, a forgotten arm would pass silently, so the default is the loud one.
//
// Frozen finding facts are history: the backfill, drain, verification and key fill read
// them, and the pre-C2 read paths read them until PR-C2 m removes those paths. A proof
// that must plant one disables the trigger around the plant with SeedFindingFacts (the
// code under test then runs armed), or, for a test that models a schema from before C2,
// for the whole test with DisarmFindingWriteStopForTest. Every caller of either form is
// named, with its reason, in the shrink-only allowlist of finding_write_stop_allowlist_test.go.
const FindingWriteStopTrigger = "fact_records_reject_supply_chain_impact_finding"

// FindingWriteStopArmed reports whether the trigger exists on db's fact_records and is
// enabled for ordinary sessions (tgenabled 'O', or 'A' for always). A schema that
// predates the trigger reports false.
func FindingWriteStopArmed(ctx context.Context, db *sql.DB) (bool, error) {
	var armed bool
	err := db.QueryRowContext(ctx, `
SELECT COALESCE((SELECT tgenabled IN ('O', 'A') FROM pg_trigger
    WHERE tgrelid = to_regclass('fact_records') AND tgname = $1 AND NOT tgisinternal), FALSE)`,
		FindingWriteStopTrigger).Scan(&armed)
	if err != nil {
		return false, fmt.Errorf("read finding write stop state: %w", err)
	}
	return armed, nil
}

// DisarmFindingWriteStop disables the finding write stop on db's fact_records. It is a
// no-op on a database whose schema predates the trigger. Prefer SeedFindingFacts or
// DisarmFindingWriteStopForTest, which re-arm and check; this primitive does neither.
func DisarmFindingWriteStop(ctx context.Context, db *sql.DB) error {
	return setFindingWriteStop(ctx, db, "DISABLE")
}

// ArmFindingWriteStop enables the finding write stop on db's fact_records, as production
// has it. It is a no-op on a database whose schema predates the trigger.
func ArmFindingWriteStop(ctx context.Context, db *sql.DB) error {
	return setFindingWriteStop(ctx, db, "ENABLE")
}

func setFindingWriteStop(ctx context.Context, db *sql.DB, verb string) error {
	var present bool
	if err := db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM pg_trigger
    WHERE tgrelid = to_regclass('fact_records') AND tgname = $1 AND NOT tgisinternal)`,
		FindingWriteStopTrigger).Scan(&present); err != nil {
		return fmt.Errorf("probe finding write stop trigger: %w", err)
	}
	if !present {
		return nil
	}
	// The identifier is a package constant, not input.
	if _, err := db.ExecContext(ctx, "ALTER TABLE fact_records "+verb+" TRIGGER "+FindingWriteStopTrigger); err != nil {
		return fmt.Errorf("%s finding write stop trigger: %w", verb, err)
	}
	return nil
}

// failer is the part of testing.TB the helpers use, so a hermetic test can script it.
type failer interface {
	Helper()
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
}

// rearmFindingWriteStop re-enables the trigger and fails the test unless it reads enabled
// afterwards. It runs at the end of a scoped seed or a whole-test disarm, possibly after the
// test context ended, so it uses its own bounded context.
func rearmFindingWriteStop(t failer, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ArmFindingWriteStop(ctx, db); err != nil {
		t.Errorf("re-arm the finding write stop: %v", err)
		return
	}
	armed, err := FindingWriteStopArmed(ctx, db)
	switch {
	case err != nil:
		t.Errorf("verify the finding write stop is armed: %v", err)
	case !armed:
		t.Errorf("the finding write stop reads disabled after the re-arm: a later write of the kind would pass unchecked")
	}
}

// SeedFindingFacts disables the finding write stop, runs seed, and re-enables it, failing
// the test if it cannot re-arm. Use it around a raw INSERT or UPDATE that plants a frozen
// finding fact: the code under test then runs armed, so a writer, retraction or recovery
// path that still wrote a finding fact would fail with 55000. seed must not run the code
// under test, and must not run concurrently with it on the same database: the stop is
// disabled for the whole window.
func SeedFindingFacts(t failer, db *sql.DB, seed func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := DisarmFindingWriteStop(ctx, db)
	cancel()
	if err != nil {
		t.Fatalf("disarm the finding write stop to seed: %v", err)
		return
	}
	defer rearmFindingWriteStop(t, db)
	seed()
}

// DisarmFindingWriteStopForTest disables the finding write stop for the whole test and
// re-enables it at cleanup, failing the test if it cannot re-arm. It is for a test that
// models a schema from before C2 (the old-writer fence, the backfill, drain and
// verification interleavings, the legacy reads until PR-C2 m removes them); a test of C2
// code uses SeedFindingFacts instead. Every caller is in the shrink-only allowlist.
func DisarmFindingWriteStopForTest(t interface {
	failer
	Cleanup(func())
}, db *sql.DB,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := DisarmFindingWriteStop(ctx, db)
	cancel()
	if err != nil {
		t.Fatalf("disarm the finding write stop for the test: %v", err)
		return
	}
	t.Cleanup(func() { rearmFindingWriteStop(t, db) })
}
