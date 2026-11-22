// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11217 PostgreSQL 18 full-bootstrap proof that the poison-liveness auto-retry sweep gives a recovered projector dead letter a new row's retry allowance through the production ProjectorQueue Claim and Fail: projection and admission budgets, the poison_recovery_attempts loop bound, the attempt_count claim fence, the attempt-0 operator-skip raise and the first-failure backoff; isolated schema, wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestPoisonSweepGrantsProjectionRetriesLive is the #11217 regression. A
// projector row dead-lettered by using up its projection budget (attempt 3)
// is recovered by the sweep, claims at attempt 4 with baseline 3, and its
// first retryable non-admission failure retries as a new row's does. Before
// the fix the sweep kept the counters, 4 - 0 < 3 was false, and the row
// dead-lettered again at once, spending the recovery budget. The count after
// the recovery equals the count of a new row driven through the same helper
// in this test, not a constant.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestPoisonSweepGrantsProjectionRetriesLive(t *testing.T) {
	start := time.Now()
	p := openReplayProof(t)
	p.commit("gen-a")
	fresh := p.failUntilDeadLetter("gen-a", proofProjectionError{}, 10, false)
	dead := p.work("gen-a")
	if dead.attempts != fresh || dead.baseline != 0 {
		t.Fatalf("new-row dead letter = %+v after %d failures, want attempt %d, baseline 0", dead, fresh, fresh)
	}
	if got := p.sweep(1); got != 1 {
		t.Fatalf("sweep moved %d rows, want 1", got)
	}
	p.requireRecovered("gen-a", 1)

	work := p.failOnce("gen-a", proofProjectionError{}, false)
	if row := p.work("gen-a"); row.status != "retrying" {
		t.Fatalf("first projection failure after recovery: work = %+v, want retrying like a new row", row)
	}
	if work.AttemptCount != dead.attempts+1 || work.ReplayAttemptBaseline != dead.attempts || work.AdmissionWaitAttempts != 0 {
		t.Fatalf("recovered claim = attempt %d, baseline %d, waits %d; want %d, %d, 0",
			work.AttemptCount, work.ReplayAttemptBaseline, work.AdmissionWaitAttempts, dead.attempts+1, dead.attempts)
	}
	if n := p.failUntilDeadLetter("gen-a", proofProjectionError{}, 10, false); n+1 != fresh {
		t.Fatalf("recovered row dead-lettered after %d projection failures, want %d as a new row", n+1, fresh)
	}
	t.Logf("wall time %s", time.Since(start))
}

// TestPoisonSweepGrantsAdmissionWaitsLive proves the admission half. A row
// dead-lettered by its 48-wait admission budget is recovered with the
// admission counter reset, survives 47 more admission waits and dies on the
// 48th. Before the fix the sweep kept admission_wait_attempts at 47 and the
// first wait after the recovery dead-lettered the row.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestPoisonSweepGrantsAdmissionWaitsLive(t *testing.T) {
	const admissionBudget = 48 // defaultProjectorAdmissionMaxAttempts
	start := time.Now()
	p := openReplayProof(t)
	p.commit("gen-a")
	if n := p.failUntilDeadLetter("gen-a", proofAdmissionTimeoutError{}, admissionBudget+1, false); n != admissionBudget {
		t.Fatalf("new row dead-lettered after %d admission waits, want %d", n, admissionBudget)
	}
	dead := p.work("gen-a")
	if got := p.sweep(1); got != 1 {
		t.Fatalf("sweep moved %d rows, want 1", got)
	}
	p.requireRecovered("gen-a", 1)
	if n := p.failUntilDeadLetter("gen-a", proofAdmissionTimeoutError{}, admissionBudget+1, false); n != admissionBudget {
		t.Fatalf("recovered row dead-lettered after %d admission waits, want %d", n, admissionBudget)
	}
	p.requireSweptBaseline("gen-a", dead.attempts)
	t.Logf("wall time %s", time.Since(start))
}

// TestPoisonSweepBudgetBoundsRecoveriesLive proves poison_recovery_attempts
// is still the only loop bound (decision 4 of the #11217 ruling). With budget
// M = 2 the row is recovered twice, takes a new row's three projection
// failures after each recovery, and the third sweep moves nothing: the row
// stays dead_letter at attempt 9 with the counter at 2. attempt_count, the
// claim fence, rises on every claim and never decreases.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestPoisonSweepBudgetBoundsRecoveriesLive(t *testing.T) {
	const budget = 2
	start := time.Now()
	p := openReplayProof(t)
	p.commit("gen-a")
	last := 0
	failToDeadLetter := func(phase string) int {
		t.Helper()
		for n := 1; n <= 10; n++ {
			work := p.failOnce("gen-a", proofProjectionError{}, false)
			if work.AttemptCount <= last {
				t.Fatalf("%s: claim at attempt %d after attempt %d, attempt_count must rise", phase, work.AttemptCount, last)
			}
			last = work.AttemptCount
			if p.work("gen-a").status == "dead_letter" {
				return n
			}
		}
		t.Fatalf("%s: row did not dead-letter within 10 failures", phase)
		return 0
	}
	if n := failToDeadLetter("new row"); n != 3 {
		t.Fatalf("new row dead-lettered after %d failures, want 3", n)
	}
	for recovery := 1; recovery <= budget; recovery++ {
		if got := p.sweep(budget); got != 1 {
			t.Fatalf("sweep %d moved %d rows, want 1", recovery, got)
		}
		p.requireSwept("gen-a", last, recovery)
		if n := failToDeadLetter("after recovery"); n != 3 {
			t.Fatalf("after recovery %d the row dead-lettered after %d failures, want 3", recovery, n)
		}
	}
	if got := p.sweep(budget); got != 0 {
		t.Fatalf("sweep past the budget moved %d rows, want 0", got)
	}
	row := p.work("gen-a")
	if row.status != "dead_letter" || row.attempts != 9 || p.poisonRecoveries("gen-a") != budget {
		t.Fatalf("after the budget: work = %+v, recoveries %d; want dead_letter at attempt 9 with %d recoveries",
			row, p.poisonRecoveries("gen-a"), budget)
	}
	t.Logf("wall time %s", time.Since(start))
}

// TestPoisonSweepKeepsClaimFenceLive proves the allowance does not weaken the
// attempt_count fence on the sweep. The work value of the claim that
// dead-lettered the row (attempt 3) is held across the recovery and the
// reclaim (attempt 4). After each, its Ack, Fail and Heartbeat answer
// ErrProjectorClaimRejected and leave the row unchanged.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestPoisonSweepKeepsClaimFenceLive(t *testing.T) {
	p := openReplayProof(t)
	p.commit("gen-a")
	p.failOnce("gen-a", proofProjectionError{}, false)
	p.failOnce("gen-a", proofProjectionError{}, false)
	stale := p.failOnce("gen-a", proofProjectionError{}, false)
	dead := p.work("gen-a")
	if dead.status != "dead_letter" || stale.AttemptCount != 3 {
		t.Fatalf("dead letter = %+v from attempt %d, want dead_letter from attempt 3", dead, stale.AttemptCount)
	}
	requireRejected := func(phase string) {
		t.Helper()
		before := p.work("gen-a")
		if err := p.queue.Ack(p.ctx, stale, projectorruntime.Result{}); !errors.Is(err, pgstatus.ErrProjectorClaimRejected) {
			t.Fatalf("%s: stale Ack error = %v, want ErrProjectorClaimRejected", phase, err)
		}
		if err := p.queue.Fail(p.ctx, stale, proofProjectionError{}); !errors.Is(err, pgstatus.ErrProjectorClaimRejected) {
			t.Fatalf("%s: stale Fail error = %v, want ErrProjectorClaimRejected", phase, err)
		}
		if err := p.queue.Heartbeat(p.ctx, stale); !errors.Is(err, pgstatus.ErrProjectorClaimRejected) {
			t.Fatalf("%s: stale Heartbeat error = %v, want ErrProjectorClaimRejected", phase, err)
		}
		if row := p.work("gen-a"); row != before {
			t.Fatalf("%s: stale calls changed work from %+v to %+v", phase, before, row)
		}
	}
	if got := p.sweep(1); got != 1 {
		t.Fatalf("sweep moved %d rows, want 1", got)
	}
	if row := p.work("gen-a"); row.attempts < dead.attempts {
		t.Fatalf("sweep lowered attempt_count from %d to %d", dead.attempts, row.attempts)
	}
	requireRejected("after recovery")
	live := p.claim("gen-a")
	if live.AttemptCount != 4 || live.ReplayAttemptBaseline != 3 {
		t.Fatalf("reclaim = attempt %d, baseline %d; want 4, 3", live.AttemptCount, live.ReplayAttemptBaseline)
	}
	requireRejected("after reclaim")
	p.ack(live)
	if row := p.work("gen-a"); row.status != "succeeded" {
		t.Fatalf("live Ack left work = %+v, want succeeded", row)
	}
}

// TestPoisonSweepRaisesOperatorSkipAttemptLive covers the attempt-0 cell on
// the production path. The operator skip dead-letters a pending projector row
// that was never claimed (attempt 0). The sweep raises it to attempt 1 with
// baseline 1, so the first claim after the recovery counts 1 and the row gets
// exactly three counting claims. Without the raise the fragment alone would
// give baseline 1 at attempt 0: a first claim that counts 0, four claims, and
// a backoff input of 0.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestPoisonSweepRaisesOperatorSkipAttemptLive(t *testing.T) {
	p := openReplayProof(t)
	p.commit("gen-a")
	if _, err := p.admin.SkipRepositoryWorkItems(p.ctx, p.scope.ScopeID, "#11217 proof"); err != nil {
		t.Fatalf("operator skip: %v", err)
	}
	if row := p.work("gen-a"); row.status != "dead_letter" || row.attempts != 0 {
		t.Fatalf("skipped row = %+v, want dead_letter at attempt 0", row)
	}
	if got := p.sweep(1); got != 1 {
		t.Fatalf("sweep moved %d rows, want 1", got)
	}
	p.requireSwept("gen-a", 1, 1)
	if n := p.failUntilDeadLetter("gen-a", proofProjectionError{}, 10, false); n != 3 {
		t.Fatalf("recovered attempt-0 row dead-lettered after %d projection failures, want 3", n)
	}
	if row := p.work("gen-a"); row.attempts != 4 || row.baseline != 1 {
		t.Fatalf("second dead letter = %+v, want attempt 4 with baseline 1", row)
	}
}

// TestPoisonSweepBackoffMatchesNewRowLive proves the first projection failure
// after a recovery backs off with the attempt-1 delay, as a new row's first
// failure does, and that the delay grows with the attempt (so the equality is
// not a constant delay).
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestPoisonSweepBackoffMatchesNewRowLive(t *testing.T) {
	p := openReplayProof(t)
	failAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	p.queue.Now = func() time.Time { return failAt }
	p.queue.JitterFraction = 0
	p.queue.RetryDelay = 2 * time.Second
	p.queue.MaxRetryDelay = time.Hour
	p.commit("gen-a")
	delay := func(phase string) time.Duration {
		t.Helper()
		var next sql.NullTime
		var status string
		if err := p.db.QueryRowContext(p.ctx, `
SELECT status, next_attempt_at FROM fact_work_items
WHERE stage = 'projector' AND generation_id = 'gen-a'
  AND work_item_id = 'projector_' || scope_id || '_' || generation_id`).Scan(&status, &next); err != nil {
			t.Fatalf("%s: read next_attempt_at: %v", phase, err)
		}
		if status != "retrying" || !next.Valid {
			t.Fatalf("%s: work status %s, next_attempt_at %v; want retrying with a next attempt", phase, status, next)
		}
		return next.Time.Sub(failAt)
	}
	p.failOnce("gen-a", proofProjectionError{}, false)
	first := delay("new row, first failure")
	p.failOnce("gen-a", proofProjectionError{}, false)
	second := delay("new row, second failure")
	if second <= first {
		t.Fatalf("second-failure delay %s, want longer than the first %s", second, first)
	}
	p.failOnce("gen-a", proofProjectionError{}, false)
	if got := p.sweep(1); got != 1 {
		t.Fatalf("sweep moved %d rows, want 1", got)
	}
	p.failOnce("gen-a", proofProjectionError{}, false)
	if got := delay("first failure after recovery"); got != first {
		t.Fatalf("first failure after recovery backs off %s, want the new-row first delay %s", got, first)
	}
}
