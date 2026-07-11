// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// ownerLedgerUpsertWhereReturning is the WHERE-guarded max-resolution upsert
// (identical semantics to ownerLedgerUpsert) with RETURNING added. Postgres
// semantics to prove: when the ON CONFLICT DO UPDATE's WHERE is false (the
// incoming order key does not beat the stored one), the row is NOT updated and
// RETURNING yields NO row — so a losing writer learns nothing about the current
// winner from this statement alone and needs a fallback read.
const ownerLedgerUpsertWhereReturning = `
INSERT INTO cloud_resource_owner_probe (uid, source_order_key, value, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (uid) DO UPDATE
    SET source_order_key = EXCLUDED.source_order_key,
        value = EXCLUDED.value,
        updated_at = EXCLUDED.updated_at
    WHERE EXCLUDED.source_order_key > cloud_resource_owner_probe.source_order_key
RETURNING source_order_key, value`

// ownerLedgerUpsertCaseReturning is the always-update variant: the DO UPDATE
// has no WHERE, so the conflict arm always fires and RETURNING always yields
// the post-update row; per-column CASE keeps the stored winner when the
// incoming key loses. One statement tells every writer the current winner —
// winners see their own values, losers see the stored winner's values.
// Cost: a losing/equal-key write still produces a new heap tuple version
// (dead-tuple churn), measured in the perf shim.
const ownerLedgerUpsertCaseReturning = `
INSERT INTO cloud_resource_owner_probe (uid, source_order_key, value, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (uid) DO UPDATE
    SET source_order_key = CASE
            WHEN EXCLUDED.source_order_key > cloud_resource_owner_probe.source_order_key
            THEN EXCLUDED.source_order_key
            ELSE cloud_resource_owner_probe.source_order_key END,
        value = CASE
            WHEN EXCLUDED.source_order_key > cloud_resource_owner_probe.source_order_key
            THEN EXCLUDED.value
            ELSE cloud_resource_owner_probe.value END,
        updated_at = now()
RETURNING source_order_key, value`

// TestLiveOwnerLedgerReturningSemantics proves the exact Postgres 18 RETURNING
// semantics the #5007 RETURNING optimization rests on, against the real pinned
// backend (not the manual): what RETURNING yields on fresh insert, on a
// conflict whose WHERE passes, on a conflict whose WHERE fails (lower key),
// and on an equal-key no-op — for both the WHERE-guarded and the
// CASE-always-update mechanics.
func TestLiveOwnerLedgerReturningSemantics(t *testing.T) {
	if strings.TrimSpace(os.Getenv(ownerLedgerProveEnv)) == "" {
		t.Skipf("set %s=1 to run the #5007 RETURNING semantics probe", ownerLedgerProveEnv)
	}
	dsn := strings.TrimSpace(os.Getenv(ownerLedgerPGDSNEnv))
	if dsn == "" {
		t.Fatalf("%s is required", ownerLedgerPGDSNEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, ownerLedgerDDL); err != nil {
		t.Fatalf("create owner ledger table: %v", err)
	}

	upsertReturning := func(stmt, uid, key, value string) (retKey, retValue string, returned bool) {
		t.Helper()
		err := db.QueryRowContext(ctx, stmt, uid, key, value).Scan(&retKey, &retValue)
		if err == sql.ErrNoRows {
			return "", "", false
		}
		if err != nil {
			t.Fatalf("upsert returning (%s,%s): %v", uid, key, err)
		}
		return retKey, retValue, true
	}

	t.Run("where_guarded", func(t *testing.T) {
		uid := "ret-sem-where"
		if _, err := db.ExecContext(ctx, "DELETE FROM cloud_resource_owner_probe WHERE uid = $1", uid); err != nil {
			t.Fatalf("reset: %v", err)
		}
		// Fresh insert: RETURNING yields the inserted row.
		if k, v, ok := upsertReturning(ownerLedgerUpsertWhereReturning, uid, "2000-b", "vb"); !ok || k != "2000-b" || v != "vb" {
			t.Errorf("fresh insert: got (%q,%q,returned=%v), want (2000-b,vb,true)", k, v, ok)
		}
		// Higher key: WHERE passes, RETURNING yields the updated row.
		if k, v, ok := upsertReturning(ownerLedgerUpsertWhereReturning, uid, "3000-c", "vc"); !ok || k != "3000-c" || v != "vc" {
			t.Errorf("winning update: got (%q,%q,returned=%v), want (3000-c,vc,true)", k, v, ok)
		}
		// Lower key: WHERE false. THE semantics under proof: the row is not
		// updated and RETURNING yields NO row — the losing writer is blind.
		if k, v, ok := upsertReturning(ownerLedgerUpsertWhereReturning, uid, "1000-a", "va"); ok {
			t.Errorf("losing update: RETURNING unexpectedly yielded (%q,%q); Postgres should suppress the row when the DO UPDATE WHERE is false", k, v)
		} else {
			t.Logf("PROVEN: ON CONFLICT DO UPDATE ... WHERE false returns NO row from RETURNING (losing writer learns nothing)")
		}
		// Equal key (duplicate replay): strict > is false, same blindness.
		if _, _, ok := upsertReturning(ownerLedgerUpsertWhereReturning, uid, "3000-c", "vc"); ok {
			t.Errorf("equal-key replay: RETURNING unexpectedly yielded a row; strict > must suppress it")
		}
		// The stored winner is intact after the losing attempts.
		if k, v, err := selectOwner(ctx, db, uid); err != nil || k != "3000-c" || v != "vc" {
			t.Errorf("stored winner: got (%q,%q,%v), want (3000-c,vc)", k, v, err)
		}
	})

	t.Run("case_always_update", func(t *testing.T) {
		uid := "ret-sem-case"
		if _, err := db.ExecContext(ctx, "DELETE FROM cloud_resource_owner_probe WHERE uid = $1", uid); err != nil {
			t.Fatalf("reset: %v", err)
		}
		// Fresh insert: RETURNING yields the inserted row.
		if k, v, ok := upsertReturning(ownerLedgerUpsertCaseReturning, uid, "2000-b", "vb"); !ok || k != "2000-b" || v != "vb" {
			t.Errorf("fresh insert: got (%q,%q,returned=%v), want (2000-b,vb,true)", k, v, ok)
		}
		// Higher key: CASE picks the incoming values; RETURNING yields them.
		if k, v, ok := upsertReturning(ownerLedgerUpsertCaseReturning, uid, "3000-c", "vc"); !ok || k != "3000-c" || v != "vc" {
			t.Errorf("winning update: got (%q,%q,returned=%v), want (3000-c,vc,true)", k, v, ok)
		}
		// Lower key: the conflict arm still fires (no WHERE), CASE keeps the
		// stored winner, and RETURNING yields the CURRENT WINNER — the losing
		// writer learns the winner from the upsert itself, no read-back.
		if k, v, ok := upsertReturning(ownerLedgerUpsertCaseReturning, uid, "1000-a", "va"); !ok || k != "3000-c" || v != "vc" {
			t.Errorf("losing update: got (%q,%q,returned=%v), want the stored winner (3000-c,vc,true)", k, v, ok)
		} else {
			t.Logf("PROVEN: unconditional DO UPDATE with per-column CASE always RETURNs the post-update row = current winner, even for a losing writer")
		}
		// Equal key: same — RETURNING yields the winner.
		if k, v, ok := upsertReturning(ownerLedgerUpsertCaseReturning, uid, "3000-c", "vc"); !ok || k != "3000-c" || v != "vc" {
			t.Errorf("equal-key replay: got (%q,%q,returned=%v), want (3000-c,vc,true)", k, v, ok)
		}
		if k, v, err := selectOwner(ctx, db, uid); err != nil || k != "3000-c" || v != "vc" {
			t.Errorf("stored winner: got (%q,%q,%v), want (3000-c,vc)", k, v, err)
		}
	})
}
