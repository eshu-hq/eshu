// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11217 PostgreSQL 18 full-bootstrap concurrency proof for the poison-liveness sweep as the fourth replay-allowance writer: the sweep racing each of the three replay writers with each side holding the row lock first, two concurrent sweeps, and a committed attempt_count change while the sweep waits on the lock; exactly one writer moves the row and the baseline reads the newest committed row; isolated schema, wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"testing"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// requireOneWinner checks the race outcome on gen-a's row: exactly one
// writer moved it, the row carries a new row's allowance with the baseline
// equal to the prior attempt_count, and the sweep's budget counter rose only
// when the sweep was the winner.
func (p *replayProof) requireOneWinner(sweepMoved, otherMoved, priorAttempts int) {
	p.t.Helper()
	if sweepMoved+otherMoved != 1 {
		p.t.Fatalf("sweep moved %d and the other writer %d rows, want exactly one in all", sweepMoved, otherMoved)
	}
	p.requireSwept("gen-a", priorAttempts, sweepMoved)
}

// TestPoisonSweepRacesReplayWritersLive races the sweep against each of the
// three replay writers on one projector dead letter at attempt 3, once with
// the sweep holding the row lock first and once with the replay holding it
// first. The holder runs inside a transaction the test commits only after the
// other writer is queued on the lock, so the lock order is fixed, not left to
// scheduling. Under Read Committed the waiter rechecks status on the newest
// row version and moves nothing. The bounded drain uses SKIP LOCKED and never
// waits: behind a held lock it moves nothing at once.
//
// The admin replay opens its own transaction, so when it must hold the lock
// first a blocker transaction takes the row lock, the admin replay queues
// first behind it and the sweep second; the first waiter takes the lock when
// the blocker commits.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestPoisonSweepRacesReplayWritersLive(t *testing.T) {
	for _, site := range replaySites {
		t.Run(string(site)+"/sweep-holds-first", func(t *testing.T) {
			p := openReplayProof(t)
			p.commit("gen-a")
			dead := p.deadLetterAtThree("gen-a", false)
			holder := p.begin()
			swept, err := sweepWith(p.ctx, pgstatus.SQLTx{Tx: holder}, 1)
			if err != nil || swept != 1 {
				t.Fatalf("held sweep moved %d (%v), want 1", swept, err)
			}
			var writer func() (int, error)
			switch site {
			case replayViaAdmin:
				writer = p.adminReplayWriter(dead.id)
			default:
				bounded := site == replayViaRecoveryBounded
				writer = func() (int, error) { return p.recoveryReplayWith(pgstatus.SQLDB{DB: p.db}, bounded) }
			}
			done := async(writer)
			if site != replayViaRecoveryBounded {
				p.waitForLockWaiters(1)
			}
			if site == replayViaRecoveryBounded {
				// SKIP LOCKED: the drain returns before the holder commits.
				if got := p.await(string(site), done); got != 0 {
					t.Fatalf("bounded drain behind a held lock moved %d rows, want 0", got)
				}
				p.commitTx(holder)
				p.requireOneWinner(swept, 0, dead.attempts)
				return
			}
			p.commitTx(holder)
			p.requireOneWinner(swept, p.await(string(site), done), dead.attempts)
		})
		t.Run(string(site)+"/replay-holds-first", func(t *testing.T) {
			p := openReplayProof(t)
			p.commit("gen-a")
			dead := p.deadLetterAtThree("gen-a", false)
			var replayed int
			var sweepDone <-chan asyncCount
			sweeper := func() (int, error) { return sweepWith(p.ctx, pgstatus.SQLDB{DB: p.db}, 1) }
			switch site {
			case replayViaAdmin:
				blocker := p.begin()
				if _, err := blocker.ExecContext(p.ctx,
					`SELECT 1 FROM fact_work_items WHERE work_item_id = $1 FOR UPDATE`, dead.id); err != nil {
					t.Fatalf("blocker lock: %v", err)
				}
				adminDone := async(p.adminReplayWriter(dead.id))
				p.waitForLockWaiters(1)
				sweepDone = async(sweeper)
				p.waitForLockWaiters(2)
				p.commitTx(blocker)
				replayed = p.await("admin replay", adminDone)
			default:
				holder := p.begin()
				var err error
				replayed, err = p.recoveryReplayWith(pgstatus.SQLTx{Tx: holder}, site == replayViaRecoveryBounded)
				if err != nil || replayed != 1 {
					t.Fatalf("held %s replay moved %d (%v), want 1", site, replayed, err)
				}
				sweepDone = async(sweeper)
				p.waitForLockWaiters(1)
				p.commitTx(holder)
			}
			p.requireOneWinner(p.await("sweep", sweepDone), replayed, dead.attempts)
		})
	}
}

// TestConcurrentPoisonSweepsMoveRowOnceLive races two sweeps on one dead
// letter, the first holding the row lock until the second is queued on it.
// One moves the row and the budget counter rises by exactly one.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestConcurrentPoisonSweepsMoveRowOnceLive(t *testing.T) {
	p := openReplayProof(t)
	p.commit("gen-a")
	dead := p.deadLetterAtThree("gen-a", false)
	holder := p.begin()
	first, err := sweepWith(p.ctx, pgstatus.SQLTx{Tx: holder}, 2)
	if err != nil || first != 1 {
		t.Fatalf("held sweep moved %d (%v), want 1", first, err)
	}
	done := async(func() (int, error) { return sweepWith(p.ctx, pgstatus.SQLDB{DB: p.db}, 2) })
	p.waitForLockWaiters(1)
	p.commitTx(holder)
	if second := p.await("second sweep", done); first+second != 1 {
		t.Fatalf("sweeps moved %d and %d rows, want exactly one in all", first, second)
	}
	p.requireSwept("gen-a", dead.attempts, 1)
}

// TestPoisonSweepReadsCommittedAttemptWhileWaitingLive proves the SET list
// reads the newest committed row version, not the sweep's snapshot. A
// concurrent transaction raises attempt_count on the still-dead row and holds
// the lock; the sweep selects the row from its snapshot (attempt 3), queues
// on the lock, and after the commit writes baseline = the committed value.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestPoisonSweepReadsCommittedAttemptWhileWaitingLive(t *testing.T) {
	const committed = 7
	p := openReplayProof(t)
	p.commit("gen-a")
	dead := p.deadLetterAtThree("gen-a", false)
	writer := p.begin()
	if _, err := writer.ExecContext(p.ctx,
		`UPDATE fact_work_items SET attempt_count = $2 WHERE work_item_id = $1 AND status = 'dead_letter'`,
		dead.id, committed); err != nil {
		t.Fatalf("concurrent attempt_count change: %v", err)
	}
	done := async(func() (int, error) { return sweepWith(p.ctx, pgstatus.SQLDB{DB: p.db}, 1) })
	p.waitForLockWaiters(1)
	p.commitTx(writer)
	if got := p.await("sweep", done); got != 1 {
		t.Fatalf("sweep moved %d rows, want 1", got)
	}
	p.requireSwept("gen-a", committed, 1)
}
