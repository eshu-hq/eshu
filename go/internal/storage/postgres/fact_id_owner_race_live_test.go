// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11203 two-transaction race proof on disposable PostgreSQL 18: two scopes writing one brand-new fact id in concurrent transactions, in both orders, leave exactly one owner and refuse the other with the typed owner-conflict error once the first commits (the second statement waits on the first's insert, then re-evaluates the conflict WHERE); also a free-for-all round with no forced order; reuses the #7978 bootstrapped proof schema; wall time logged not gated
//eshu:readiness:env ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// raceSave runs one scope's streaming save of one fact in its own transaction
// and, when commit is true, commits it. It returns the save error.
func raceSave(
	ctx context.Context,
	p *reprojectProof,
	scopeID, generationID, factID string,
	started chan<- struct{},
	release <-chan struct{},
) error {
	tx, err := SQLDB{DB: p.db}.Begin(ctx)
	if err != nil {
		return err
	}
	stream := make(chan facts.Envelope, 1)
	stream <- ownerRefusalFile(scopeID, generationID, factID, time.Now().UTC(), scopeID)
	close(stream)
	if started != nil {
		close(started)
	}
	_, saveErr := upsertStreamingFacts(ctx, tx, stream, scopeID, generationID, nil)
	if release != nil {
		<-release
	}
	if saveErr != nil {
		_ = tx.Rollback()
		return saveErr
	}
	return tx.Commit()
}

// waitForSecondFactWriterBlocked polls until some other backend is waiting on a lock (the
// second transaction is blocked on the first one's uncommitted insert).
func waitForSecondFactWriterBlocked(t *testing.T, ctx context.Context, p *reprojectProof) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := p.db.QueryRowContext(ctx,
			`SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND datname = current_database() AND pid <> pg_backend_pid()`).Scan(&n); err != nil {
			t.Fatalf("poll lock waits: %v", err)
		}
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the second transaction never blocked on the first one's insert")
}

// TestFactIDOwnerRaceTwoScopesLive: with a deterministic order (first scope's
// insert done and uncommitted, second scope's statement blocked on it, then
// the first commits) exactly one scope owns the row and the other gets the
// typed conflict. Both orders run, so each scope wins once per round.
func TestFactIDOwnerRaceTwoScopesLive(t *testing.T) {
	p := openReprojectProof(t)
	ctx := context.Background()
	now := time.Now().UTC()
	seedUnchangedSkipScope(t, ctx, p.db, "scope-r1", "gen-r1", now)
	seedUnchangedSkipScope(t, ctx, p.db, "scope-r2", "gen-r2", now)
	type side struct{ scope, generation string }
	sides := [2]side{{"scope-r1", "gen-r1"}, {"scope-r2", "gen-r2"}}

	wins := map[string]int{}
	const rounds = 6
	for round := 0; round < rounds; round++ {
		first, second := sides[round%2], sides[(round+1)%2] // both orders
		factID := fmt.Sprintf("race-fact-%d", round)

		started := make(chan struct{})
		release := make(chan struct{})
		firstErr := make(chan error, 1)
		go func() { firstErr <- raceSave(ctx, p, first.scope, first.generation, factID, started, release) }()
		<-started
		// Let the first transaction's insert run, then start the second.
		time.Sleep(150 * time.Millisecond)
		secondErr := make(chan error, 1)
		go func() { secondErr <- raceSave(ctx, p, second.scope, second.generation, factID, nil, nil) }()
		waitForSecondFactWriterBlocked(t, ctx, p)
		close(release) // the first commits; the second wakes and re-evaluates
		if err := <-firstErr; err != nil {
			t.Fatalf("round %d: the first writer failed: %v", round, err)
		}
		var conflict *FactIDOwnerConflictError
		if err := <-secondErr; !errors.As(err, &conflict) {
			t.Fatalf("round %d: the second writer error = %v, want *FactIDOwnerConflictError", round, err)
		} else if conflict.Count != 1 || conflict.ByReason["scope"] != 1 {
			t.Fatalf("round %d: conflict = %+v, want one scope conflict", round, conflict)
		}
		owner := p.queryString(`SELECT scope_id FROM fact_records WHERE fact_id = $1`, factID)
		if owner != first.scope {
			t.Fatalf("round %d: owner = %q, want the first committer %q", round, owner, first.scope)
		}
		wins[owner]++
	}
	if wins["scope-r1"] != rounds/2 || wins["scope-r2"] != rounds/2 {
		t.Fatalf("wins = %v, want each scope to win %d rounds (both orders exercised)", wins, rounds/2)
	}
}

// TestFactIDOwnerRaceFreeForAllLive runs many concurrent two-scope races with
// no forced order: whatever the interleaving, each id ends with exactly one
// owner and the other scope's save is either a typed conflict or (when it
// started after the commit) also a typed conflict; neither ever errors
// otherwise, deadlocks, or overwrites the owner.
func TestFactIDOwnerRaceFreeForAllLive(t *testing.T) {
	p := openReprojectProof(t)
	ctx := context.Background()
	now := time.Now().UTC()
	seedUnchangedSkipScope(t, ctx, p.db, "scope-f1", "gen-f1", now)
	seedUnchangedSkipScope(t, ctx, p.db, "scope-f2", "gen-f2", now)

	const ids = 40
	var wg sync.WaitGroup
	results := make([][2]error, ids)
	for i := 0; i < ids; i++ {
		factID := fmt.Sprintf("free-fact-%d", i)
		for side, ids := range [2][2]string{{"scope-f1", "gen-f1"}, {"scope-f2", "gen-f2"}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i][side] = raceSave(ctx, p, ids[0], ids[1], factID, nil, nil)
			}()
		}
	}
	wg.Wait()
	for i, pair := range results {
		var conflict *FactIDOwnerConflictError
		okCount, conflictCount := 0, 0
		for _, err := range pair {
			switch {
			case err == nil:
				okCount++
			case errors.As(err, &conflict):
				conflictCount++
			default:
				t.Fatalf("id %d: unexpected error %v (a deadlock or a non-conflict failure)", i, err)
			}
		}
		if okCount != 1 || conflictCount != 1 {
			t.Fatalf("id %d: %d commits and %d conflicts, want exactly one of each", i, okCount, conflictCount)
		}
		if got := p.queryString(`SELECT count(*)::text FROM fact_records WHERE fact_id = $1`, fmt.Sprintf("free-fact-%d", i)); got != "1" {
			t.Fatalf("id %d: %s rows, want 1", i, got)
		}
	}
}
