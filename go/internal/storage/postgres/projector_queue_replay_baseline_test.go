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
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/poison"
	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
)

// replayBaselineProjectionError is a retryable projection failure that is not
// an admission timeout, such as a transient Postgres or graph write error.
type replayBaselineProjectionError struct{}

func (replayBaselineProjectionError) Error() string   { return "transient projection failure" }
func (replayBaselineProjectionError) Retryable() bool { return true }

// TestRetryDecisionReplayBaselineDifferential pins the #11182 budget against
// the #10671 rule it extends. With baseline 0 every (attempt, waits) cell
// decides exactly as main did (attempt - waits < MaxAttempts), and with
// baseline b a cell decides as the baseline-0 cell b attempts earlier: the
// claims before the replay no longer count. The admission branch never reads
// the baseline.
func TestRetryDecisionReplayBaselineDifferential(t *testing.T) {
	t.Parallel()

	queue := NewProjectorQueue(&recordingExecQueryer{}, "projector-1", 30*time.Second)
	queue.MaxAttempts = 3
	queue.AdmissionMaxAttempts = 48
	work := func(attempts, waits, baseline int) projector.ScopeGenerationWork {
		return projector.ScopeGenerationWork{
			AttemptCount: attempts, AdmissionWaitAttempts: waits, ReplayAttemptBaseline: baseline,
		}
	}
	admission := productionWrapAdmissionError(&admissionTimeoutBudgetTestError{message: "admission wait timed out"})
	cells := 0
	for attempts := 1; attempts <= 60; attempts++ {
		for waits := 0; waits <= attempts; waits++ {
			got, admissionRetry := queue.retryDecision(work(attempts, waits, 0), replayBaselineProjectionError{})
			if want := attempts-waits < 3; got != want || admissionRetry {
				t.Fatalf("baseline 0, attempt %d, waits %d: retry = %v (admission %v), want %v as on main",
					attempts, waits, got, admissionRetry, want)
			}
			for baseline := 1; baseline <= attempts-waits; baseline++ {
				got, _ := queue.retryDecision(work(attempts, waits, baseline), replayBaselineProjectionError{})
				want, _ := queue.retryDecision(work(attempts-baseline, waits, 0), replayBaselineProjectionError{})
				if got != want {
					t.Fatalf("baseline %d, attempt %d, waits %d: retry = %v, want %v (the baseline-0 cell at attempt %d)",
						baseline, attempts, waits, got, want, attempts-baseline)
				}
				gotAdmission, _ := queue.retryDecision(work(attempts, waits, baseline), admission)
				wantAdmission, _ := queue.retryDecision(work(attempts, waits, 0), admission)
				if gotAdmission != wantAdmission {
					t.Fatalf("baseline %d changed the admission decision at attempt %d, waits %d", baseline, attempts, waits)
				}
				cells++
			}
		}
	}
	if cells == 0 {
		t.Fatal("differential covered no baseline cells")
	}
}

// TestProjectorQueueFailRetriesFirstFailureAfterReplay is the unit form of the
// #11182 regression: the #10972 canary row, dead-lettered at attempt 3 with no
// admission waits and then replayed, claims at attempt 4 with baseline 3. Its
// first non-admission failure must retry as a new row's does. Before the fix
// 4 - 0 < 3 was false and the row dead-lettered at once.
func TestProjectorQueueFailRetriesFirstFailureAfterReplay(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.Now = func() time.Time { return time.Date(2026, time.November, 22, 1, 0, 0, 0, time.UTC) }
	work := projector.ScopeGenerationWork{
		Scope:                 scope.IngestionScope{ScopeID: "scope-123"},
		Generation:            scope.ScopeGeneration{GenerationID: "generation-456"},
		AttemptCount:          4,
		ReplayAttemptBaseline: 3,
	}
	if err := queue.Fail(context.Background(), work, replayBaselineProjectionError{}); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if len(db.execs) != 1 || !strings.Contains(db.execs[0].query, "status = 'retrying'") {
		t.Fatalf("first failure after replay should retry, execs = %+v", db.execs)
	}
}

// TestProjectorQueueFailReplayBackoffMatchesNewRow proves the first
// non-admission failure after a replay backs off as a new row's first failure
// does (attempt input 1), not as attempt 4 of the claim fence would.
func TestProjectorQueueFailReplayBackoffMatchesNewRow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 1, 0, 0, 0, time.UTC)
	nextAttempt := func(work projector.ScopeGenerationWork) time.Time {
		t.Helper()
		db := &recordingExecQueryer{}
		queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
		queue.MaxAttempts = 10
		queue.JitterFraction = 0
		queue.Now = func() time.Time { return now }
		if err := queue.Fail(context.Background(), work, replayBaselineProjectionError{}); err != nil {
			t.Fatalf("Fail() error = %v, want nil", err)
		}
		if len(db.execs) != 1 || !strings.Contains(db.execs[0].query, "status = 'retrying'") {
			t.Fatalf("Fail() should retry, execs = %+v", db.execs)
		}
		at, ok := db.execs[0].args[4].(time.Time)
		if !ok {
			t.Fatalf("retry arg $5 = %T, want next_attempt_at time", db.execs[0].args[4])
		}
		return at
	}
	fresh := nextAttempt(projector.ScopeGenerationWork{AttemptCount: 1})
	replayed := nextAttempt(projector.ScopeGenerationWork{AttemptCount: 4, ReplayAttemptBaseline: 3})
	unreplayed := nextAttempt(projector.ScopeGenerationWork{AttemptCount: 4})
	if !replayed.Equal(fresh) {
		t.Fatalf("replayed first failure retries at %v, want the new-row delay %v", replayed.Sub(now), fresh.Sub(now))
	}
	if !unreplayed.After(fresh) {
		t.Fatalf("attempt 4 without a baseline retries at %v, want a longer delay than %v", unreplayed.Sub(now), fresh.Sub(now))
	}
}

// TestRecoveryReplayTemplatesCarryRetryAllowance is the lockstep guard for the
// two recovery replay SET lists (#11182). It reads the shipped templates, not
// copies: each must carry the shared queuestore fragment and keep
// attempt_count at GREATEST(attempt_count, 1), never lowered, because
// attempt_count is the claim fence. The admin store's SET list has the same
// guard in its own package.
func TestRecoveryReplayTemplatesCarryRetryAllowance(t *testing.T) {
	t.Parallel()

	for name, template := range map[string]string{
		"replayFailedWorkItemsTemplate":        replayFailedWorkItemsTemplate,
		"replayFailedWorkItemsBoundedTemplate": replayFailedWorkItemsBoundedTemplate,
	} {
		if !strings.Contains(template, queuestore.ReplayRetryAllowanceAssignments) {
			t.Fatalf("%s does not carry queuestore.ReplayRetryAllowanceAssignments:\n%s", name, template)
		}
		if !strings.Contains(template, "attempt_count = GREATEST(attempt_count, 1)") {
			t.Fatalf("%s must keep attempt_count = GREATEST(attempt_count, 1):\n%s", name, template)
		}
		if strings.Contains(template, "attempt_count = 0") {
			t.Fatalf("%s lowers attempt_count, the claim fence:\n%s", name, template)
		}
	}
	fragment := queuestore.ReplayRetryAllowanceAssignments
	for _, want := range []string{
		"replay_attempt_baseline = CASE",
		"WHEN stage = 'projector' THEN GREATEST(attempt_count, 1)",
		"ELSE replay_attempt_baseline",
		"admission_wait_attempts = 0",
	} {
		if !strings.Contains(fragment, want) {
			t.Fatalf("ReplayRetryAllowanceAssignments missing %q", want)
		}
	}
}

// poisonSweepAttemptRaise is the attempt_count assignment the poison-liveness
// sweep pairs with the allowance fragment (#11217). It raises a projector
// dead letter at attempt 0 (the operator skip of a pending row) to 1, so the
// baseline the fragment writes equals attempt_count and the first claim after
// the recovery counts 1. It never lowers attempt_count, the claim fence, and
// leaves a reducer row's attempt_count alone because the reducer budget does
// not read the baseline yet (#11184).
const poisonSweepAttemptRaise = `attempt_count = CASE
        WHEN target.stage = 'projector' THEN GREATEST(target.attempt_count, 1)
        ELSE target.attempt_count
    END`

// TestReplayAllowanceCarriersRaiseAttemptCount is the lockstep guard over
// every dead_letter-to-pending writer in this module tree that carries
// queuestore.ReplayRetryAllowanceAssignments: the two recovery replay
// templates and the poison-liveness sweep, the fourth carrier (#11217). It
// reads the shipped constants, not copies. Each carrier must hold the whole
// fragment (a baseline without the admission reset lets the projection
// difference go negative) and the attempt_count raise that matches it, and
// none may lower attempt_count. The admin store, the other carrier, has the
// same guard in its own package.
func TestReplayAllowanceCarriersRaiseAttemptCount(t *testing.T) {
	t.Parallel()

	carriers := []struct {
		name, statement, raise string
	}{
		{"replayFailedWorkItemsTemplate", replayFailedWorkItemsTemplate, "attempt_count = GREATEST(attempt_count, 1)"},
		{"replayFailedWorkItemsBoundedTemplate", replayFailedWorkItemsBoundedTemplate, "attempt_count = GREATEST(attempt_count, 1)"},
		{"poison.RecoverDeadLettersQuery", poison.RecoverDeadLettersQuery, poisonSweepAttemptRaise},
	}
	for _, carrier := range carriers {
		if !strings.Contains(carrier.statement, queuestore.ReplayRetryAllowanceAssignments) {
			t.Fatalf("%s does not carry queuestore.ReplayRetryAllowanceAssignments:\n%s", carrier.name, carrier.statement)
		}
		if !strings.Contains(carrier.statement, carrier.raise) {
			t.Fatalf("%s carries the allowance without its attempt_count raise %q:\n%s", carrier.name, carrier.raise, carrier.statement)
		}
		if strings.Contains(carrier.statement, "attempt_count = 0") {
			t.Fatalf("%s lowers attempt_count, the claim fence:\n%s", carrier.name, carrier.statement)
		}
	}
	// The sweep appends the fragment as its last assignment, after
	// reopened_at, so the literal that opens its SET list still names
	// reopened_at for the #9320 source guard.
	sweep := poison.RecoverDeadLettersQuery
	if !strings.Contains(sweep, "reopened_at = $1,\n    "+queuestore.ReplayRetryAllowanceAssignments+"\nWHERE") {
		t.Fatalf("poison sweep must append the allowance fragment right after reopened_at:\n%s", sweep)
	}
}

// TestReprojectResetsReplayBaseline guards decision 5 of the #11182 ruling on
// the reproject re-open: it lowers a terminal row's attempt_count to 0, so it
// lowers the replay baseline with it, or attempt_count -
// admission_wait_attempts - replay_attempt_baseline goes negative and the row
// gets more than MaxAttempts projection retries. The refinalize re-enqueue has
// the same guard in the rebuild/reset package.
func TestReprojectResetsReplayBaseline(t *testing.T) {
	t.Parallel()

	if !strings.Contains(reprojectReopenedCTE, `replay_attempt_baseline = CASE
            WHEN fact_work_items.status IN ('succeeded', 'failed', 'dead_letter') THEN 0
            ELSE fact_work_items.replay_attempt_baseline
        END`) {
		t.Fatalf("reproject re-open resets attempt_count but not replay_attempt_baseline:\n%s", reprojectReopenedCTE)
	}
}
