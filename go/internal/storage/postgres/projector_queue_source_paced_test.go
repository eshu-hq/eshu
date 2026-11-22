// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
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
	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-456"},
	}
	cause := productionWrapAdmissionError(sourcePacedTestError{})

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
