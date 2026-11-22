// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11182 PostgreSQL 18 full-bootstrap proof that each replay writer (admin replay, recovery replay, bounded recovery drain) gives a dead-lettered projector row a new row's retry allowance without lowering attempt_count: three projection failures and 48 admission waits, each still ending in a dead letter, and a duplicate admin request grants nothing; isolated schema, wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"testing"
	"time"
)

// TestReplayGrantsProjectionRetriesLive is the #11182 regression, run on every
// replay writer. A row dead-lettered by the projection budget (attempt 3, no
// admission waits, the shape the #10972 QA canary row had) is replayed and
// claims at attempt 4 with baseline 3. Its first transient non-admission
// failure retries, as a new row's does; before the fix 4 - 0 < 3 was false
// and the row dead-lettered at once. The allowance stays bounded: the third
// projection failure after the replay dead-letters the row again. The first
// phase also covers the upgrade case: a row with the column at its default 0
// dead-letters after exactly three failures, as on main.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestReplayGrantsProjectionRetriesLive(t *testing.T) {
	for _, site := range replaySites {
		t.Run(string(site), func(t *testing.T) {
			start := time.Now()
			p := openReplayProof(t)
			p.commit("gen-a")
			dead := p.deadLetterAtThree("gen-a", false)
			p.replay(site, dead.id)
			p.requireReplayed("gen-a", 3)

			work := p.failOnce("gen-a", proofProjectionError{}, false)
			if work.AttemptCount != 4 || work.ReplayAttemptBaseline != 3 || work.AdmissionWaitAttempts != 0 {
				t.Fatalf("replayed claim = attempt %d, baseline %d, waits %d; want 4, 3, 0",
					work.AttemptCount, work.ReplayAttemptBaseline, work.AdmissionWaitAttempts)
			}
			if row := p.work("gen-a"); row.status != "retrying" {
				t.Fatalf("first projection failure after replay: work = %+v, want retrying like a new row", row)
			}
			if n := p.failUntilDeadLetter("gen-a", proofProjectionError{}, 10, false); n != 2 {
				t.Fatalf("replayed row dead-lettered after %d projection failures, want 3 in all", n+1)
			}
			if row := p.work("gen-a"); row.attempts != 6 || row.baseline != 3 {
				t.Fatalf("second dead letter = %+v, want attempt 6 with baseline 3", row)
			}
			t.Logf("wall time %s", time.Since(start))
		})
	}
}

// TestReplayGrantsAdmissionWaitsLive proves the admission side of the
// allowance on each of the three replay paths. A row dead-lettered by exhausting the
// 48-wait admission budget is replayed with its admission counter reset, so
// it survives 47 more admission waits and dies on the 48th. Before the fix
// the replay kept admission_wait_attempts at 47 and the first wait after the
// replay dead-lettered the row.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestReplayGrantsAdmissionWaitsLive(t *testing.T) {
	const admissionBudget = 48 // defaultProjectorAdmissionMaxAttempts
	for _, site := range replaySites {
		t.Run(string(site), func(t *testing.T) {
			start := time.Now()
			p := openReplayProof(t)
			p.commit("gen-a")
			if n := p.failUntilDeadLetter("gen-a", proofAdmissionTimeoutError{}, admissionBudget+1, false); n != admissionBudget {
				t.Fatalf("new row dead-lettered after %d admission waits, want %d", n, admissionBudget)
			}
			dead := p.work("gen-a")
			if dead.admissionWaits != admissionBudget-1 {
				t.Fatalf("admission dead letter = %+v, want %d recorded waits", dead, admissionBudget-1)
			}
			p.replay(site, dead.id)
			p.requireReplayed("gen-a", admissionBudget)
			if n := p.failUntilDeadLetter("gen-a", proofAdmissionTimeoutError{}, admissionBudget+1, false); n != admissionBudget {
				t.Fatalf("replayed row dead-lettered after %d admission waits, want %d", n, admissionBudget)
			}
			t.Logf("wall time %s", time.Since(start))
		})
	}
}

// TestAdminReplayDuplicateRequestGrantsNothingLive proves replay idempotency
// keeps the allowance bounded: the same admin request again is answered from
// the admin_replay_requests ledger and does not reset the row a second time.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestAdminReplayDuplicateRequestGrantsNothingLive(t *testing.T) {
	p := openReplayProof(t)
	p.commit("gen-a")
	dead := p.deadLetterAtThree("gen-a", false)
	const key = "11182-duplicate"
	if got := p.adminReplay(dead.id, key); got["replayed_count"] != float64(1) {
		t.Fatalf("replay body = %v, want one replayed item", got)
	}
	p.failOnce("gen-a", proofProjectionError{}, false)
	before := p.work("gen-a")
	if before.status != "retrying" || before.attempts != 4 {
		t.Fatalf("after one failure: work = %+v, want retrying at attempt 4", before)
	}
	if got := p.adminReplay(dead.id, key); got["duplicate"] != true || got["replayed_count"] != float64(1) {
		t.Fatalf("duplicate replay body = %v, want the recorded outcome with duplicate=true", got)
	}
	if after := p.work("gen-a"); after != before {
		t.Fatalf("after duplicate replay: work = %+v, want unchanged %+v", after, before)
	}
}
