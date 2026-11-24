// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// The live half of the finding-fact write stop (#10880 PR-C2 k). The hermetic test reads
// the migration text; this one installs the trigger and reads the real *pgconn.PgError it
// raises, because the C0 classifier fires only on SQLSTATE 55000 together with the
// constraint name, and the repo's older triggers raise with ERRCODE alone. Unit tests that
// script the PgError by hand cannot catch a trigger written in that style.

const (
	writeStopKind      = "reducer_supply_chain_impact_finding"
	writeStopOtherKind = "reducer_container_image_identity"
	writeStopMigration = "357_supply_chain_impact_finding_fact_write_stop.sql"

	writeStopInsert = `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
    observed_at, ingested_at, is_tombstone, payload, fencing_token)
VALUES ($1, $2, $3, $4, $1, 'synthetic', $1, now(), now(), FALSE, '{"cve_id":"CVE-2026-0001"}'::jsonb, 1)`
	writeStopUpsert = writeStopInsert + `
ON CONFLICT (fact_id) DO UPDATE SET payload = EXCLUDED.payload`
)

func writeStopTriggerState(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var state string
	if err := db.QueryRowContext(ctx, `
SELECT tgenabled::text FROM pg_trigger
WHERE tgrelid = 'fact_records'::regclass AND tgname = $1 AND NOT tgisinternal`,
		postgresproof.FindingWriteStopTrigger).Scan(&state); err != nil {
		t.Fatalf("read the write stop trigger state: %v", err)
	}
	return state
}

// applyWriteStopMigration applies migration 357 through the production applier, as a
// bootstrap would, so a multi-statement file runs the way it ships.
func applyWriteStopMigration(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, def := range postgres.BootstrapDefinitions() {
		if path.Base(def.Path) == writeStopMigration {
			if err := postgres.ApplyDefinitions(ctx, postgres.SQLDB{DB: db}, []postgres.Definition{def}); err != nil {
				t.Fatalf("apply %s: %v", writeStopMigration, err)
			}
			return
		}
	}
	t.Fatalf("bootstrap definitions carry no %s", writeStopMigration)
}

func writeStopFactCount(t *testing.T, ctx context.Context, db *sql.DB, kind string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_records WHERE fact_kind = $1`, kind).Scan(&n); err != nil {
		t.Fatalf("count %s facts: %v", kind, err)
	}
	return n
}

// requireWriteStop asserts err is the stop's real error: the *pgconn.PgError carries
// SQLSTATE 55000, the constraint name and the table, and the reducer's transaction
// classifies it as the retryable, non-counting supply_chain_impact_fact_write_stopped.
func requireWriteStop(t *testing.T, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("error = %v (%T), want a *pgconn.PgError from the trigger", err, err)
	}
	if pgErr.Code != "55000" || pgErr.ConstraintName != "fact_records_reject_supply_chain_impact_finding" || pgErr.TableName != "fact_records" {
		t.Fatalf("PgError code=%q constraint=%q table=%q, want 55000, fact_records_reject_supply_chain_impact_finding, fact_records: the C0 classifier fires only on that pair",
			pgErr.Code, pgErr.ConstraintName, pgErr.TableName)
	}
	if !strings.Contains(pgErr.Message, "supply_chain_impact_finding_supports") {
		t.Fatalf("message %q does not name the supports table", pgErr.Message)
	}
}

// TestSupplyChainImpactSupportsFactWriteStopTriggerLive installs the trigger on a real database
// and proves what the C2 review checks: the raised error carries the pinned code and
// constraint, the reducer's writer transaction classifies it, INSERT, upsert and UPDATE
// of the kind are refused whole, other kinds and DELETE pass, a transaction opened before
// the trigger existed is refused when it writes after it, and re-applying the migration
// neither errors nor re-enables a disabled trigger.
func TestSupplyChainImpactSupportsFactWriteStopTriggerLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	// A proof clone starts disarmed; production has the trigger armed.
	if err := postgresproof.ArmFindingWriteStop(ctx, db); err != nil {
		t.Fatalf("arm the write stop: %v", err)
	}
	if state := writeStopTriggerState(t, ctx, db); state != "O" {
		t.Fatalf("trigger state = %q, want O (enabled)", state)
	}
	scope, generation := replaceSetLiveScope, replaceSetLiveGeneration

	t.Run("insert raises the pinned error and the reducer transaction classifies it", func(t *testing.T) {
		_, err := db.ExecContext(ctx, writeStopInsert, "stop-insert", scope, generation, writeStopKind)
		requireWriteStop(t, err)

		tx, err := (postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}}).BeginSupplyChainImpactTx(ctx)
		if err != nil {
			t.Fatalf("begin the impact transaction: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		_, execErr := tx.ExecContext(ctx, writeStopInsert, "stop-insert-tx", scope, generation, writeStopKind)
		var stopped *postgres.SupplyChainImpactFactWriteStoppedError
		if !errors.As(execErr, &stopped) {
			t.Fatalf("impact transaction error = %v, want *SupplyChainImpactFactWriteStoppedError", execErr)
		}
		requireWriteStop(t, stopped)
		if stopped.FailureClass() != postgres.SupplyChainImpactFactWriteStoppedFailureClass || !reducercontract.IsRetryable(stopped) {
			t.Fatalf("class %q retryable %v, want %q and retryable", stopped.FailureClass(), reducercontract.IsRetryable(stopped), postgres.SupplyChainImpactFactWriteStoppedFailureClass)
		}
		if n := writeStopFactCount(t, ctx, db, writeStopKind); n != 0 {
			t.Fatalf("%d finding facts exist after refused inserts, want 0", n)
		}
	})

	t.Run("upsert raises too", func(t *testing.T) {
		_, err := db.ExecContext(ctx, writeStopUpsert, "stop-upsert", scope, generation, writeStopKind)
		requireWriteStop(t, err)
	})

	t.Run("update of an existing kind row raises", func(t *testing.T) {
		// Seed the row the way a pre-stop writer left it, then arm again.
		if err := postgresproof.DisarmFindingWriteStop(ctx, db); err != nil {
			t.Fatalf("disarm to seed: %v", err)
		}
		if _, err := db.ExecContext(ctx, writeStopInsert, "stop-frozen", scope, generation, writeStopKind); err != nil {
			t.Fatalf("seed a frozen finding fact: %v", err)
		}
		if err := postgresproof.ArmFindingWriteStop(ctx, db); err != nil {
			t.Fatalf("arm: %v", err)
		}
		_, err := db.ExecContext(ctx, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = 'stop-frozen'`)
		requireWriteStop(t, err)
		var tomb bool
		if err := db.QueryRowContext(ctx, `SELECT is_tombstone FROM fact_records WHERE fact_id = 'stop-frozen'`).Scan(&tomb); err != nil || tomb {
			t.Fatalf("frozen row is_tombstone = %v (err %v) after a refused update, want it untouched", tomb, err)
		}
	})

	t.Run("DELETE and other kinds pass", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, writeStopInsert, "stop-other", scope, generation, writeStopOtherKind); err != nil {
			t.Fatalf("insert of another kind: %v", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = 'stop-other'`); err != nil {
			t.Fatalf("update of another kind: %v", err)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM fact_records WHERE fact_id = 'stop-frozen'`); err != nil {
			t.Fatalf("delete of a finding row (retention removes frozen rows): %v", err)
		}
	})

	t.Run("a transaction opened before the trigger existed is refused whole when it writes after", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `DROP TRIGGER `+postgresproof.FindingWriteStopTrigger+` ON fact_records`); err != nil {
			t.Fatalf("drop the trigger: %v", err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, `SELECT 1`); err != nil {
			t.Fatalf("first statement: %v", err)
		}
		// The roll: the migration applies on another connection after the transaction began.
		applyWriteStopMigration(t, ctx, db)
		if _, err := tx.ExecContext(ctx, writeStopInsert, "stop-tx-other", scope, generation, writeStopOtherKind); err != nil {
			t.Fatalf("other-kind write in the old transaction: %v", err)
		}
		_, err = tx.ExecContext(ctx, writeStopInsert, "stop-tx-kind", scope, generation, writeStopKind)
		requireWriteStop(t, err)
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Fatalf("rollback: %v", err)
		}
		for _, id := range []string{"stop-tx-other", "stop-tx-kind"} {
			var n int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_records WHERE fact_id = $1`, id).Scan(&n); err != nil || n != 0 {
				t.Fatalf("fact %s: count %d (err %v) after the refused transaction, want 0: the whole transaction rolls back", id, n, err)
			}
		}
		if state := writeStopTriggerState(t, ctx, db); state != "O" {
			t.Fatalf("trigger state after the mid-transaction apply = %q, want O", state)
		}
	})

	t.Run("re-applying the migration keeps a disabled trigger disabled", func(t *testing.T) {
		if err := postgresproof.DisarmFindingWriteStop(ctx, db); err != nil {
			t.Fatalf("disarm: %v", err)
		}
		applyWriteStopMigration(t, ctx, db)
		if state := writeStopTriggerState(t, ctx, db); state != "D" {
			t.Fatalf("trigger state after a re-apply = %q, want D: the guard must not re-enable it", state)
		}
		if _, err := db.ExecContext(ctx, writeStopInsert, "stop-disabled", scope, generation, writeStopKind); err != nil {
			t.Fatalf("insert with the trigger disabled (the rollback switch): %v", err)
		}
	})
}
