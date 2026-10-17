// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/rebuild/reset"
)

// Interleaving proofs for the #8283 fingerprint gate on a real database. They
// drive the production claim, ACK, reopen, fingerprint-write, and reset paths on
// separate connections and fix the order with explicit transactions, so each
// asserts one named overlap rather than hoping a race happens.

const interleaveLeaseOwner = "reducer-8283-interleave"

func (f *fingerprintFixture) queue() ReducerQueue {
	return ReducerQueue{
		database:      SQLDB{DB: f.database},
		LeaseOwner:    interleaveLeaseOwner,
		LeaseDuration: time.Minute,
		ClaimDomain:   reducer.DomainDeployableUnitCorrelation,
	}
}

// claim claims the one pending deployable_unit_correlation item.
func (f *fingerprintFixture) claim(t *testing.T) reducer.Intent {
	t.Helper()
	intent, ok, err := f.queue().Claim(f.ctx)
	if err != nil || !ok || intent.IntentID != fingerprintItemID {
		t.Fatalf("claim = (%+v, ok=%t, err=%v), want %s", intent, ok, err, fingerprintItemID)
	}
	return intent
}

func (f *fingerprintFixture) ack(t *testing.T, intent reducer.Intent) {
	t.Helper()
	if err := f.queue().Ack(f.ctx, intent, reducer.Result{Status: reducer.ResultStatusSucceeded}); err != nil {
		t.Fatalf("ack: %v", err)
	}
}

func (f *fingerprintFixture) attemptCount(t *testing.T) int {
	t.Helper()
	var attempts int
	if err := f.database.QueryRowContext(f.ctx,
		`SELECT attempt_count FROM fact_work_items WHERE work_item_id = $1`, fingerprintItemID,
	).Scan(&attempts); err != nil {
		t.Fatalf("read attempt_count: %v", err)
	}
	return attempts
}

func (f *fingerprintFixture) fingerprintRows(t *testing.T) int {
	t.Helper()
	var rows int
	if err := f.database.QueryRowContext(f.ctx,
		`SELECT count(*) FROM reducer_work_input_fingerprints WHERE work_item_id = $1`, fingerprintItemID,
	).Scan(&rows); err != nil {
		t.Fatalf("count fingerprint rows: %v", err)
	}
	return rows
}

// TestInputFingerprintInterleavePassVsClaimVsAckVsWrite walks one item through
// an overlap of two maintenance passes, a worker claim, the handler's
// fingerprint write, and the ACK:
//
//  1. pass A lists the item as changed in its snapshot and stops before its
//     reopen UPDATE;
//  2. pass B reopens the item and commits;
//  3. a worker claims it;
//  4. pass A resumes and runs its reopen UPDATE: the status guard makes it a
//     no-op, so the claimed item is not reopened twice and no lease is lost;
//  5. the worker records a fresh fingerprint and ACKs;
//  6. the next pass finds the item unchanged and leaves it succeeded;
//  7. a foreign change after that is not lost: the next pass reopens it.
func TestInputFingerprintInterleavePassVsClaimVsAckVsWrite(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)
	f.recordFingerprint(t)
	f.reingestScope(t, "git:scope-b", "gen-b2", "repo-b", fingerprintDepRepoID)

	txA, err := f.database.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin pass A: %v", err)
	}
	defer func() { _ = txA.Rollback() }()
	listing, err := listSucceededReducerWorkItemsForDomain(f.ctx, SQLTx{Tx: txA}, fingerprintDomain)
	if err != nil || len(listing.reopenIDs) != 1 || listing.reopenIDs[0] != fingerprintItemID {
		t.Fatalf("pass A listing = (%+v, %v), want the item as changed", listing, err)
	}

	f.runPass(t) // pass B reopens it and commits
	if got := f.status(t); got != "pending" {
		t.Fatalf("status after pass B = %q, want pending", got)
	}
	intent := f.claim(t)
	attemptsClaimed := f.attemptCount(t)

	reopened, err := ReducerQueue{database: SQLTx{Tx: txA}, Now: time.Now}.ReopenSucceeded(f.ctx, fingerprintItemID)
	if err != nil {
		t.Fatalf("pass A reopen UPDATE: %v", err)
	}
	if reopened {
		t.Fatal("pass A reopened a claimed item: the status guard failed and the item would run twice")
	}
	if err := txA.Commit(); err != nil {
		t.Fatalf("commit pass A: %v", err)
	}
	if got := f.status(t); got != "claimed" || f.attemptCount(t) != attemptsClaimed {
		t.Fatalf("after pass A resumed: status=%q attempts=%d, want claimed with attempts %d", got, f.attemptCount(t), attemptsClaimed)
	}

	f.recordFingerprint(t) // the handler's write: a fresh read after the foreign change
	f.ack(t, intent)
	if got := f.status(t); got != "succeeded" {
		t.Fatalf("status after ack = %q, want succeeded", got)
	}
	f.runPass(t)
	if got := f.status(t); got != "succeeded" {
		t.Fatalf("status after a pass over an unchanged item = %q, want succeeded", got)
	}

	f.reingestScope(t, "git:scope-b", "gen-b3", "repo-b", fingerprintDepRepoID)
	f.runPass(t)
	if got := f.status(t); got != "pending" {
		t.Fatalf("status after a later foreign change = %q, want pending (a lost reopen)", got)
	}
}

// TestInputFingerprintInterleaveActivationCommitsWhileClaimed pins the in-flight
// case with the real claim and ACK: the item is claimed, its read snapshot (the
// recorded fingerprint) predates a foreign activation that then commits, the
// pass that runs while the item is claimed leaves it alone, and the pass after
// the ACK reopens it.
func TestInputFingerprintInterleaveActivationCommitsWhileClaimed(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)
	f.runPass(t) // no row: reopens
	intent := f.claim(t)
	f.recordFingerprint(t) // read snapshot before the activation
	f.reingestScope(t, "git:scope-b", "gen-b2", "repo-b", fingerprintDepRepoID)

	f.runPass(t)
	if got := f.status(t); got != "claimed" {
		t.Fatalf("claimed item status after a pass = %q, want claimed", got)
	}
	f.ack(t, intent)
	f.runPass(t)
	if got := f.status(t); got != "pending" {
		t.Fatalf("status on the pass after the ack = %q, want pending", got)
	}
}

// TestInputFingerprintInterleaveActivationVsReadSnapshot holds a foreign
// activation uncommitted while the fingerprint is read. The read snapshot must
// not see it; once it commits, the recorded value no longer matches, so the next
// pass reopens the item.
func TestInputFingerprintInterleaveActivationVsReadSnapshot(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)

	activation, err := f.database.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin activation: %v", err)
	}
	defer func() { _ = activation.Rollback() }()
	for _, stmt := range []string{
		`UPDATE scope_generations SET status = 'superseded' WHERE scope_id = 'git:scope-b'`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
		 VALUES ('gen-b2', 'git:scope-b', 'snapshot', now(), now(), 'active')`,
		`UPDATE ingestion_scopes SET active_generation_id = 'gen-b2' WHERE scope_id = 'git:scope-b'`,
		`UPDATE relationship_generations SET status = 'superseded' WHERE generation_id = 'gen-b1'`,
		`INSERT INTO relationship_generations (generation_id, scope, status, created_at, activated_at)
		 VALUES ('gen-b2', 'git:scope-b', 'active', now(), now())`,
		`INSERT INTO resolved_relationships
		   (resolved_id, generation_id, source_repo_id, target_repo_id, relationship_type,
		    confidence, evidence_count, rationale, resolution_source, details)
		 VALUES ('b2-row', 'gen-b2', 'repo-b', 'repo-dep', 'DEPLOYS_FROM', 0.9, 1, 'fixture', 'inferred', '{}'::jsonb)`,
	} {
		if _, err := activation.ExecContext(f.ctx, stmt); err != nil {
			t.Fatalf("uncommitted activation: %v", err)
		}
	}

	before := f.recordFingerprint(t) // snapshot excludes the uncommitted activation
	if err := activation.Commit(); err != nil {
		t.Fatalf("commit activation: %v", err)
	}
	_, _, after, err := f.relations.GetResolvedRelationshipsForReposWithCorpusFenceAndInputFingerprint(
		f.ctx, fingerprintOwnGen, fingerprintCandidateRepoIDs)
	if err != nil || after == "" || after == before {
		t.Fatalf("fingerprint after commit = %q (err %v), want a value different from %q", after, err, before)
	}
	f.runPass(t)
	if got := f.status(t); got != "pending" {
		t.Fatalf("status after the activation committed behind the read snapshot = %q, want pending", got)
	}
}

// TestInputFingerprintInterleaveConcurrentWritersOnDistinctItems has many
// handlers record fingerprints and a worker ACK distinct items at once, with a
// short lock_timeout on every connection. A row-lock overlap between distinct
// items would surface as a 55P03 or a deadlock; none must.
func TestInputFingerprintInterleaveConcurrentWritersOnDistinctItems(t *testing.T) {
	f := newFingerprintFixture(t)
	const items = 48
	for i := 0; i < items; i++ {
		seedSucceededReopenWorkItem(t, f.ctx, f.database, fmt.Sprintf("work-duc-%03d", i),
			fingerprintOwnScope, fingerprintOwnGen, fingerprintDomain, f.ackedAt)
	}
	_, _, fingerprint, err := f.relations.GetResolvedRelationshipsForReposWithCorpusFenceAndInputFingerprint(
		f.ctx, fingerprintOwnGen, fingerprintCandidateRepoIDs)
	if err != nil || fingerprint == "" {
		t.Fatalf("fingerprint read = (%q, %v)", fingerprint, err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, items*2)
	for i := 0; i < items; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := f.database.Conn(f.ctx)
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = conn.Close() }()
			if _, err := conn.ExecContext(f.ctx, `SET lock_timeout = '3s'`); err != nil {
				errs <- err
				return
			}
			id := fmt.Sprintf("work-duc-%03d", i)
			store := NewInputFingerprintStore(pinnedConn{conn})
			for round := 0; round < 3; round++ {
				if err := store.RecordDeployableUnitInputFingerprint(f.ctx, id, fingerprintCandidateRepoIDs, fingerprint); err != nil {
					errs <- fmt.Errorf("record %s: %w", id, err)
					return
				}
				if _, err := conn.ExecContext(f.ctx,
					`UPDATE fact_work_items SET status = 'succeeded', updated_at = now() WHERE work_item_id = $1`, id,
				); err != nil {
					errs <- fmt.Errorf("ack-like update %s: %w", id, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent writer: %v", err)
	}
	var rows int
	if err := f.database.QueryRowContext(f.ctx,
		`SELECT count(*) FROM reducer_work_input_fingerprints WHERE work_item_id LIKE 'work-duc-%'`).Scan(&rows); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rows != items {
		t.Fatalf("fingerprint rows = %d, want %d (one per item, idempotent upserts)", rows, items)
	}
}

// TestInputFingerprintInterleaveRetryAfterFailedAckIsIdempotent fails the first
// ACK (the lease owner no longer matches), re-runs the handler's write, and
// ACKs again. The row count stays one and the value is the newest read.
func TestInputFingerprintInterleaveRetryAfterFailedAckIsIdempotent(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)
	f.runPass(t)
	intent := f.claim(t)
	first := f.recordFingerprint(t)

	stale := ReducerQueue{
		database: SQLDB{DB: f.database}, LeaseOwner: "another-worker", LeaseDuration: time.Minute,
	}
	if err := stale.Ack(f.ctx, intent, reducer.Result{Status: reducer.ResultStatusSucceeded}); err == nil {
		t.Fatal("ack under the wrong lease owner succeeded, want a claim rejection")
	}
	if got := f.status(t); got == "succeeded" {
		t.Fatalf("a rejected ack left the item %q", got)
	}

	f.reingestScope(t, "git:scope-b", "gen-b2", "repo-b", fingerprintDepRepoID) // before the retry's read
	second := f.recordFingerprint(t)
	if second == first {
		t.Fatalf("retry fingerprint equals the first (%q); the retry should record the newer read", first)
	}
	if f.fingerprintRows(t) != 1 {
		t.Fatalf("fingerprint rows after a retry = %d, want 1", f.fingerprintRows(t))
	}
	f.ack(t, intent)
	f.runPass(t)
	if got := f.status(t); got != "succeeded" {
		t.Fatalf("status after the retry ack and a pass = %q, want succeeded (the retry read the newer state)", got)
	}
}

// TestInputFingerprintInterleaveForeignKeyInsertVsResetDelete holds an
// uncommitted fingerprint insert (its foreign key check takes KEY SHARE on the
// work row) and proves two things. The ACK's non-key UPDATE of the same row is
// compatible and completes at once. The reset path's DELETE of the succeeded
// work item waits for the insert to commit, then cascades the fingerprint row
// away, leaving no orphan and no deadlock.
func TestInputFingerprintInterleaveForeignKeyInsertVsResetDelete(t *testing.T) {
	f := newFingerprintFixture(t)
	f.succeedItem(t)
	f.runPass(t)
	intent := f.claim(t)

	insert, err := f.database.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin insert: %v", err)
	}
	defer func() { _ = insert.Rollback() }()
	if _, err := insert.ExecContext(f.ctx, backfill.UpsertInputFingerprintSQL,
		fingerprintItemID, fingerprintDomain, "{repo-a,repo-dep}", "fp-held", time.Now().UTC()); err != nil {
		t.Fatalf("uncommitted fingerprint insert: %v", err)
	}

	ackCtx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	ackConn, err := f.database.Conn(f.ctx)
	if err != nil {
		t.Fatalf("ack connection: %v", err)
	}
	defer func() { _ = ackConn.Close() }()
	if _, err := ackConn.ExecContext(f.ctx, `SET lock_timeout = '1s'`); err != nil {
		t.Fatalf("set lock_timeout: %v", err)
	}
	ackQueue := f.queue()
	ackQueue.database = pinnedConn{ackConn}
	if err := ackQueue.Ack(ackCtx, intent, reducer.Result{Status: reducer.ResultStatusSucceeded}); err != nil {
		t.Fatalf("ack while the fingerprint insert is uncommitted: %v (the non-key UPDATE must not wait on KEY SHARE)", err)
	}

	resetDone := make(chan error, 1)
	go func() {
		resetTx, err := f.database.BeginTx(f.ctx, nil)
		if err != nil {
			resetDone <- err
			return
		}
		defer func() { _ = resetTx.Rollback() }()
		_, err = reset.ApplyPreRetirement(f.ctx, resetTx, reset.Generations{
			ScopeIDs: []string{fingerprintOwnScope}, GenerationIDs: []string{fingerprintOwnGen},
		})
		if err == nil {
			err = resetTx.Commit()
		}
		resetDone <- err
	}()
	select {
	case err := <-resetDone:
		t.Fatalf("reset DELETE finished (%v) while the foreign-key insert was still uncommitted; it should wait", err)
	case <-time.After(750 * time.Millisecond):
	}
	if err := insert.Commit(); err != nil {
		t.Fatalf("commit fingerprint insert: %v", err)
	}
	if err := <-resetDone; err != nil {
		t.Fatalf("reset after the insert committed: %v", err)
	}
	var workRows, fingerprintRows int
	if err := f.database.QueryRowContext(f.ctx,
		`SELECT (SELECT count(*) FROM fact_work_items WHERE work_item_id = $1),
		        (SELECT count(*) FROM reducer_work_input_fingerprints WHERE work_item_id = $1)`, fingerprintItemID,
	).Scan(&workRows, &fingerprintRows); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if workRows != 0 || fingerprintRows != 0 {
		t.Fatalf("after reset: work rows=%d fingerprint rows=%d, want 0 and 0 (the cascade removes the row)", workRows, fingerprintRows)
	}
}

// pinnedConn adapts one *sql.Conn, so a session setting such as lock_timeout
// applies to every statement a store or queue issues through it.
type pinnedConn struct{ conn *sql.Conn }

func (p pinnedConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return p.conn.ExecContext(ctx, query, args...)
}

func (p pinnedConn) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return p.conn.QueryContext(ctx, query, args...)
}
