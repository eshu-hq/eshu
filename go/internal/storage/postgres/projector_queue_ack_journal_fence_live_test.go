// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7127 PR-3b pg_locks fences: a flag-on Ack paused after its insert holds only the shared (7127, 0) key besides its row locks, and the link writer, a journal pass and retention return without waiting; link-writer locks held do not stop the Ack; with retention holding the scope row the Ack waits on the same transactionid lock with the journal on and off; disposable database from ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN (env Skip); about 2 s locally; enrolled in the live-postgres-readiness runner; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// ajFenceRetention is a retention store and policy that prune every
// superseded generation, so a prune that is not fenced off shows.
func ajFenceRetention(d *sql.DB) (GenerationRetentionStore, GenerationRetentionPolicy) {
	retention := NewGenerationRetentionStore(SQLDB{DB: d})
	retention.Now = func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }
	return retention, GenerationRetentionPolicy{
		MinSupersededGenerations: 0, MaxSupersededAge: time.Minute, HardMaxSupersededAge: time.Minute,
		BatchGenerationLimit: 100, BatchRowLimit: 1_000_000,
	}
}

func ajSeedGold(t *testing.T, d *sql.DB, scopeID string) {
	t.Helper()
	ajExec(t, d, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at,
        status, activated_at, superseded_at) VALUES ($1, $2, 'snapshot', FALSE, now(), now(), 'superseded',
        now() - interval '3 hours', now() - interval '2 hours')`, scopeID+"-gold", scopeID)
}

// ajBackendLocks lists a backend's relation, tuple and advisory locks.
func ajBackendLocks(t *testing.T, d *sql.DB, pid int) []string {
	t.Helper()
	rows, err := d.Query(`SELECT locktype || ':' || COALESCE(relation::regclass::text, '') || ':' ||
            COALESCE(classid::text, '') || '/' || COALESCE(objid::text, '') || '/' || COALESCE(objsubid::text, '') || ':' || mode || ':' || granted
        FROM pg_locks WHERE pid = $1 AND locktype IN ('relation', 'advisory', 'tuple') ORDER BY 1`, pid)
	if err != nil {
		t.Fatalf("pg_locks: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var lock string
		if err := rows.Scan(&lock); err != nil {
			t.Fatalf("scan lock: %v", err)
		}
		out = append(out, lock)
	}
	return out
}

// TestProjectorAckJournalLockFences proves, from pg_locks, that the journal
// write adds no wait to the Ack and that the link writer, the journal pass
// and generation retention never wait on it (#7127 PR-3b):
//
//   - A: a flag-on Ack paused after its insert holds the advisory ShareLock
//     on (7127, 0) and nothing else new; the link writer on its scope and on
//     another scope, a journal pass (skipped), and retention all return while
//     it is paused, no backend waits, and retention keeps the paused scope's
//     superseded generation and prunes the other scope's.
//   - B: with a link transaction's locks held on the scope (cursor row,
//     generation KEY SHARE, link slot) a flag-on Ack completes and journals.
//   - C: with retention holding the scope row, the Ack waits on it, with the
//     journal on exactly as with it off, and completes once it is released.
func TestProjectorAckJournalLockFences(t *testing.T) {
	d := openAckJournalDB(t, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	journal := linksfreshnessstore.NewJournalStore(SQLDB{DB: d})

	t.Run("A ack paused after its insert", func(t *testing.T) {
		for _, s := range []string{"fa", "fa-other"} {
			ajSeedScope(t, d, s)
			ajSeedGold(t, d, s)
		}
		if _, err := journal.Journal(ctx, 100); err != nil {
			t.Fatalf("journal: %v", err)
		}
		ajDrainLinks(t, d, "fa")
		pid, release, done := ajPausedAck(t, d, ajPending(t, d, "fa", "fa-g1", 1), "")
		defer release()
		locks := ajBackendLocks(t, d, pid)
		if !slices.Contains(locks, "advisory::7127/0/2:ShareLock:true") {
			t.Fatalf("paused Ack does not hold the shared journal key: %v", locks)
		}
		for _, lock := range locks {
			if strings.HasPrefix(lock, "advisory:") && lock != "advisory::7127/0/2:ShareLock:true" {
				t.Fatalf("paused Ack holds an unexpected advisory lock %s", lock)
			}
		}
		writer := linksfreshnessstore.NewLinkWriter(SQLDB{DB: d})
		if _, err := writer.LinkNext(ctx, "fa"); err != nil {
			t.Fatalf("link writer on the paused scope: %v", err)
		}
		other, err := writer.LinkNext(ctx, "fa-other")
		if err != nil || other.Idle {
			t.Fatalf("link writer on another scope = %+v, %v; want a link", other, err)
		}
		pass, err := journal.Journal(ctx, 100)
		if err != nil || !pass.Skipped {
			t.Fatalf("journal pass during the paused Ack = %+v, %v; want skipped", pass, err)
		}
		retention, policy := ajFenceRetention(d)
		pruned, err := retention.PruneSupersededGenerations(ctx, policy)
		if err != nil {
			t.Fatalf("retention during the paused Ack: %v", err)
		}
		waiting := ajUngrantedLocks(t, d)
		release()
		if err := <-done; err != nil {
			t.Fatalf("Ack: %v", err)
		}
		kept := ajCount(t, d, `SELECT count(*) FROM scope_generations WHERE generation_id = 'fa-gold'`)
		gone := ajCount(t, d, `SELECT count(*) FROM scope_generations WHERE generation_id = 'fa-other-gold'`)
		t.Logf("A: Ack locks %v; retention pruned %d; ungranted locks %d", locks, pruned.GenerationsPruned, waiting)
		if waiting != 0 || kept != 1 || gone != 0 {
			t.Fatalf("A: ungranted=%d, paused scope's superseded kept=%d (want 1), other scope's left=%d (want 0)", waiting, kept, gone)
		}
		if ajSeq(t, d, "fa", "fa-g1") <= 0 {
			t.Fatal("A: the paused Ack's row did not commit")
		}
	})

	t.Run("B link writer locks held", func(t *testing.T) {
		ajSeedScope(t, d, "fb")
		if _, err := journal.Journal(ctx, 100); err != nil {
			t.Fatalf("journal: %v", err)
		}
		work := ajPending(t, d, "fb", "fb-g1", 1)
		holder, err := d.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = holder.Rollback() }()
		for _, stmt := range []struct {
			query string
			args  []any
		}{
			{`SELECT 1 FROM changed_since_scope_cursor WHERE scope_id = 'fb' FOR UPDATE SKIP LOCKED`, nil},
			{`SELECT 1 FROM scope_generations WHERE generation_id = 'fb-g0' FOR KEY SHARE SKIP LOCKED`, nil},
			{`SELECT pg_try_advisory_xact_lock($1::integer, 1)`, []any{linksfreshnessstore.SlotLockClass}},
			{`INSERT INTO changed_since_links (scope_id, generation_id, prior_generation_id, link_kind, digest_version,
                delta_rows, files_keys, content_entities_keys, facts_keys, computed_at)
                VALUES ('fb', 'fb-g0', '', 'root', 1, 0, 0, 0, 0, now())`, nil},
		} {
			if _, err := holder.ExecContext(ctx, stmt.query, stmt.args...); err != nil {
				t.Fatalf("hold link locks: %v", err)
			}
		}
		if err := ajQueue(SQLDB{DB: d}, true).Ack(ctx, work, runtime.Result{}); err != nil {
			t.Fatalf("B: Ack with link locks held: %v", err)
		}
		if ajSeq(t, d, "fb", "fb-g1") <= 0 {
			t.Fatal("B: Ack did not journal")
		}
	})

	t.Run("C retention holds the scope row", func(t *testing.T) {
		var waits [2][]string
		for i, journalOn := range []bool{false, true} {
			scopeID := []string{"fc-off", "fc-on"}[i]
			ajSeedScope(t, d, scopeID)
			ajSeedGold(t, d, scopeID)
			work := ajPending(t, d, scopeID, scopeID+"-g1", 1)
			holder, err := d.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			if _, err := holder.ExecContext(ctx, `SELECT 1 FROM ingestion_scopes WHERE scope_id = $1 FOR UPDATE SKIP LOCKED`, scopeID); err != nil {
				t.Fatalf("hold scope: %v", err)
			}
			if _, err := holder.ExecContext(ctx, `SELECT 1 FROM scope_generations WHERE generation_id = $1 FOR UPDATE SKIP LOCKED`, scopeID+"-gold"); err != nil {
				t.Fatalf("hold generation: %v", err)
			}
			q := ajQueue(SQLDB{DB: d}, journalOn)
			q.AckScopeLockTimeout = 4 * time.Second
			done := make(chan error, 1)
			go func() { done <- q.Ack(ctx, work, runtime.Result{}) }()
			waits[i] = ajAwaitWaiter(t, d)
			if err := holder.Commit(); err != nil {
				t.Fatalf("release: %v", err)
			}
			if err := <-done; err != nil {
				t.Fatalf("C: Ack after release (journal %v): %v", journalOn, err)
			}
			if got := ajSeq(t, d, scopeID, scopeID+"-g1") > 0; got != journalOn {
				t.Fatalf("C: journal row present = %v, want %v", got, journalOn)
			}
		}
		t.Logf("C: Ack waits off=%v on=%v", waits[0], waits[1])
		if !slices.Equal(waits[0], waits[1]) {
			t.Fatalf("C: the flag-on Ack waits on %v, the flag-off Ack on %v", waits[1], waits[0])
		}
	})
}

// ajAwaitWaiter returns the lock types the database's waiting backends wait
// on, once one waits.
func ajAwaitWaiter(t *testing.T, d *sql.DB) []string {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		rows, err := d.Query(`SELECT l.locktype || ':' || l.mode FROM pg_locks AS l JOIN pg_stat_activity AS a ON a.pid = l.pid
            WHERE NOT l.granted AND a.datname = current_database() ORDER BY 1`)
		if err != nil {
			t.Fatalf("pg_locks: %v", err)
		}
		var waits []string
		for rows.Next() {
			var w string
			_ = rows.Scan(&w)
			waits = append(waits, w)
		}
		_ = rows.Close()
		if len(waits) > 0 {
			return waits
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the Ack never waited on the held scope row")
	return nil
}
