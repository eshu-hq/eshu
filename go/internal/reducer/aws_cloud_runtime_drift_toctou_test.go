// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// transitioningAWSCloudRuntimeDriftEvidenceLoader deterministically forces the
// exact interleaving a hostile review (codex, PR #5875) found: a
// state_snapshot generation activating AFTER evidence is snapshotted but
// BEFORE the readiness check runs. Rather than racing goroutines against a
// clock (flaky, 1-in-N), onTransition is invoked synchronously the moment
// LoadAWSCloudRuntimeDriftEvidence is called -- forcing the transition INTO
// existence by construction, exactly as
// TestAWSCloudRuntimeDriftReadinessDeterministicReproductionLive
// (go/internal/storage/postgres) already does for the original #5837
// collector-order race, so this reproduces the bug 100% of runs, not 1-in-3.
type transitioningAWSCloudRuntimeDriftEvidenceLoader struct {
	rows         []cloudruntime.AddressedRow
	onTransition func()
	calls        int
}

func (l *transitioningAWSCloudRuntimeDriftEvidenceLoader) LoadAWSCloudRuntimeDriftEvidence(
	_ context.Context, _, _ string,
) ([]cloudruntime.AddressedRow, error) {
	l.calls++
	if l.onTransition != nil {
		l.onTransition()
	}
	return l.rows, nil
}

// TestAWSCloudRuntimeDriftHandlerDefersDespiteStateActivatingDuringEvidenceLoad
// is the P1 regression a round-5 hostile review (codex) found: Handle used to
// check readiness AFTER the evidence load, so a state_snapshot generation
// that activates in the window between the evidence snapshot and the
// readiness check made the checker report "ready" and Handle wrote the STALE
// orphaned_cloud_resource verdict computed from evidence that predated the
// activation -- the exact "written durably as a success, never repaired"
// shape #5837 is about, reintroduced through a narrower window. Worse, the
// repair path cannot save it: ReopenSucceeded's reopen selects only
// 'succeeded' rows, so a maintenance pass racing this exact intent while it
// is still claimed/running skips it, leaving no guaranteed later repair.
//
// The fix reorders Handle to check readiness BEFORE loading evidence and to
// capture that pending signal in a local variable used later, rather than
// re-querying the checker after the load (see aws_cloud_runtime_drift.go),
// so the signal can never reflect an activation that happened after evidence
// was captured. Evidence is still always loaded (needed for classification
// either way), and the actual defer decision combines the PRE-load pending
// signal with whether the freshly loaded evidence contains an orphaned
// candidate -- preserving the existing "only defer when it could actually
// matter" optimization instead of broadening every pass to defer whenever any
// state_snapshot scope anywhere is pending.
//
// The reverse race -- state activating BETWEEN the readiness check and the
// evidence load -- is benign by construction: the evidence load simply reads
// fresher data than the (now stale, "still pending") readiness signal
// assumed, which can only ever produce a MORE correct candidate set, never a
// worse one. If that fresher evidence resolves the match, Handle writes
// immediately with no wasted defer at all (see the sibling test,
// TestAWSCloudRuntimeDriftHandlerWritesFresherEvidenceWhenStateActivatesBeforeLoad).
// If the fresher evidence still shows an orphan for an unrelated reason,
// Handle defers unnecessarily -- the coarse checker's own documented contract
// (aws_cloud_runtime_drift_readiness.go's AWSCloudRuntimeDriftReadinessChecker
// doc comment) already accepts that as bounded and safe, never an incorrect
// verdict.
//
// This test forces the interleaving deterministically via onTransition, which
// flips the SAME ReadinessChecker stub's pending flag from true to false the
// moment LoadAWSCloudRuntimeDriftEvidence is called -- exactly simulating "the
// state_snapshot generation activates concurrently with the evidence load".
// Against the OLD (load-then-check) order this reproduces the bug: the load
// call flips pending to false as a side effect, then the check (running
// after) observes the already-flipped value and does not defer, so the STALE
// orphaned evidence the load already returned gets written. Against the FIXED
// (check-captured-before-load) order, the check runs and captures pending=true
// BEFORE the load's side effect fires, so the later flip has no bearing on the
// decision Handle already locked in.
func TestAWSCloudRuntimeDriftHandlerDefersDespiteStateActivatingDuringEvidenceLoad(t *testing.T) {
	t.Parallel()

	readiness := &stubAWSCloudRuntimeDriftReadinessChecker{pending: true}
	loader := &transitioningAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{
			awsCloudRuntimeDriftOrphanedEvidenceRow("arn:aws:lambda:us-east-1:123456789012:function:toctou"),
		},
		// The moment evidence is loaded, the state_snapshot generation
		// "activates" -- readiness would report ready to anyone checking
		// AFTER this point, but the evidence this loader just returned was
		// captured from the pre-activation world.
		onTransition: func() { readiness.pending = false },
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1}},
		ReadinessChecker:   readiness,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-toctou",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
	})
	if err == nil {
		t.Fatal(
			"Handle() error = nil, want a deferred error: the readiness check must observe the " +
				"PRE-transition pending state, not a state the evidence load's own timing could have " +
				"already raced past",
		)
	}
	if writer.calls != 0 {
		t.Fatalf(
			"writer.calls = %d, want 0: Handle must not durably write a verdict computed from evidence "+
				"that predates a state activation the readiness check could not see",
			writer.calls,
		)
	}
	if !reducerErrorIsRetryable(err) {
		t.Fatalf("Handle() error = %v, want Retryable() == true", err)
	}
	if class := reducerErrorFailureClass(t, err); class != AWSCloudRuntimeDriftStatePendingFailureClass {
		t.Fatalf("Handle() error FailureClass() = %q, want %q", class, AWSCloudRuntimeDriftStatePendingFailureClass)
	}
}

// TestAWSCloudRuntimeDriftHandlerWritesFresherEvidenceWhenStateActivatesBeforeLoad
// is the "reverse race" the fix's own doc comment argues is benign: state
// activates BETWEEN the (now-first) readiness check and the evidence load, so
// the readiness check observes a STALE pending=true signal (activation
// already happened by the time load actually runs), but the evidence load
// itself reads the FRESHER, already-matched state. The fix always loads
// evidence regardless of the pre-load pending signal -- the pending signal
// only combines with whether the FRESHLY LOADED evidence actually contains an
// orphaned_cloud_resource candidate (hasOrphanedAWSCloudRuntimeDriftCandidate)
// to decide deferral, preserving the existing "only defer when it could
// actually matter" optimization instead of broadening every pass to defer
// whenever any state_snapshot scope anywhere is pending. So here, even though
// the pre-load check said pending=true, the freshly loaded evidence already
// shows a resolved match -- no orphaned candidate -- so Handle writes the
// CORRECT verdict immediately rather than deferring unnecessarily. This is a
// strictly BETTER outcome than "safe but wasteful": the reverse race costs
// nothing at all when the fresher evidence resolves cleanly.
func TestAWSCloudRuntimeDriftHandlerWritesFresherEvidenceWhenStateActivatesBeforeLoad(t *testing.T) {
	t.Parallel()

	readiness := &stubAWSCloudRuntimeDriftReadinessChecker{pending: true}
	loader := &transitioningAWSCloudRuntimeDriftEvidenceLoader{
		// Fresher evidence than the stale "pending" signal assumed: state has
		// already activated and now matches, so Classify no longer admits an
		// orphaned_cloud_resource candidate for this ARN.
		rows: []cloudruntime.AddressedRow{
			{
				ARN:          "arn:aws:lambda:us-east-1:123456789012:function:reverse-race",
				ResourceType: "aws_lambda_function",
				Cloud: &cloudruntime.ResourceRow{
					ARN:          "arn:aws:lambda:us-east-1:123456789012:function:reverse-race",
					ResourceType: "aws_lambda_function",
					ScopeID:      "aws:123456789012:us-east-1",
				},
				State: &cloudruntime.ResourceRow{
					ARN:          "arn:aws:lambda:us-east-1:123456789012:function:reverse-race",
					Address:      "aws_lambda_function.reverse_race",
					ResourceType: "aws_lambda_function",
					ScopeID:      "state_snapshot:s3:hash",
				},
			},
		},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1}},
		ReadinessChecker:   readiness,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-reverse-race",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "gen-1",
		SourceSystem: "aws",
		Domain:       DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
	})
	if err != nil {
		t.Fatalf(
			"Handle() error = %v, want nil: the pre-load pending signal was stale (state had already "+
				"activated by load time), and the freshly loaded evidence shows no orphaned candidate, "+
				"so Handle must write the correct verdict rather than defer on a signal the load has "+
				"already superseded",
			err,
		)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1 (the reverse race must not cost an unnecessary defer)", writer.calls)
	}
	if loader.calls != 1 {
		t.Fatalf("loader.calls = %d, want 1 (evidence must always be loaded, independent of the pre-load pending signal)", loader.calls)
	}
}
