// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// sourcePacedTestError mirrors the production reduceradmission
// reducerSourcePacedError contract: retryable, self-describing as
// source-paced, and deliberately NOT an AdmissionTimeoutError and without a
// FailureClass. The reduceradmission package cannot be imported from this
// package's tests without a cycle, so the contract is mirrored (the
// production type is pinned in reduceradmission tests).
type sourcePacedTestError struct{}

func (sourcePacedTestError) Error() string       { return "reducer source paced" }
func (sourcePacedTestError) Retryable() bool     { return true }
func (sourcePacedTestError) IsSourcePaced() bool { return true }

func sourcePacedTestQueue(db *recordingExecQueryer) ProjectorQueue {
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.MaxAttempts = 3
	queue.AdmissionMaxAttempts = 2
	queue.Now = func() time.Time {
		return time.Date(2026, time.November, 21, 9, 0, 0, 0, time.UTC)
	}
	return queue
}

// TestProjectorQueueFailSourcePacedNeverDeadLettersAndSpendsNoBudget proves
// #10882 fork 1 at the queue: a source-paced release retries even when
// every attempt counter is far past both budgets, increments only the
// source-paced counter, and leaves the admission counter alone so a later
// genuine admission timeout still has its full budget.
func TestProjectorQueueFailSourcePacedNeverDeadLettersAndSpendsNoBudget(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := sourcePacedTestQueue(db)
	work := projector.ScopeGenerationWork{
		Scope:                 scope.IngestionScope{ScopeID: "scope-123"},
		Generation:            scope.ScopeGeneration{GenerationID: "generation-456"},
		AttemptCount:          10_000,
		AdmissionWaitAttempts: 2,
		SourcePacedWaits:      9_998,
	}

	if err := queue.Fail(context.Background(), work, productionWrapAdmissionError(sourcePacedTestError{})); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got := len(db.execs); got != 1 {
		t.Fatalf("exec count = %d, want 1", got)
	}
	query := db.execs[0].query
	if strings.Contains(query, "status = 'dead_letter'") {
		t.Fatalf("source-paced release must never dead-letter, query:\n%s", query)
	}
	if !strings.Contains(query, "status = 'retrying'") {
		t.Fatalf("source-paced release should retry, query:\n%s", query)
	}
	if !strings.Contains(query, "source_paced_waits = fact_work_items.source_paced_waits + 1") {
		t.Fatalf("source-paced retry must increment source_paced_waits, query:\n%s", query)
	}
	if strings.Contains(query, "admission_wait_attempts = ") {
		t.Fatalf("source-paced retry must not touch the admission budget, query:\n%s", query)
	}
}

// TestProjectorQueueSourcePacedSlowerThanBudgetsLosesNothing is the AC4
// arithmetic proof: a source slowed for many more cycles than the
// admission budget and the projection budget combined produces zero dead
// letters, spends neither budget, and the item still has its FULL genuine
// budgets after the latch releases. It drives the real retryDecision over
// the counters the claim and retry SQL maintain (claim: attempt_count+1;
// paced retry: source_paced_waits+1).
func TestProjectorQueueSourcePacedSlowerThanBudgetsLosesNothing(t *testing.T) {
	t.Parallel()

	queue := sourcePacedTestQueue(&recordingExecQueryer{})
	queue.RetryDelay = 30 * time.Second
	queue.MaxRetryDelay = time.Hour
	queue.JitterFraction = 0.1
	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-456"},
	}
	// The pre-write release cause, with the release-age resume bound (#10882
	// R4-4): the delay never exceeds the bound plus jitter, however long the
	// engagement, and none of the 1,000 cycles dead-letters.
	const resumeBound = 15 * time.Minute
	cause := productionWrapAdmissionError(failure.SourcePacedRelease{Source: "git", RetryAfter: time.Second, ResumeBound: resumeBound})

	// A source slowed for 1,000 MaxWait cycles: with the default 48-wait
	// admission budget that is ~20x the budget the old path would have
	// dead-lettered on.
	for cycle := 0; cycle < 1000; cycle++ {
		work.AttemptCount++ // claim
		willRetry, kind := queue.retryDecision(work, cause)
		if !willRetry {
			t.Fatalf("cycle %d: slowed source dead-lettered (attempt_count=%d, paced=%d)", cycle, work.AttemptCount, work.SourcePacedWaits)
		}
		if kind != retryKindSourcePaced {
			t.Fatalf("cycle %d: retry kind = %v, want source-paced", cycle, kind)
		}
		if delay := queue.retryBackoff(kind, work.SourcePacedWaits, cause); delay >= resumeBound+3*time.Second {
			t.Fatalf("cycle %d: paced delay %s exceeds the resume bound %s plus jitter", cycle, delay, resumeBound)
		}
		work.SourcePacedWaits++
	}
	if work.AdmissionWaitAttempts != 0 {
		t.Fatalf("admission wait attempts = %d, want 0 (pacing must not spend the admission budget)", work.AdmissionWaitAttempts)
	}

	// Latch released: the item runs and then fails genuinely. It must have
	// the full projection budget (3 here), not a budget eaten by pacing.
	for claim := 1; claim < queue.MaxAttempts; claim++ {
		work.AttemptCount++
		willRetry, kind := queue.retryDecision(work, errProjectionBugRetryable{})
		if !willRetry || kind != retryKindProjection {
			t.Fatalf("genuine failure %d after release: willRetry=%v kind=%v, want projection retry (budget refunded)", claim, willRetry, kind)
		}
	}
	// And a genuine admission timeout still has the whole admission budget.
	work.AttemptCount++
	willRetry, kind := queue.retryDecision(work, productionWrapAdmissionError(&admissionTimeoutBudgetTestError{message: "x"}))
	if !willRetry || kind != retryKindAdmission {
		t.Fatalf("genuine admission timeout after release: willRetry=%v kind=%v, want admission retry (budget intact)", willRetry, kind)
	}
}

// errProjectionBugRetryable is a plain retryable projection failure.
type errProjectionBugRetryable struct{}

func (errProjectionBugRetryable) Error() string   { return "transient projection failure" }
func (errProjectionBugRetryable) Retryable() bool { return true }

// TestProjectorQueueFailSourcePacedReleaseHonorsRetryAfter proves #10882 F8's
// release is not claimable while the source stays slowed: the paced retry is
// scheduled no earlier than the grant delay the cause carries, even when it
// exceeds the exponential backoff cap, and still increments only
// source_paced_waits.
func TestProjectorQueueFailSourcePacedReleaseHonorsRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name        string
		retryAfter  time.Duration
		wantAtLeast time.Duration
	}{
		{"hint above the backoff cap", 10 * time.Minute, 10 * time.Minute},
		{"no hint", 0, 0},
	} {
		db := &recordingExecQueryer{}
		queue := sourcePacedTestQueue(db)
		queue.Now = func() time.Time { return now }
		queue.RetryDelay = time.Second
		queue.MaxRetryDelay = time.Minute
		work := projector.ScopeGenerationWork{
			Scope:        scope.IngestionScope{ScopeID: "scope-123"},
			Generation:   scope.ScopeGeneration{GenerationID: "generation-456"},
			AttemptCount: 3, SourcePacedWaits: 2,
		}
		cause := failure.SourcePacedRelease{Source: "git", RetryAfter: tc.retryAfter}
		if err := queue.Fail(context.Background(), work, cause); err != nil {
			t.Fatalf("%s: Fail() error = %v, want nil", tc.name, err)
		}
		if len(db.execs) != 1 {
			t.Fatalf("%s: exec count = %d, want 1", tc.name, len(db.execs))
		}
		query := db.execs[0].query
		if !strings.Contains(query, "source_paced_waits = fact_work_items.source_paced_waits + 1") ||
			strings.Contains(query, "status = 'dead_letter'") {
			t.Fatalf("%s: release must take the paced retry path, query:\n%s", tc.name, query)
		}
		visibleAt, ok := db.execs[0].args[4].(time.Time)
		if !ok {
			t.Fatalf("%s: visible_at arg = %T, want time.Time", tc.name, db.execs[0].args[4])
		}
		if got := visibleAt.Sub(now); got < tc.wantAtLeast {
			t.Fatalf("%s: visible_at is %s out, want at least %s", tc.name, got, tc.wantAtLeast)
		}
	}
}

// TestProjectorQueueSourcePacedDelayIsBoundedByResumeBound is the #10882 R4-4
// delay table. A paced release is scheduled at
// max(grant delay, min(paced exponential, resume bound)) plus jitter, so a
// long engagement never parks an item longer than the bound (the release age),
// where the unbounded exponential reached the 1 h retry cap after about 7
// cycles regardless of the grant delay. A cause with no bound keeps the queue
// cap, and a grant delay above the bound wins.
func TestProjectorQueueSourcePacedDelayIsBoundedByResumeBound(t *testing.T) {
	t.Parallel()

	const jitterCeiling = 3 * time.Second // JitterFraction 0.1 of the 30s base
	now := time.Date(2026, time.November, 22, 9, 0, 0, 0, time.UTC)
	fifteen := 15 * time.Minute
	for _, tc := range []struct {
		name       string
		waits      int
		retryAfter time.Duration
		bound      time.Duration
		wantMin    time.Duration
		wantMax    time.Duration // exclusive upper bound
	}{
		{"waits 0 under the bound", 0, 0, fifteen, 30 * time.Second, 30*time.Second + jitterCeiling},
		{"waits 3 under the bound", 3, 0, fifteen, 240 * time.Second, 240*time.Second + jitterCeiling},
		{"waits 7 hits the bound", 7, 0, fifteen, fifteen, fifteen + jitterCeiling},
		{"waits 50 stays at the bound", 50, 0, fifteen, fifteen, fifteen + jitterCeiling},
		{"waits 10000 stays at the bound", 10_000, 0, fifteen, fifteen, fifteen + jitterCeiling},
		{"no bound keeps the queue cap", 50, 0, 0, time.Hour, time.Hour + jitterCeiling},
		{"bound above the queue cap keeps the cap", 50, 0, 2 * time.Hour, time.Hour, time.Hour + jitterCeiling},
		{"grant delay above the bound wins", 50, 20 * time.Minute, fifteen, 20 * time.Minute, 20*time.Minute + jitterCeiling},
		{"grant delay under the delay is ignored", 3, time.Second, fifteen, 240 * time.Second, 240*time.Second + jitterCeiling},
	} {
		db := &recordingExecQueryer{}
		queue := sourcePacedTestQueue(db)
		queue.Now = func() time.Time { return now }
		queue.RetryDelay = 30 * time.Second
		queue.MaxRetryDelay = time.Hour
		queue.JitterFraction = 0.1
		work := projector.ScopeGenerationWork{
			Scope:            scope.IngestionScope{ScopeID: "scope-123"},
			Generation:       scope.ScopeGeneration{GenerationID: "generation-456"},
			AttemptCount:     tc.waits + 1,
			SourcePacedWaits: tc.waits,
		}
		cause := failure.SourcePacedRelease{Source: "git", RetryAfter: tc.retryAfter, ResumeBound: tc.bound}
		if err := queue.Fail(context.Background(), work, cause); err != nil {
			t.Fatalf("%s: Fail() error = %v, want nil", tc.name, err)
		}
		visibleAt, ok := db.execs[0].args[4].(time.Time)
		if !ok {
			t.Fatalf("%s: visible_at arg = %T, want time.Time", tc.name, db.execs[0].args[4])
		}
		got := visibleAt.Sub(now)
		if got < tc.wantMin || got >= tc.wantMax {
			t.Fatalf("%s: visible_at is %s out, want [%s, %s)", tc.name, got, tc.wantMin, tc.wantMax)
		}
	}
}
