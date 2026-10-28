// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// recordingOutcomeRecorder captures every claim-outcome seam call in order.
type recordingOutcomeRecorder struct {
	mu         sync.Mutex
	calls      []string
	outcomes   []ClaimOutcome
	committed  []ClaimOutcome
	outcomeErr error
	commitErr  error
}

func (r *recordingOutcomeRecorder) RecordClaimOutcome(_ context.Context, outcome ClaimOutcome) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "outcome")
	r.outcomes = append(r.outcomes, outcome)
	return r.outcomeErr
}

func (r *recordingOutcomeRecorder) RecordClaimCommitted(_ context.Context, outcome ClaimOutcome) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "committed")
	r.committed = append(r.committed, outcome)
	return r.commitErr
}

func mustNewSourceWithOutcomeRecorder(
	t testing.TB,
	runner Runner,
	statusRecorder StatusRecorder,
	outcomeRecorder ClaimOutcomeRecorder,
) *Source {
	t.Helper()
	source, err := NewSource(Config{
		Manifest:             testManifest(),
		CollectorInstanceID:  "scorecard-instance",
		ScopeKind:            "repository",
		ConfigHandle:         "cfg-scorecard",
		Config:               map[string]any{"fixture": "scorecard"},
		Runner:               runner,
		StatusRecorder:       statusRecorder,
		ClaimOutcomeRecorder: outcomeRecorder,
		Clock:                testObservedAt,
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	return source
}

// TestNextClaimedReportsOneClaimOutcomeOnEveryExit proves the #9420 seam
// fires exactly once per NextClaimed call on every exit path, with the
// decided outcome, and that only completed outcomes wait for the commit.
func TestNextClaimedReportsOneClaimOutcomeOnEveryExit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		item          func() workflow.WorkItem
		result        func(workflow.WorkItem) sdkcollector.Result
		runnerErr     error
		statusErr     error
		want          ClaimOutcome
		wantCompleted bool
	}{
		{
			name:          "complete",
			result:        func(item workflow.WorkItem) sdkcollector.Result { return completeResult(item, testSDKFact(item)) },
			want:          ClaimOutcome{Origin: ResultOriginSDK, State: sdkcollector.ResultComplete, FactCount: 1},
			wantCompleted: true,
		},
		{
			name:          "unchanged",
			result:        unchangedBenchResult,
			want:          ClaimOutcome{Origin: ResultOriginSDK, State: sdkcollector.ResultUnchanged},
			wantCompleted: true,
		},
		{
			name: "partial",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := baseResult(item, sdkcollector.ResultPartial)
				result.Facts = []sdkcollector.Fact{testSDKFact(item)}
				result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusWarning, Partial: true, WarningCount: 2, FactCount: 1}}
				return result
			},
			want:          ClaimOutcome{Origin: ResultOriginSDK, State: sdkcollector.ResultPartial, Partial: true, FactCount: 1, WarningCount: 2},
			wantCompleted: true,
		},
		{
			// A partial result is partial whatever its status rows say: SDK
			// validation accepts a warning-only status row with Partial unset.
			name: "partial result with a warning-only status row",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := baseResult(item, sdkcollector.ResultPartial)
				result.Facts = []sdkcollector.Fact{testSDKFact(item)}
				result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusWarning, WarningCount: 1, FactCount: 1}}
				return result
			},
			want:          ClaimOutcome{Origin: ResultOriginSDK, State: sdkcollector.ResultPartial, Partial: true, FactCount: 1, WarningCount: 1},
			wantCompleted: true,
		},
		{
			// A complete result may still carry a partial status row; the
			// claim is then partial, so the row cannot read fresh/succeeded.
			name: "complete result with a partial status row",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := completeResult(item, testSDKFact(item))
				result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusWarning, Partial: true, WarningCount: 1, FactCount: 1}}
				return result
			},
			want:          ClaimOutcome{Origin: ResultOriginSDK, State: sdkcollector.ResultComplete, Partial: true, FactCount: 1, WarningCount: 1},
			wantCompleted: true,
		},
		{
			name:   "sdk retryable",
			result: func(item workflow.WorkItem) sdkcollector.Result { return retryableResultWithDelays(item, 30, 120) },
			want: ClaimOutcome{
				Origin: ResultOriginSDK, State: sdkcollector.ResultRetryable,
				FailureClass: "rate_limited", RetryAfter: 120 * time.Second,
			},
		},
		{
			name: "sdk terminal",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := baseResult(item, sdkcollector.ResultTerminal)
				result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusFailure, FailureClass: "invalid_config"}}
				return result
			},
			want: ClaimOutcome{Origin: ResultOriginSDK, State: sdkcollector.ResultTerminal, FailureClass: "invalid_config", Terminal: true},
		},
		{
			name: "host invalid claim",
			item: func() workflow.WorkItem {
				item := testWorkItem()
				item.Status = workflow.WorkItemStatusPending
				return item
			},
			result: func(item workflow.WorkItem) sdkcollector.Result { return completeResult(item, testSDKFact(item)) },
			want:   ClaimOutcome{Origin: ResultOriginHost, State: sdkcollector.ResultTerminal, FailureClass: FailureClassInvalidClaim, Terminal: true},
		},
		{
			name:      "host launch failure",
			runnerErr: errors.New("exec: no such file"),
			result:    func(workflow.WorkItem) sdkcollector.Result { return sdkcollector.Result{} },
			want:      ClaimOutcome{Origin: ResultOriginHost, State: sdkcollector.ResultRetryable, FailureClass: FailureClassLaunchFailure},
		},
		{
			name: "host invalid result",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := completeResult(item, testSDKFact(item))
				result.Facts[0].Kind = "dev.example.undeclared"
				return result
			},
			want: ClaimOutcome{Origin: ResultOriginHost, State: sdkcollector.ResultTerminal, FailureClass: FailureClassInvalidResult, Terminal: true},
		},
		{
			name: "host identity mismatch",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := completeResult(item, testSDKFact(item))
				result.Claim.WorkItemID = "work-someone-else"
				return result
			},
			want: ClaimOutcome{Origin: ResultOriginHost, State: sdkcollector.ResultTerminal, FailureClass: FailureClassIdentityMismatch, Terminal: true},
		},
		{
			name:      "host status record failure",
			statusErr: errors.New("recorder down"),
			result:    func(item workflow.WorkItem) sdkcollector.Result { return completeResult(item, testSDKFact(item)) },
			want:      ClaimOutcome{Origin: ResultOriginHost, State: sdkcollector.ResultRetryable, FailureClass: FailureClassStatusRecord},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := testWorkItem()
			if tc.item != nil {
				item = tc.item()
			}
			outcomes := &recordingOutcomeRecorder{}
			runner := &recordingRunner{result: tc.result(item), err: tc.runnerErr}
			source := mustNewSourceWithOutcomeRecorder(t, runner, &recordingStatusRecorder{err: tc.statusErr}, outcomes)
			collected, _, _ := source.NextClaimed(context.Background(), item)
			_ = collectFacts(t, collected)

			if len(outcomes.outcomes) != 1 || len(outcomes.committed) != 0 {
				t.Fatalf("outcome calls = %d, committed calls = %d, want 1 and 0", len(outcomes.outcomes), len(outcomes.committed))
			}
			got := outcomes.outcomes[0]
			if got.Item.WorkItemID != item.WorkItemID || got.Item.ScopeID != item.ScopeID {
				t.Fatalf("outcome item = %+v, want the claimed work item", got.Item)
			}
			if !got.AttemptedAt.Equal(testObservedAt()) {
				t.Fatalf("AttemptedAt = %s, want the host clock %s", got.AttemptedAt, testObservedAt())
			}
			want := tc.want
			want.Item, want.AttemptedAt, want.ObservedAt = got.Item, got.AttemptedAt, got.ObservedAt
			if got != want {
				t.Fatalf("outcome = %+v, want %+v", got, want)
			}
			if got.Completed() != tc.wantCompleted {
				t.Fatalf("Completed() = %t, want %t", got.Completed(), tc.wantCompleted)
			}
			if pending := source.pendingOutcomes.len(); pending != btoi(tc.wantCompleted) {
				t.Fatalf("pending committed outcomes = %d, want %d", pending, btoi(tc.wantCompleted))
			}
		})
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}

// TestObserveClaimedGenerationCommittedWritesTheStashedOutcomeOnce proves the
// success row waits for the commit hook, carries the same attempt time as the
// attempt row, and is written at most once.
func TestObserveClaimedGenerationCommittedWritesTheStashedOutcomeOnce(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	outcomes := &recordingOutcomeRecorder{}
	source := mustNewSourceWithOutcomeRecorder(t, &recordingRunner{result: completeResult(item, testSDKFact(item))}, nil, outcomes)
	if _, _, err := source.NextClaimed(context.Background(), item); err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if err := source.ObserveClaimedGenerationCommitted(context.Background(), item); err != nil {
		t.Fatalf("ObserveClaimedGenerationCommitted() error = %v, want nil", err)
	}
	if err := source.ObserveClaimedGenerationCommitted(context.Background(), item); err != nil {
		t.Fatalf("second ObserveClaimedGenerationCommitted() error = %v, want nil", err)
	}
	if got := strings.Join(outcomes.calls, ","); got != "outcome,committed" {
		t.Fatalf("recorder calls = %q, want outcome,committed", got)
	}
	if outcomes.committed[0] != outcomes.outcomes[0] {
		t.Fatalf("committed outcome = %+v, want the stashed outcome %+v", outcomes.committed[0], outcomes.outcomes[0])
	}
	if source.pendingOutcomes.len() != 0 {
		t.Fatalf("pending committed outcomes = %d, want 0 after the commit", source.pendingOutcomes.len())
	}
}

// TestFailedRetryClearsTheStashedOutcome proves the stash is cleared on every
// exit: a completed attempt whose commit never fired, followed by a failed
// retry of the same work item, leaves nothing for a late commit hook to write.
func TestFailedRetryClearsTheStashedOutcome(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	outcomes := &recordingOutcomeRecorder{}
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	source := mustNewSourceWithOutcomeRecorder(t, runner, nil, outcomes)
	if _, _, err := source.NextClaimed(context.Background(), item); err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	runner.err = errors.New("exec: no such file")
	if _, _, err := source.NextClaimed(context.Background(), item); err == nil {
		t.Fatal("retry NextClaimed() error = nil, want launch failure")
	}
	if err := source.ObserveClaimedGenerationCommitted(context.Background(), item); err != nil {
		t.Fatalf("ObserveClaimedGenerationCommitted() error = %v, want nil", err)
	}
	if len(outcomes.committed) != 0 || source.pendingOutcomes.len() != 0 {
		t.Fatalf("committed = %d, pending = %d, want 0 and 0", len(outcomes.committed), source.pendingOutcomes.len())
	}
}

// TestAttemptRowWriteFailureFailsTheClaimRetryable matches the in-tree
// collector: when the attempt row cannot be written, the claim fails
// retryable and nothing waits for a commit.
func TestAttemptRowWriteFailureFailsTheClaimRetryable(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	outcomes := &recordingOutcomeRecorder{outcomeErr: errors.New("postgres unavailable")}
	results := &recordingResultRecorder{}
	source := mustNewSourceWithOutcomeRecorder(t, &recordingRunner{result: completeResult(item, testSDKFact(item))}, results, outcomes)
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if ok || collected.Facts != nil {
		t.Fatalf("NextClaimed() = (facts=%v, ok=%t), want no generation", collected.Facts != nil, ok)
	}
	assertFailure(t, err, FailureClassSourceStateRecord, false)
	if source.pendingOutcomes.len() != 0 {
		t.Fatalf("pending committed outcomes = %d, want 0", source.pendingOutcomes.len())
	}
	want := ResultRecord{CollectorKind: "repo", Origin: ResultOriginHost, State: sdkcollector.ResultRetryable, FailureClass: FailureClassSourceStateRecord}
	if len(results.results) != 1 || results.results[0] != want {
		t.Fatalf("result records = %+v, want exactly %+v", results.results, want)
	}
}

// TestFailureRowWriteErrorKeepsTheFailureClassification matches the in-tree
// collector: a failure-row write error joins the claim error but never turns
// a terminal failure into a retry loop, or the reverse.
func TestFailureRowWriteErrorKeepsTheFailureClassification(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	result := completeResult(item, testSDKFact(item))
	result.Facts[0].Kind = "dev.example.undeclared"
	outcomes := &recordingOutcomeRecorder{outcomeErr: errors.New("postgres unavailable")}
	source := mustNewSourceWithOutcomeRecorder(t, &recordingRunner{result: result}, nil, outcomes)
	_, _, err := source.NextClaimed(context.Background(), item)
	assertFailure(t, err, FailureClassInvalidResult, true)
	if !strings.Contains(err.Error(), "postgres unavailable") {
		t.Fatalf("error = %v, want the state write error joined", err)
	}
}

// TestPendingClaimOutcomesStayBounded proves a commit hook that never fires
// (for example a commit that fails terminal) cannot grow the stash without
// bound.
func TestPendingClaimOutcomesStayBounded(t *testing.T) {
	t.Parallel()

	pending := newPendingClaimOutcomes()
	base := testObservedAt()
	for i := range maxPendingClaimOutcomes + 50 {
		item := testWorkItem()
		item.WorkItemID = "work-" + time.Duration(i).String()
		pending.stash(ClaimOutcome{Item: item, AttemptedAt: base.Add(time.Duration(i) * time.Second)})
	}
	if got := pending.len(); got != maxPendingClaimOutcomes {
		t.Fatalf("pending = %d, want the cap %d", got, maxPendingClaimOutcomes)
	}
	oldest := testWorkItem()
	oldest.WorkItemID = "work-0s"
	if _, ok := pending.take(oldest); ok {
		t.Fatal("take(oldest) ok = true, want the oldest entry evicted first")
	}
}
