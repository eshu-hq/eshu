package queue

import (
	"errors"
	"fmt"
	"time"
)

// WorkItemStatus captures the durable lifecycle state of a queue row.
type WorkItemStatus string

const (
	// StatusPending means the item is ready to be claimed.
	StatusPending WorkItemStatus = "pending"
	// StatusClaimed means the item has been leased and is awaiting execution.
	StatusClaimed WorkItemStatus = "claimed"
	// StatusRunning means the worker is actively processing the item.
	StatusRunning WorkItemStatus = "running"
	// StatusRetrying means the item has failed once and will reappear later.
	StatusRetrying WorkItemStatus = "retrying"
	// StatusSucceeded means the item finished successfully.
	StatusSucceeded WorkItemStatus = "succeeded"
	// StatusDeadLetter means the item exhausted retries or was quarantined.
	StatusDeadLetter WorkItemStatus = "dead_letter"
	// StatusFailed means the item is terminally failed.
	// Deprecated: retained only so legacy rows can still be replayed.
	StatusFailed WorkItemStatus = "failed"
)

// RetryState captures bounded retry timing for one work item.
type RetryState struct {
	AttemptCount  int
	LastAttemptAt time.Time
	NextAttemptAt time.Time
}

// FailureRecord captures the durable failure classification for one work item.
type FailureRecord struct {
	FailureClass string
	Message      string
	Details      string
}

// WorkItem is the durable Go representation of a fact-projection work item.
type WorkItem struct {
	WorkItemID    string
	ScopeID       string
	GenerationID  string
	Stage         string
	Domain        string
	AttemptCount  int
	Status        WorkItemStatus
	ClaimUntil    *time.Time
	VisibleAt     *time.Time
	RetryState    *RetryState
	FailureRecord *FailureRecord
}

// ScopeGenerationKey returns the durable scope-generation boundary for this work item.
func (w WorkItem) ScopeGenerationKey() string {
	return fmt.Sprintf("%s:%s", w.ScopeID, w.GenerationID)
}

// Claim moves a pending or retryable item into a claimed state.
func (w WorkItem) Claim(now time.Time, leaseFor time.Duration) (WorkItem, error) {
	if leaseFor <= 0 {
		return WorkItem{}, errors.New("lease duration must be positive")
	}
	if !w.canTransitionFromClaimable() {
		return WorkItem{}, fmt.Errorf("cannot claim work item in status %q", w.Status)
	}

	leaseUntil := now.Add(leaseFor)
	cloned := w.clone()
	cloned.AttemptCount++
	cloned.Status = StatusClaimed
	cloned.ClaimUntil = &leaseUntil
	cloned.VisibleAt = &leaseUntil
	cloned.RetryState = &RetryState{
		AttemptCount:  cloned.AttemptCount,
		LastAttemptAt: now,
		NextAttemptAt: leaseUntil,
	}

	return cloned, nil
}

// StartRunning moves a claimed item into a running state.
func (w WorkItem) StartRunning(now time.Time) (WorkItem, error) {
	if w.Status != StatusClaimed {
		return WorkItem{}, fmt.Errorf("cannot start work item in status %q", w.Status)
	}

	cloned := w.clone()
	cloned.Status = StatusRunning
	cloned.ClaimUntil = nil
	cloned.VisibleAt = nil
	if cloned.RetryState != nil {
		cloned.RetryState.LastAttemptAt = now
	}

	return cloned, nil
}

// Retry returns the item to a bounded retrying state.
func (w WorkItem) Retry(now time.Time, nextAttempt time.Time, failure FailureRecord) (WorkItem, error) {
	if w.Status != StatusRunning && w.Status != StatusClaimed {
		return WorkItem{}, fmt.Errorf("cannot retry work item in status %q", w.Status)
	}
	if nextAttempt.Before(now) {
		return WorkItem{}, errors.New("next attempt cannot be before now")
	}

	cloned := w.clone()
	cloned.Status = StatusRetrying
	cloned.FailureRecord = &failure
	cloned.ClaimUntil = nil
	cloned.VisibleAt = &nextAttempt
	retryCount := cloned.AttemptCount
	if retryCount == 0 {
		retryCount = 1
	}
	cloned.RetryState = &RetryState{
		AttemptCount:  retryCount,
		LastAttemptAt: now,
		NextAttemptAt: nextAttempt,
	}

	return cloned, nil
}

// Fail moves the item into a terminal dead-letter state.
func (w WorkItem) Fail(now time.Time) (WorkItem, error) {
	if w.Status != StatusRunning && w.Status != StatusClaimed && w.Status != StatusRetrying {
		return WorkItem{}, fmt.Errorf("cannot fail work item in status %q", w.Status)
	}

	cloned := w.clone()
	cloned.Status = StatusDeadLetter
	cloned.ClaimUntil = nil
	cloned.VisibleAt = nil
	if cloned.RetryState != nil {
		cloned.RetryState.LastAttemptAt = now
	}

	return cloned, nil
}

// Succeed moves the item into its terminal success state.
func (w WorkItem) Succeed() (WorkItem, error) {
	if w.Status != StatusRunning && w.Status != StatusClaimed {
		return WorkItem{}, fmt.Errorf("cannot succeed work item in status %q", w.Status)
	}

	cloned := w.clone()
	cloned.Status = StatusSucceeded
	cloned.ClaimUntil = nil
	cloned.VisibleAt = nil
	cloned.RetryState = nil
	cloned.FailureRecord = nil

	return cloned, nil
}

// Replay returns a terminal dead-lettered item to the pending state.
func (w WorkItem) Replay(now time.Time) (WorkItem, error) {
	if w.Status != StatusDeadLetter && w.Status != StatusFailed {
		return WorkItem{}, fmt.Errorf("cannot replay work item in status %q", w.Status)
	}

	cloned := w.clone()
	cloned.Status = StatusPending
	cloned.AttemptCount = 0
	cloned.ClaimUntil = nil
	cloned.VisibleAt = nil
	cloned.RetryState = nil
	cloned.FailureRecord = nil

	return cloned, nil
}

func (w WorkItem) canTransitionFromClaimable() bool {
	switch w.Status {
	case StatusPending, StatusRetrying:
		return true
	default:
		return false
	}
}

func (w WorkItem) clone() WorkItem {
	cloned := w
	if w.ClaimUntil != nil {
		claimUntil := *w.ClaimUntil
		cloned.ClaimUntil = &claimUntil
	}
	if w.VisibleAt != nil {
		visibleAt := *w.VisibleAt
		cloned.VisibleAt = &visibleAt
	}
	if w.RetryState != nil {
		retryState := *w.RetryState
		cloned.RetryState = &retryState
	}
	if w.FailureRecord != nil {
		failureRecord := *w.FailureRecord
		cloned.FailureRecord = &failureRecord
	}

	return cloned
}
