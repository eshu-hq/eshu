// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11182 PostgreSQL 18 full-bootstrap proof that the replay retry allowance keeps the attempt_count claim fence: a work value held from before a replay is rejected by Ack, Fail and Heartbeat after the replay and reclaim; concurrent replays of one row move it once; a replay never rewrites a claimed row; the replay event and work row record the baseline; refinalize resets it; isolated schema, wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"errors"
	"sync"
	"testing"
	"time"

	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestReplayKeepsClaimFenceLive proves the allowance does not weaken the
// attempt_count fence on any replay writer. The work value of the claim that
// dead-lettered the row (attempt 3) is held across the replay and the reclaim
// (attempt 4). Its Ack, Fail and Heartbeat all answer
// ErrProjectorClaimRejected and leave the live claim untouched; attempt_count
// never decreases across the replay.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestReplayKeepsClaimFenceLive(t *testing.T) {
	for _, site := range replaySites {
		t.Run(string(site), func(t *testing.T) {
			p := openReplayProof(t)
			p.commit("gen-a")
			p.failOnce("gen-a", proofProjectionError{}, false)
			p.failOnce("gen-a", proofProjectionError{}, false)
			stale := p.failOnce("gen-a", proofProjectionError{}, false)
			dead := p.work("gen-a")
			if dead.status != "dead_letter" || stale.AttemptCount != 3 {
				t.Fatalf("dead letter = %+v from attempt %d, want dead_letter from attempt 3", dead, stale.AttemptCount)
			}
			p.replay(site, dead.id)
			if row := p.work("gen-a"); row.attempts < dead.attempts {
				t.Fatalf("replay lowered attempt_count from %d to %d", dead.attempts, row.attempts)
			}
			live := p.claim("gen-a")
			claimed := p.work("gen-a")
			if live.AttemptCount != 4 || claimed.status != "claimed" {
				t.Fatalf("reclaim = attempt %d, row %+v; want attempt 4, claimed", live.AttemptCount, claimed)
			}

			if err := p.queue.Ack(p.ctx, stale, projectorruntime.Result{}); !errors.Is(err, pgstatus.ErrProjectorClaimRejected) {
				t.Fatalf("stale Ack error = %v, want ErrProjectorClaimRejected", err)
			}
			if err := p.queue.Fail(p.ctx, stale, proofProjectionError{}); !errors.Is(err, pgstatus.ErrProjectorClaimRejected) {
				t.Fatalf("stale Fail error = %v, want ErrProjectorClaimRejected", err)
			}
			if err := p.queue.Heartbeat(p.ctx, stale); !errors.Is(err, pgstatus.ErrProjectorClaimRejected) {
				t.Fatalf("stale Heartbeat error = %v, want ErrProjectorClaimRejected", err)
			}
			if row := p.work("gen-a"); row != claimed {
				t.Fatalf("after stale calls: work = %+v, want the live claim unchanged %+v", row, claimed)
			}
			p.ack(live)
			if row := p.work("gen-a"); row.status != "succeeded" {
				t.Fatalf("live Ack left work = %+v, want succeeded", row)
			}
		})
	}
}

// TestConcurrentReplaysMoveRowOnceLive races two replays of one dead-lettered
// row, for each pair of writers (two admin requests with different keys, and
// the unbounded recovery replay against the bounded drain). Exactly one moves
// the row, the baseline equals the prior attempt_count, and the admin pair
// writes one replay event.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestConcurrentReplaysMoveRowOnceLive(t *testing.T) {
	pairs := [][2]replaySite{
		{replayViaAdmin, replayViaAdmin},
		{replayViaRecovery, replayViaRecoveryBounded},
	}
	const rounds = 8
	for _, pair := range pairs {
		t.Run(string(pair[0])+"+"+string(pair[1]), func(t *testing.T) {
			p := openReplayProof(t)
			p.commit("gen-a")
			dead := p.deadLetterAtThree("gen-a", false)
			for round := range rounds {
				var wg sync.WaitGroup
				moved := make([]int, 2)
				startGate := make(chan struct{})
				for i, site := range pair {
					wg.Add(1)
					go func() {
						defer wg.Done()
						<-startGate
						moved[i] = p.replayCount(site, dead.id)
					}()
				}
				close(startGate)
				wg.Wait()
				if moved[0]+moved[1] != 1 {
					t.Fatalf("round %d: replays moved %d and %d rows, want exactly one in all", round, moved[0], moved[1])
				}
				p.requireReplayed("gen-a", 3)
				// Put the row back to the same dead letter for the next round.
				if _, err := p.db.ExecContext(p.ctx, `UPDATE fact_work_items
SET status = 'dead_letter', failure_class = 'retry_exhausted', replay_attempt_baseline = 0
WHERE work_item_id = $1`, dead.id); err != nil {
					t.Fatalf("round %d: reset dead letter: %v", round, err)
				}
			}
			if pair[0] == replayViaAdmin {
				events, err := p.admin.ListReplayEvents(p.ctx, admin.ReplayEventFilter{WorkItemID: dead.id, Limit: 100})
				if err != nil || len(events) != rounds {
					t.Fatalf("replay events = %d (%v), want one per round (%d)", len(events), err, rounds)
				}
			}
		})
	}
}

// TestReplayNeverRewritesClaimedRowLive proves a replay that arrives after a
// worker claimed the replayed row selects nothing and leaves the claim alone,
// on each of the three replay paths.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestReplayNeverRewritesClaimedRowLive(t *testing.T) {
	for _, site := range replaySites {
		t.Run(string(site), func(t *testing.T) {
			p := openReplayProof(t)
			p.commit("gen-a")
			dead := p.deadLetterAtThree("gen-a", false)
			p.replay(site, dead.id)
			p.claim("gen-a")
			claimed := p.work("gen-a")
			for _, again := range replaySites {
				if got := p.replayCount(again, dead.id); got != 0 {
					t.Fatalf("%s replay of a claimed row moved %d rows, want 0", again, got)
				}
			}
			if row := p.work("gen-a"); row != claimed {
				t.Fatalf("after replays: work = %+v, want the claim unchanged %+v", row, claimed)
			}
		})
	}
}

// TestAdminReplayRecordsBaselineLive proves the amended audit criterion: the
// work row and the replay event record the baseline. The admin replay returns
// replay_attempt_baseline on the item, the work-item query reads it back, and
// the replay event carries it with the admission waits the replay reset. An
// event written before migration 334 (no new columns) reads both as nil.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestAdminReplayRecordsBaselineLive(t *testing.T) {
	p := openReplayProof(t)
	p.commit("gen-a")
	p.failOnce("gen-a", proofAdmissionTimeoutError{}, false)
	p.failOnce("gen-a", proofAdmissionTimeoutError{}, false)
	dead := p.deadLetterAfterAdmissionWaits(t)

	items, err := p.admin.ReplayFailedWorkItems(p.ctx, admin.ReplayWorkItemFilter{WorkItemIDs: []string{dead.id}, Limit: 1})
	if err != nil || len(items) != 1 || items[0].ReplayAttemptBaseline != dead.attempts {
		t.Fatalf("admin replay = (%+v, %v), want one item with replay_attempt_baseline %d", items, err, dead.attempts)
	}
	listed, err := p.admin.ListWorkItems(p.ctx, admin.WorkItemFilter{ScopeID: p.scope.ScopeID, Stage: "projector", Limit: 10})
	if err != nil || len(listed) != 1 || listed[0].ReplayAttemptBaseline != dead.attempts {
		t.Fatalf("work-item query = (%+v, %v), want replay_attempt_baseline %d", listed, err, dead.attempts)
	}
	if _, err := p.db.ExecContext(p.ctx, `INSERT INTO fact_replay_events
    (replay_event_id, work_item_id, scope_id, generation_id, failure_class, operator_note, created_at)
VALUES ('replay-pre-334', $1, $2, 'gen-a', 'retry_exhausted', NULL, now() - interval '1 hour')`,
		dead.id, p.scope.ScopeID); err != nil {
		t.Fatalf("insert pre-migration event: %v", err)
	}
	events, err := p.admin.ListReplayEvents(p.ctx, admin.ReplayEventFilter{WorkItemID: dead.id, Limit: 10})
	if err != nil || len(events) != 2 {
		t.Fatalf("replay events = (%+v, %v), want the replay event and the pre-migration one", events, err)
	}
	latest, legacy := events[0], events[1]
	if latest.ReplayAttemptBaseline == nil || *latest.ReplayAttemptBaseline != int64(dead.attempts) {
		t.Fatalf("event replay_attempt_baseline = %v, want %d", latest.ReplayAttemptBaseline, dead.attempts)
	}
	if latest.PriorAdmissionWaitAttempts == nil || *latest.PriorAdmissionWaitAttempts != 2 {
		t.Fatalf("event prior_admission_wait_attempts = %v, want 2", latest.PriorAdmissionWaitAttempts)
	}
	if legacy.ReplayAttemptBaseline != nil || legacy.PriorAdmissionWaitAttempts != nil {
		t.Fatalf("pre-migration event = %+v, want nil baseline and prior waits", legacy)
	}
}

// deadLetterAfterAdmissionWaits finishes a row that already spent admission waits: it
// adds three projection failures, so the dead letter has attempt 5 with two
// admission waits.
func (p *replayProof) deadLetterAfterAdmissionWaits(t *testing.T) budgetRow {
	t.Helper()
	if n := p.failUntilDeadLetter("gen-a", proofProjectionError{}, 10, false); n != 3 {
		t.Fatalf("row dead-lettered after %d projection failures, want 3", n)
	}
	row := p.work("gen-a")
	if row.attempts != 5 || row.admissionWaits != 2 {
		t.Fatalf("dead letter = %+v, want attempt 5 with 2 admission waits", row)
	}
	return row
}

// TestRefinalizeResetsReplayBaselineLive proves decision 5 of the #11182
// ruling on the writer that lowers attempt_count through ON CONFLICT: a
// replayed refinalize row that refinalize re-enqueues again starts at
// attempt 0 with baseline 0 and gets exactly three projection retries, not
// more.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestRefinalizeResetsReplayBaselineLive(t *testing.T) {
	p := openReplayProof(t)
	p.commit("gen-a")
	p.ack(p.claim("gen-a"))
	refinalize := func() {
		t.Helper()
		if _, err := p.recovery.RefinalizeScopeProjections(p.ctx,
			recovery.RefinalizeFilter{ScopeIDs: []string{p.scope.ScopeID}}, time.Now().UTC()); err != nil {
			t.Fatalf("refinalize: %v", err)
		}
	}
	refinalize()
	rebuildID := "refinalize_" + p.scope.ScopeID + "_gen-a"
	readRebuild := func() budgetRow {
		t.Helper()
		var row budgetRow
		if err := p.db.QueryRowContext(p.ctx, `SELECT work_item_id, status, attempt_count, admission_wait_attempts, replay_attempt_baseline, reopened_at IS NOT NULL
FROM fact_work_items WHERE work_item_id = $1`, rebuildID).Scan(
			&row.id, &row.status, &row.attempts, &row.admissionWaits, &row.baseline, &row.reopened); err != nil {
			t.Fatalf("read rebuild row: %v", err)
		}
		return row
	}
	failRebuild := func() budgetRow {
		t.Helper()
		p.makeVisible("gen-a")
		work := p.claim("gen-a")
		if err := p.queue.Fail(p.ctx, work, proofProjectionError{}); err != nil {
			t.Fatalf("fail rebuild row: %v", err)
		}
		return readRebuild()
	}
	// Seed the rebuild row's dead letter directly: a refinalize row that
	// dead-letters through Fail also fails its generation (a9-130.1), and a
	// failed generation is not re-enqueued by the second refinalize below.
	if _, err := p.db.ExecContext(p.ctx, `UPDATE fact_work_items
SET status = 'dead_letter', attempt_count = 3, failure_class = 'retry_exhausted', lease_owner = NULL, claim_until = NULL
WHERE work_item_id = $1`, rebuildID); err != nil {
		t.Fatalf("seed rebuild dead letter: %v", err)
	}
	p.replay(replayViaRecovery, rebuildID)
	if row := readRebuild(); row.baseline != 3 {
		t.Fatalf("replayed rebuild row = %+v, want baseline 3", row)
	}
	failRebuild()
	refinalize()
	if row := readRebuild(); row.status != "pending" || row.attempts != 0 || row.baseline != 0 || row.admissionWaits != 0 {
		t.Fatalf("re-refinalized row = %+v, want pending with attempt, baseline and waits all 0", row)
	}
	for n := 1; n <= 3; n++ {
		row := failRebuild()
		if want := map[bool]string{true: "dead_letter", false: "retrying"}[n == 3]; row.status != want {
			t.Fatalf("failure %d after refinalize: row = %+v, want %s", n, row, want)
		}
	}
}
