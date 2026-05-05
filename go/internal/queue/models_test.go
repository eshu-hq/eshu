package queue

import (
	"testing"
	"time"
)

func TestWorkItemScopeGenerationKey(t *testing.T) {
	t.Parallel()

	workItem := WorkItem{
		WorkItemID:   "work-item-1",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Stage:        "project",
		Domain:       "repository",
		Status:       StatusPending,
	}

	if got, want := workItem.ScopeGenerationKey(), "scope-123:generation-456"; got != want {
		t.Fatalf("WorkItem.ScopeGenerationKey() = %q, want %q", got, want)
	}
}

func TestWorkItemTransitionLifecycle(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.April, 12, 9, 0, 0, 0, time.UTC)
	leaseFor := 5 * time.Minute
	nextAttempt := start.Add(15 * time.Minute)

	workItem := WorkItem{
		WorkItemID:   "work-item-1",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Stage:        "project",
		Domain:       "repository",
		Status:       StatusPending,
	}

	claimed, err := workItem.Claim(start, leaseFor)
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if got, want := claimed.Status, StatusClaimed; got != want {
		t.Fatalf("Claim().Status = %q, want %q", got, want)
	}
	if got, want := claimed.AttemptCount, 1; got != want {
		t.Fatalf("Claim().AttemptCount = %d, want %d", got, want)
	}
	if claimed.RetryState == nil || claimed.RetryState.AttemptCount != 1 {
		t.Fatalf("Claim().RetryState = %#v, want attempt count 1", claimed.RetryState)
	}
	if claimed.ClaimUntil == nil || !claimed.ClaimUntil.Equal(start.Add(leaseFor)) {
		t.Fatalf("Claim().ClaimUntil = %#v, want %v", claimed.ClaimUntil, start.Add(leaseFor))
	}

	running, err := claimed.StartRunning(start)
	if err != nil {
		t.Fatalf("StartRunning() error = %v, want nil", err)
	}
	if got, want := running.Status, StatusRunning; got != want {
		t.Fatalf("StartRunning().Status = %q, want %q", got, want)
	}

	retrying, err := running.Retry(start, nextAttempt, FailureRecord{
		FailureClass: "timeout",
		Message:      "upstream timeout",
		Details:      "retry later",
	})
	if err != nil {
		t.Fatalf("Retry() error = %v, want nil", err)
	}
	if got, want := retrying.Status, StatusRetrying; got != want {
		t.Fatalf("Retry().Status = %q, want %q", got, want)
	}
	if retrying.VisibleAt == nil || !retrying.VisibleAt.Equal(nextAttempt) {
		t.Fatalf("Retry().VisibleAt = %#v, want %v", retrying.VisibleAt, nextAttempt)
	}
	if retrying.FailureRecord == nil || retrying.FailureRecord.FailureClass != "timeout" {
		t.Fatalf("Retry().FailureRecord = %#v, want timeout failure", retrying.FailureRecord)
	}

	deadLettered, err := retrying.Fail(start)
	if err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got, want := deadLettered.Status, StatusDeadLetter; got != want {
		t.Fatalf("Fail().Status = %q, want %q", got, want)
	}

	replayed, err := deadLettered.Replay(start)
	if err != nil {
		t.Fatalf("Replay() error = %v, want nil", err)
	}
	if got, want := replayed.Status, StatusPending; got != want {
		t.Fatalf("Replay().Status = %q, want %q", got, want)
	}
	if got, want := replayed.AttemptCount, 0; got != want {
		t.Fatalf("Replay().AttemptCount = %d, want %d", got, want)
	}
	if replayed.RetryState != nil {
		t.Fatalf("Replay().RetryState = %#v, want nil", replayed.RetryState)
	}
	if replayed.FailureRecord != nil {
		t.Fatalf("Replay().FailureRecord = %#v, want nil", replayed.FailureRecord)
	}
	if replayed.ClaimUntil != nil || replayed.VisibleAt != nil {
		t.Fatalf("Replay() left lease/visibility fields populated: claim_until=%#v visible_at=%#v", replayed.ClaimUntil, replayed.VisibleAt)
	}
}

func TestWorkItemReplayRejectsNonTerminalItems(t *testing.T) {
	t.Parallel()

	workItem := WorkItem{
		WorkItemID:   "work-item-1",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Stage:        "project",
		Domain:       "repository",
		Status:       StatusPending,
	}

	if _, err := workItem.Replay(time.Date(2026, time.April, 12, 9, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("Replay() error = nil, want non-nil")
	}
}
