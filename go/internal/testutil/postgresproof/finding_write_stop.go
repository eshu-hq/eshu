// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"context"
	"database/sql"
	"fmt"
)

// FindingWriteStopTrigger is the fact_records trigger that refuses every INSERT or
// UPDATE of a reducer_supply_chain_impact_finding row (#10880 PR-C2 k, migration 349).
// The production schema arms it. A proof database is a clone of the bootstrapped
// template, so it carries it too, and many proofs seed finding facts into fact_records
// on purpose: the backfill, drain, verification and key fill read frozen facts, and the
// pre-C2 read paths read facts until PR-C2 m removes them. A clone therefore starts with
// the trigger disabled, and a proof that tests the stop itself arms it with
// ArmFindingWriteStop. The disabled state is per clone: it never reaches the template or
// another clone.
const FindingWriteStopTrigger = "fact_records_reject_supply_chain_impact_finding"

// DisarmFindingWriteStop disables the finding write stop on db's fact_records. It is a
// no-op on a database whose schema predates the trigger.
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

// prepareClone pings a freshly cloned database and disarms the finding write stop on
// it. On any failure it closes both handles and drops the clone, so the caller returns
// the error without cleanup of its own.
func prepareClone(ctx context.Context, target, admin *sql.DB, clone string) error {
	err := target.PingContext(ctx)
	if err != nil {
		err = fmt.Errorf("ping cloned PostgreSQL database: %w", err)
	} else if err = DisarmFindingWriteStop(ctx, target); err != nil {
		err = fmt.Errorf("disarm finding write stop on cloned PostgreSQL database: %w", err)
	}
	if err != nil {
		_ = target.Close()
		_ = dropDisposableDatabaseName(ctx, admin, clone)
		_ = admin.Close()
	}
	return err
}
