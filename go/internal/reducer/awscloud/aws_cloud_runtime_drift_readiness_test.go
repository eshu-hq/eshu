// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

type stubAWSCloudRuntimeDriftReadinessChecker struct {
	pending bool
	err     error
	calls   int
}

func (s *stubAWSCloudRuntimeDriftReadinessChecker) HasPendingStateSnapshotEvidence(
	context.Context,
) (bool, error) {
	s.calls++
	return s.pending, s.err
}

// awsCloudRuntimeDriftOrphanedEvidenceRow is one AddressedRow for an ARN with
// no matching Terraform state -- Classify(cloud, nil, nil) yields
// orphaned_cloud_resource, the only finding kind a pending state scope could
// still improve.
func awsCloudRuntimeDriftOrphanedEvidenceRow(arn string) cloudruntime.AddressedRow {
	return cloudruntime.AddressedRow{
		ARN:          arn,
		ResourceType: "aws_lambda_function",
		Cloud: &cloudruntime.ResourceRow{
			ARN:          arn,
			ResourceType: "aws_lambda_function",
			ScopeID:      "aws:123456789012:us-east-1",
		},
	}
}

// TestAWSCloudRuntimeDriftHandlerDefersOrphanedFindingWhenStatePending is
// the bounded-defer regression: a pending Terraform state_snapshot scope must
// hold back an orphaned_cloud_resource classification instead of committing
// it as a durable verdict (#5848, #5837's root cause), and the writer must
// never be called at all while deferred.
func TestAWSCloudRuntimeDriftHandlerDefersOrphanedFindingWhenStatePending(t *testing.T) {
	t.Parallel()

	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{awsCloudRuntimeDriftOrphanedEvidenceRow("arn:aws:lambda:us-east-1:123456789012:function:x")},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	readiness := &stubAWSCloudRuntimeDriftReadinessChecker{pending: true}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1}},
		ReadinessChecker:   readiness,
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-defer",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       reducercontract.DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
		AttemptCount: 0,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want a deferred error")
	}
	if !reducerErrorIsRetryable(err) {
		t.Fatalf("Handle() error = %v, want Retryable() == true", err)
	}
	if class := reducerErrorFailureClass(t, err); class != AWSCloudRuntimeDriftStatePendingFailureClass {
		t.Fatalf("Handle() error FailureClass() = %q, want %q", class, AWSCloudRuntimeDriftStatePendingFailureClass)
	}
	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0 (a deferred pass must not write anything)", writer.calls)
	}
	if readiness.calls != 1 {
		t.Fatalf("readiness.calls = %d, want 1", readiness.calls)
	}
}

// TestAWSCloudRuntimeDriftHandlerCommitsAfterElapsedBoundReached is the
// terminal-fallback regression: once elapsed time since Intent.EnqueuedAt
// reaches awsCloudRuntimeDriftStatePendingMaxWait, Handle must commit its
// best-available classification even though the state scope is STILL
// pending, so a genuine orphan (no Terraform anywhere, ever) does not starve
// forever. Deliberately NOT an Intent.AttemptCount comparison: that was the
// original, wrong shape a hostile review caught -- AttemptCount is frozen by
// this defer's own non-counting failure class once the queue sees it, so a
// bound compared against it can never fire against the REAL queue (see
// TestAWSCloudRuntimeDriftHandlerConvergesAfterElapsedBoundOverRealQueueLive
// in go/internal/storage/postgres for the proof against the real
// ReducerQueue claim/fail cycle, which this Handler-level test cannot
// provide on its own).
func TestAWSCloudRuntimeDriftHandlerCommitsAfterElapsedBoundReached(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{awsCloudRuntimeDriftOrphanedEvidenceRow("arn:aws:lambda:us-east-1:123456789012:function:x")},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	readiness := &stubAWSCloudRuntimeDriftReadinessChecker{pending: true}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1}},
		ReadinessChecker:   readiness,
		Now:                func() time.Time { return now },
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-terminal",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       reducercontract.DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
		// Far past the bound. AttemptCount is deliberately left at its zero
		// value: the fix under test must not consult it at all.
		EnqueuedAt: now.Add(-2 * awsCloudRuntimeDriftStatePendingMaxWait),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil (elapsed bound reached, must commit)", err)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1 (past the bound, Handle must write its best-available verdict)", writer.calls)
	}
}

// TestAWSCloudRuntimeDriftHandlerKeepsDeferringBeforeElapsedBoundReached
// proves the bound does not fire early: an intent enqueued well within
// awsCloudRuntimeDriftStatePendingMaxWait must still defer while the state
// scope is pending, exactly like the zero-EnqueuedAt case
// TestAWSCloudRuntimeDriftHandlerDefersOrphanedFindingWhenStatePending
// already covers, but with a REAL non-zero elapsed duration under the bound
// rather than the "unknown enqueue time" fallback.
func TestAWSCloudRuntimeDriftHandlerKeepsDeferringBeforeElapsedBoundReached(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{awsCloudRuntimeDriftOrphanedEvidenceRow("arn:aws:lambda:us-east-1:123456789012:function:x")},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	readiness := &stubAWSCloudRuntimeDriftReadinessChecker{pending: true}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1}},
		ReadinessChecker:   readiness,
		Now:                func() time.Time { return now },
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-still-waiting",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       reducercontract.DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
		EnqueuedAt:   now.Add(-awsCloudRuntimeDriftStatePendingMaxWait / 2),
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want a deferred error (well within the elapsed bound)")
	}
	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0 (still within the elapsed bound, must not write)", writer.calls)
	}
}

// TestAWSCloudRuntimeDriftHandlerDoesNotDeferWhenStateNotPending proves the
// gate is readiness-specific: a false pending signal must never defer, even
// with an orphaned candidate and a fresh attempt count.
func TestAWSCloudRuntimeDriftHandlerDoesNotDeferWhenStateNotPending(t *testing.T) {
	t.Parallel()

	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{awsCloudRuntimeDriftOrphanedEvidenceRow("arn:aws:lambda:us-east-1:123456789012:function:x")},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	readiness := &stubAWSCloudRuntimeDriftReadinessChecker{pending: false}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1}},
		ReadinessChecker:   readiness,
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-not-pending",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       reducercontract.DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1", writer.calls)
	}
}

// TestAWSCloudRuntimeDriftHandlerNilReadinessCheckerNeverDefers proves the
// gate is opt-in: a nil ReadinessChecker must behave exactly like pre-#5848,
// writing every classification regardless of state readiness.
func TestAWSCloudRuntimeDriftHandlerNilReadinessCheckerNeverDefers(t *testing.T) {
	t.Parallel()

	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{awsCloudRuntimeDriftOrphanedEvidenceRow("arn:aws:lambda:us-east-1:123456789012:function:x")},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1}},
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-no-gate",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       reducercontract.DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1", writer.calls)
	}
}

// TestAWSCloudRuntimeDriftHandlerDoesNotDeferWithoutOrphanedCandidate proves a
// pending state scope cannot improve a non-orphaned finding kind, so it must
// not defer one. The readiness checker IS still called (#5875 P1 moved the
// call to before the evidence load, so Handle no longer knows in advance
// whether this pass will produce an orphaned candidate -- see
// checkAWSCloudRuntimeDriftReadinessBeforeLoad's doc comment); what this test
// actually proves is that the pending signal alone does not defer a pass
// whose freshly loaded evidence contains no orphaned candidate
// (shouldDeferForLoadedEvidence's AND, not OR).
func TestAWSCloudRuntimeDriftHandlerDoesNotDeferWithoutOrphanedCandidate(t *testing.T) {
	t.Parallel()

	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{
			{
				ARN:          "arn:aws:lambda:us-east-1:123456789012:function:unmanaged",
				ResourceType: "aws_lambda_function",
				Cloud: &cloudruntime.ResourceRow{
					ARN:          "arn:aws:lambda:us-east-1:123456789012:function:unmanaged",
					ResourceType: "aws_lambda_function",
					ScopeID:      "aws:123456789012:us-east-1",
				},
				State: &cloudruntime.ResourceRow{
					ARN:          "arn:aws:lambda:us-east-1:123456789012:function:unmanaged",
					Address:      "aws_lambda_function.unmanaged",
					ResourceType: "aws_lambda_function",
					ScopeID:      "state_snapshot:s3:hash",
				},
			},
		},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	readiness := &stubAWSCloudRuntimeDriftReadinessChecker{pending: true}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1}},
		ReadinessChecker:   readiness,
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-unmanaged",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       reducercontract.DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil (unmanaged_cloud_resource cannot be improved by a pending state scope)", err)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1", writer.calls)
	}
	if readiness.calls != 1 {
		t.Fatalf(
			"readiness.calls = %d, want 1 (the check now runs before the load, unconditionally; "+
				"it just must not, by itself, defer a pass whose loaded evidence has no orphaned candidate)",
			readiness.calls,
		)
	}
}

// TestAWSCloudRuntimeDriftCycleAnchorPrefersCycleStartedAt is the round-4
// review's required fast unit coverage for awsCloudRuntimeDriftCycleAnchor,
// which had zero direct coverage: a hostile review found the live
// reopen-anchor test alone is not "fast unit test" coverage for the pure
// selection function itself. Table-driven over both branches: CycleStartedAt
// set (must win, even over a different EnqueuedAt) and CycleStartedAt zero
// (must fall back to EnqueuedAt, the only shape a hand-built Intent from a
// caller that predates this field will ever produce).
func TestAWSCloudRuntimeDriftCycleAnchorPrefersCycleStartedAt(t *testing.T) {
	t.Parallel()

	enqueuedAt := time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)
	cycleStartedAt := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		intent reducercontract.Intent
		want   time.Time
	}{
		{
			name: "CycleStartedAt set wins over a different EnqueuedAt",
			intent: reducercontract.Intent{
				EnqueuedAt:     enqueuedAt,
				CycleStartedAt: cycleStartedAt,
			},
			want: cycleStartedAt,
		},
		{
			name: "CycleStartedAt zero falls back to EnqueuedAt",
			intent: reducercontract.Intent{
				EnqueuedAt: enqueuedAt,
			},
			want: enqueuedAt,
		},
		{
			name:   "both zero returns zero",
			intent: reducercontract.Intent{},
			want:   time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := awsCloudRuntimeDriftCycleAnchor(tt.intent); !got.Equal(tt.want) {
				t.Fatalf("awsCloudRuntimeDriftCycleAnchor() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestAWSCloudRuntimeDriftHandlerDefersOnFreshCycleStartedAtDespiteStaleEnqueuedAt
// is the Handle()-level companion to the anchor-selection unit test above: it
// proves CycleStartedAt actually GOVERNS the defer decision, not just that the
// pure selector function returns it. EnqueuedAt is set far enough in the past
// to be past the bound on its own (the round-3 regression shape); CycleStartedAt
// is recent, well within the bound. Handle must still defer -- if it read
// EnqueuedAt instead (or in addition), it would commit immediately, which is
// exactly the round-3 regression this branch's fix closes. This is a fast
// in-process companion to the live round-trip proof,
// TestAWSCloudRuntimeDriftReopenGetsFreshElapsedBoundWhileStatePendingLive
// (go/internal/storage/postgres), which additionally proves the real queue's
// SQL actually populates CycleStartedAt this way on a real reopen.
func TestAWSCloudRuntimeDriftHandlerDefersOnFreshCycleStartedAtDespiteStaleEnqueuedAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{awsCloudRuntimeDriftOrphanedEvidenceRow("arn:aws:lambda:us-east-1:123456789012:function:x")},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	readiness := &stubAWSCloudRuntimeDriftReadinessChecker{pending: true}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1}},
		ReadinessChecker:   readiness,
		Now:                func() time.Time { return now },
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-reopened",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       reducercontract.DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
		// Far past the bound on its own -- the exact shape a maintenance
		// reopen produces if the anchor reads EnqueuedAt instead of
		// CycleStartedAt.
		EnqueuedAt: now.Add(-2 * awsCloudRuntimeDriftStatePendingMaxWait),
		// The reopen reset this to "now": well within the bound.
		CycleStartedAt: now,
	})
	if err == nil {
		t.Fatal(
			"Handle() error = nil, want a deferred error: a fresh CycleStartedAt must govern the " +
				"defer decision even though EnqueuedAt alone is already past the bound",
		)
	}
	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0 (CycleStartedAt is within the bound, must not write)", writer.calls)
	}
}

func reducerErrorIsRetryable(err error) bool {
	var retryable reducercontract.RetryableError
	return errors.As(err, &retryable) && retryable.Retryable()
}

func reducerErrorFailureClass(t *testing.T, err error) string {
	t.Helper()
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("error %v does not self-classify a failure class", err)
	}
	return classified.FailureClass()
}
