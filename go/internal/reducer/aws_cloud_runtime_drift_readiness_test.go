// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
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
		EvidenceLoader:   loader,
		Writer:           writer,
		ReadinessChecker: readiness,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-defer",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       DomainAWSCloudRuntimeDrift,
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

// TestAWSCloudRuntimeDriftHandlerCommitsAfterAttemptBoundReached is the
// terminal-fallback regression: past awsCloudRuntimeDriftStatePendingMaxAttempts,
// Handle must commit its best-available classification even though the state
// scope is STILL pending, so a genuine orphan (no Terraform anywhere, ever)
// does not starve forever.
func TestAWSCloudRuntimeDriftHandlerCommitsAfterAttemptBoundReached(t *testing.T) {
	t.Parallel()

	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{awsCloudRuntimeDriftOrphanedEvidenceRow("arn:aws:lambda:us-east-1:123456789012:function:x")},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	readiness := &stubAWSCloudRuntimeDriftReadinessChecker{pending: true}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:   loader,
		Writer:           writer,
		ReadinessChecker: readiness,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-terminal",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
		AttemptCount: awsCloudRuntimeDriftStatePendingMaxAttempts,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil (bound reached, must commit)", err)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1 (past the bound, Handle must write its best-available verdict)", writer.calls)
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
		EvidenceLoader:   loader,
		Writer:           writer,
		ReadinessChecker: readiness,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-not-pending",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       DomainAWSCloudRuntimeDrift,
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
		EvidenceLoader: loader,
		Writer:         writer,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-no-gate",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       DomainAWSCloudRuntimeDrift,
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
// not defer one.
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
		EvidenceLoader:   loader,
		Writer:           writer,
		ReadinessChecker: readiness,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-unmanaged",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil (unmanaged_cloud_resource cannot be improved by a pending state scope)", err)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1", writer.calls)
	}
	if readiness.calls != 0 {
		t.Fatalf("readiness.calls = %d, want 0 (no orphaned candidate, so the readiness check should not even run)", readiness.calls)
	}
}

func reducerErrorIsRetryable(err error) bool {
	var retryable RetryableError
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
