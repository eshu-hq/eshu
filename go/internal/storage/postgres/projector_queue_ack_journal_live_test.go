// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestProjectorAckJournalAtomicity proves the 'ack' activation row of #7127
// PR-3b commits with the Ack and only with it: a successful Ack writes one
// row whose prior is the superseded generation and whose activated_at is the
// generation's; an error after the insert, inside the transaction, and a
// failure at commit both leave no row and the work and generation as they
// were; a superseded target returns ErrWorkSuperseded with no row; and a
// re-ack of a journaled generation keeps its one row.
func TestProjectorAckJournalAtomicity(t *testing.T) {
	d := openAckJournalDB(t, 8)
	ctx := context.Background()
	rowCount := func(scopeID string) int64 {
		return ajCount(t, d, `SELECT count(*) FROM changed_since_activations WHERE scope_id = $1`, scopeID)
	}
	untouched := func(scopeID, generationID string) bool {
		return ajCount(t, d, `SELECT count(*) FROM fact_work_items WHERE generation_id = $1 AND status = 'running'`, generationID) == 1 &&
			ajCount(t, d, `SELECT count(*) FROM scope_generations WHERE generation_id = $1 AND status = 'pending'`, generationID) == 1 &&
			ajCount(t, d, `SELECT count(*) FROM ingestion_scopes WHERE scope_id = $1 AND active_generation_id = $2`, scopeID, scopeID+"-g0") == 1
	}

	t.Run("success", func(t *testing.T) {
		ajSeedScope(t, d, "ok")
		if err := ajQueue(SQLDB{DB: d}, true).Ack(ctx, ajPending(t, d, "ok", "ok-g1", 1), runtime.Result{}); err != nil {
			t.Fatalf("Ack: %v", err)
		}
		n := ajCount(t, d, `SELECT count(*) FROM changed_since_activations AS a
            JOIN scope_generations AS g USING (scope_id, generation_id)
            WHERE a.scope_id = 'ok' AND a.generation_id = 'ok-g1' AND a.source = 'ack'
              AND a.prior_generation_id = 'ok-g0' AND a.activated_at = g.activated_at AND g.status = 'active'`)
		if n != 1 || rowCount("ok") != 1 {
			t.Fatalf("ack rows = %d (scope total %d), want one row with prior ok-g0 and the generation's activated_at", n, rowCount("ok"))
		}
	})

	t.Run("abort after the insert", func(t *testing.T) {
		ajSeedScope(t, d, "ab")
		work := ajPending(t, d, "ab", "ab-g1", 1)
		planted := errors.New("planted abort after the journal insert")
		hookDB := newAJHookDB(d, func(context.Context, db.Transaction) error { return planted })
		err := ajQueue(hookDB, true).Ack(ctx, work, runtime.Result{})
		if !errors.Is(err, planted) {
			t.Fatalf("Ack error = %v, want the planted abort", err)
		}
		if rowCount("ab") != 0 || !untouched("ab", "ab-g1") {
			t.Fatalf("abort left %d journal rows or changed the work, generation or scope", rowCount("ab"))
		}
	})

	t.Run("commit-time failure", func(t *testing.T) {
		ajSeedScope(t, d, "cf")
		work := ajPending(t, d, "cf", "cf-g1", 1)
		ajExec(t, d, `CREATE FUNCTION aj_planted_commit_failure() RETURNS trigger LANGUAGE plpgsql AS
            $$ BEGIN RAISE EXCEPTION 'planted commit failure'; END $$`)
		ajExec(t, d, `CREATE CONSTRAINT TRIGGER aj_planted_commit_failure AFTER INSERT ON changed_since_activations
            DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION aj_planted_commit_failure()`)
		err := ajQueue(SQLDB{DB: d}, true).Ack(ctx, work, runtime.Result{})
		ajExec(t, d, `DROP TRIGGER aj_planted_commit_failure ON changed_since_activations`)
		if err == nil || !strings.Contains(err.Error(), "commit") {
			t.Fatalf("Ack error = %v, want the commit failure", err)
		}
		if rowCount("cf") != 0 || !untouched("cf", "cf-g1") {
			t.Fatalf("commit failure left %d journal rows or changed the work, generation or scope", rowCount("cf"))
		}
	})

	t.Run("superseded target", func(t *testing.T) {
		ajSeedScope(t, d, "sp")
		work := ajPending(t, d, "sp", "sp-g1", 1)
		ajExec(t, d, `UPDATE scope_generations SET status = 'superseded', superseded_at = now() WHERE generation_id = 'sp-g1'`)
		err := ajQueue(SQLDB{DB: d}, true).Ack(ctx, work, runtime.Result{})
		if !errors.Is(err, failure.ErrWorkSuperseded) {
			t.Fatalf("Ack error = %v, want ErrWorkSuperseded", err)
		}
		if rowCount("sp") != 0 {
			t.Fatalf("superseded Ack wrote %d journal rows", rowCount("sp"))
		}
	})

	t.Run("re-ack keeps one row", func(t *testing.T) {
		ajSeedScope(t, d, "re")
		if err := ajQueue(SQLDB{DB: d}, true).Ack(ctx, ajPending(t, d, "re", "re-g1", 1), runtime.Result{}); err != nil {
			t.Fatalf("Ack: %v", err)
		}
		seq := ajSeq(t, d, "re", "re-g1")
		ajRunning(t, d, "re", "re-g1")
		if err := ajQueue(SQLDB{DB: d}, true).Ack(ctx, ajWork("re", "re-g1"), runtime.Result{}); err != nil {
			t.Fatalf("re-ack: %v", err)
		}
		if rowCount("re") != 1 || ajSeq(t, d, "re", "re-g1") != seq {
			t.Fatalf("re-ack left %d rows (seq %d -> %d), want the one original row", rowCount("re"), seq, ajSeq(t, d, "re", "re-g1"))
		}
		prior := ajCount(t, d, `SELECT count(*) FROM changed_since_activations WHERE generation_id = 're-g1' AND prior_generation_id = 're-g0'`)
		if prior != 1 {
			t.Fatal("re-ack rewrote the row's prior")
		}
	})
}

// TestProjectorAckJournalPriorLive pins the prior on a real database: the
// superseded active generation, NULL on a first activation, and NULL when
// the scope had two active generations (ambiguous), as the sweeper writes.
func TestProjectorAckJournalPriorLive(t *testing.T) {
	d := openAckJournalDB(t, 4)
	ctx := context.Background()
	prior := func(scopeID, generationID string) sql.NullString {
		var p sql.NullString
		if err := d.QueryRow(`SELECT prior_generation_id FROM changed_since_activations WHERE scope_id = $1 AND generation_id = $2`,
			scopeID, generationID).Scan(&p); err != nil {
			t.Fatalf("prior of %s: %v", generationID, err)
		}
		return p
	}

	ajSeedScope(t, d, "ps")
	if err := ajQueue(SQLDB{DB: d}, true).Ack(ctx, ajPending(t, d, "ps", "ps-g1", 1), runtime.Result{}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if p := prior("ps", "ps-g1"); !p.Valid || p.String != "ps-g0" {
		t.Fatalf("superseded prior = %v, want ps-g0", p)
	}

	ajExec(t, d, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
        partition_key, observed_at, ingested_at, status) VALUES ('pf', 'repository', 'git', 'pf', 'git', 'pf', now(), now(), 'pending')`)
	if err := ajQueue(SQLDB{DB: d}, true).Ack(ctx, ajPending(t, d, "pf", "pf-g1", 1), runtime.Result{}); err != nil {
		t.Fatalf("first activation Ack: %v", err)
	}
	if p := prior("pf", "pf-g1"); p.Valid {
		t.Fatalf("first activation prior = %q, want NULL", p.String)
	}

	ajSeedScope(t, d, "pa")
	if _, err := d.Exec(`DROP INDEX IF EXISTS scope_generations_active_scope_idx`); err != nil {
		t.Fatalf("drop the one-active index to plant an ambiguous scope: %v", err)
	}
	ajExec(t, d, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at,
        ingested_at, status, activated_at) VALUES ('pa-gx', 'pa', 'snapshot', FALSE, now(), now(), 'active', now() - interval '1 hour')`)
	if err := ajQueue(SQLDB{DB: d}, true).Ack(ctx, ajPending(t, d, "pa", "pa-g1", 1), runtime.Result{}); err != nil {
		t.Fatalf("ambiguous Ack: %v", err)
	}
	if p := prior("pa", "pa-g1"); p.Valid {
		t.Fatalf("ambiguous prior = %q, want NULL", p.String)
	}
}

// TestProjectorAckJournalOffWritesNothing runs Ack with the journal off on a
// real database: no statement names the ledger, an advisory lock, or a
// RETURNING read, and the ledger stays empty.
func TestProjectorAckJournalOffWritesNothing(t *testing.T) {
	d := openAckJournalDB(t, 4)
	ajSeedScope(t, d, "off")
	recorder := newAJHookDB(d, nil)
	if err := ajQueue(recorder, false).Ack(context.Background(), ajPending(t, d, "off", "off-g1", 1), runtime.Result{}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	statements := recorder.statements()
	for _, query := range statements {
		for _, banned := range []string{"changed_since", "advisory", "RETURNING generation_id"} {
			if strings.Contains(query, banned) {
				t.Fatalf("flag-off Ack sent %q, which contains %q", query, banned)
			}
		}
	}
	if n := ajCount(t, d, `SELECT count(*) FROM changed_since_activations`); n != 0 {
		t.Fatalf("flag-off Ack left %d ledger rows", n)
	}
	t.Logf("flag-off Ack: %d statements, none touching the ledger", len(statements))
}
