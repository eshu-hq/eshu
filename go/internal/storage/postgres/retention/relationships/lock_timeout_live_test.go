// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 proof that the batch lock timeout is a real backstop for the one kind of wait the batch cannot skip, a relation-level lock: another session holds ACCESS EXCLUSIVE on relationship_evidence_facts when the batch's evidence delete starts, the batch gives up after its 500 ms lock timeout, counts every generation of the batch as lock_timeout, rolls back whole and leaves every row, and a later pass sweeps them; a lock timeout of zero (which disables it) or of seconds would leave the sweep waiting
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

// TestRelationLockHeldByAnotherSessionEndsTheBatchOnTheLockTimeoutLive holds
// ACCESS EXCLUSIVE on the evidence table from a second session at the moment the
// batch's first statement has run, after the census (which reads that table) is
// done. The batch's evidence delete cannot skip a table lock the way it skips a
// row lock, so it waits, and only the transaction-local lock timeout ends the
// wait: the batch must come back in about 500 ms with every generation counted
// as lock_timeout and nothing deleted. Without a lock timeout (zero disables it)
// the sweep would wait for the lock holder, and with one of seconds it would
// wait that long, so the elapsed time is part of the proof.
func TestRelationLockHeldByAnotherSessionEndsTheBatchOnTheLockTimeoutLive(t *testing.T) {
	f := openLiveFixture(t)
	f.scope(t, "scope-ddl")
	generations := []string{"gen-ddl-1", "gen-ddl-2"}
	for _, id := range generations {
		f.generation(t, "scope-ddl", id, "superseded")
		f.evidence(t, id, 3)
		f.candidates(t, id, 2)
	}

	var holder *sql.Tx
	release := func() {
		if holder != nil {
			_ = holder.Rollback()
		}
	}
	t.Cleanup(release)
	opts := relationshipretention.Options{Deletes: true, RowCap: 1 << 40}
	relationshipretention.SetAfterStatement(&opts, func(stage string) error {
		if stage != relationshipretention.StageSetLockTimeout || holder != nil {
			return nil
		}
		tx, err := f.db.BeginTx(f.ctx, nil)
		if err != nil {
			return err
		}
		holder = tx
		_, err = tx.ExecContext(f.ctx, `LOCK TABLE relationship_evidence_facts IN ACCESS EXCLUSIVE MODE`)
		return err
	})

	ctx, cancel := context.WithTimeout(f.ctx, 12*time.Second)
	defer cancel()
	started := time.Now()
	result, err := relationshipretention.Sweep(ctx, f.database, opts)
	elapsed := time.Since(started)
	release()
	if err != nil {
		t.Fatalf("Sweep after %s: %v (a batch that waits for a table lock without a bound never yields)", elapsed.Round(time.Millisecond), err)
	}
	if holder == nil {
		t.Fatal("the hook never ran: the census listed nothing, so no batch opened")
	}
	t.Logf("sweep gave up after %s with the evidence table locked", elapsed.Round(time.Millisecond))
	if elapsed < 400*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("sweep took %s, want about the 500 ms lock timeout (0.4 to 3 s)", elapsed.Round(time.Millisecond))
	}
	if got := result.Skipped; got[relationshipretention.SkipLockTimeout] != len(generations) ||
		got[relationshipretention.SkipLockBusy] != 0 || got[relationshipretention.SkipIneligible] != 0 ||
		result.GenerationsSwept != 0 || result.Batches != 0 {
		t.Fatalf("pass = swept %d in %d batches, skipped %v; want both generations of the aborted batch counted as lock_timeout",
			result.GenerationsSwept, result.Batches, got)
	}
	for _, id := range generations {
		if got := f.of(t, id); got != (counts{3, 2, 1, 0}) {
			t.Fatalf("%s = %+v after the timed out batch, want every row left {3 2 1 0}", id, got)
		}
	}
	if again := f.sweep(t); again.GenerationsSwept != len(generations) {
		t.Fatalf("next pass swept %d generations, want %d once the table lock is gone", again.GenerationsSwept, len(generations))
	}
}
