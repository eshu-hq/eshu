// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// The #8283 "verified success" contract (arbiter-8312-det): after the edge
// write and before the admission decisions, every admitted
// CORRELATES_DEPLOYABLE_UNIT edge is verified present in the graph. Only a
// fully verified run writes its admission decisions and records its reopen
// fingerprint; a write that MATCH-missed (an endpoint Repository node did not
// exist yet) must not count as success, or the fingerprint gate would never
// re-open the item.

// stubDeployableUnitEdgeVerifier answers the verification with a fixed error
// and records the rows it was asked about.
type stubDeployableUnitEdgeVerifier struct {
	err   error
	calls [][]SharedProjectionIntentRow
}

func (v *stubDeployableUnitEdgeVerifier) VerifyDeployableUnitEdges(
	_ context.Context, rows []SharedProjectionIntentRow, _ string,
) error {
	v.calls = append(v.calls, append([]SharedProjectionIntentRow(nil), rows...))
	return v.err
}

// classedVerifyError stands in for the edge writer's verification errors:
// a retryable error that self-classifies.
type classedVerifyError struct{ class string }

func (e classedVerifyError) Error() string        { return "verify: " + e.class }
func (e classedVerifyError) Retryable() bool      { return true }
func (e classedVerifyError) FailureClass() string { return e.class }

func verifyingDeployableUnitHandler(
	verifier DeployableUnitEdgeVerifier,
) (DeployableUnitCorrelationHandler, *recordingReopenFingerprints, *recordingAdmissionDecisionWriter, *recordingDeployableUnitEdgeWriter) {
	recorder := &recordingReopenFingerprints{}
	decisions := &recordingAdmissionDecisionWriter{}
	writer := &recordingDeployableUnitEdgeWriter{}
	return DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader: &fingerprintedResolvedLoader{
			resolved: edgeAPIDeploysFrom(), complete: true,
			fingerprint: reducercontract.ResolvedReadFingerprint{RepoIDs: []string{"repo-edge-api"}, Value: "v"},
		},
		EdgeWriter:                writer,
		AdmissionDecisionWriter:   decisions,
		EdgeVerifier:              verifier,
		ReopenFingerprintRecorder: recorder,
	}, recorder, decisions, writer
}

func TestDeployableUnitCorrelationRecordsOnlyAfterEdgesVerifiedPresent(t *testing.T) {
	t.Parallel()

	verifier := &stubDeployableUnitEdgeVerifier{}
	handler, recorder, decisions, writer := verifyingDeployableUnitHandler(verifier)

	got, err := handler.Handle(context.Background(), claimedDeployableUnitIntent(time.Now().UTC()))
	if err != nil || got.Status != ResultStatusSucceeded {
		t.Fatalf("Handle() = %+v, %v; want succeeded", got, err)
	}
	if len(writer.writeCalls) != 1 {
		t.Fatalf("edge write calls = %d, want 1 (the fixture must admit an edge)", len(writer.writeCalls))
	}
	if len(verifier.calls) != 1 || len(verifier.calls[0]) != len(writer.writeCalls[0].rows) {
		t.Fatalf("verify calls = %+v, want one call over the %d admitted row(s)", verifier.calls, len(writer.writeCalls[0].rows))
	}
	if len(recorder.records) != 1 {
		t.Fatalf("fingerprint records = %d, want 1 after every edge verified present", len(recorder.records))
	}
	if len(decisions.calls) != 1 {
		t.Fatalf("admission decision writes = %d, want 1 after every edge verified present", len(decisions.calls))
	}
}

func TestDeployableUnitCorrelationUnverifiedEdgeRecordsNothing(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		class     string
		wantClass string
	}{
		// An endpoint Repository node is absent: the existing non-counting
		// timing class, bounded by the #7268 elapsed bound.
		{"missing_target", SharedEdgeTargetNotReadyFailureClass, SharedEdgeTargetNotReadyFailureClass},
		// Both endpoints exist and the edge still is not there: a counting
		// class that dead-letters loudly.
		{"missing_edge", DeployableUnitEdgeWriteUnverifiedFailureClass, DeployableUnitEdgeWriteUnverifiedFailureClass},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verifier := &stubDeployableUnitEdgeVerifier{err: classedVerifyError{class: tc.class}}
			handler, recorder, decisions, _ := verifyingDeployableUnitHandler(verifier)

			_, err := handler.Handle(context.Background(), claimedDeployableUnitIntent(time.Now().UTC()))
			var classed interface{ FailureClass() string }
			if !errors.As(err, &classed) || classed.FailureClass() != tc.wantClass {
				t.Fatalf("Handle() error = %v, want failure class %q", err, tc.wantClass)
			}
			var retryable interface{ Retryable() bool }
			if !errors.As(err, &retryable) || !retryable.Retryable() {
				t.Fatalf("Handle() error = %v, want retryable", err)
			}
			if len(recorder.records) != 0 {
				t.Fatalf("fingerprint records = %+v, want none for an unverified edge", recorder.records)
			}
			if len(decisions.calls) != 0 {
				t.Fatalf("admission decision writes = %d, want none for an unverified edge", len(decisions.calls))
			}
		})
	}
}

// The new class must count toward the retry budget: it is not a timing
// state, so it must never join the non-counting readiness set.
func TestDeployableUnitEdgeWriteUnverifiedClassIsDistinct(t *testing.T) {
	t.Parallel()
	if DeployableUnitEdgeWriteUnverifiedFailureClass == SharedEdgeTargetNotReadyFailureClass {
		t.Fatal("unverified-write class must differ from the non-counting target-not-ready class")
	}
}

// Without a verifier the handler cannot prove its edges landed, so it still
// succeeds (the graph write is unchanged) but records no fingerprint: a
// missing row always re-opens on the next pass.
func TestDeployableUnitCorrelationRecordsNoFingerprintWithoutVerifier(t *testing.T) {
	t.Parallel()

	handler, recorder, _, _ := verifyingDeployableUnitHandler(nil)
	if _, err := handler.Handle(context.Background(), claimedDeployableUnitIntent(time.Now().UTC())); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(recorder.records) != 0 {
		t.Fatalf("fingerprint records = %+v, want none without edge verification", recorder.records)
	}
}

// A graph executor without probe capability cannot verify: the run succeeds
// but records no fingerprint, so the item keeps re-opening rather than being
// closed on an unproven write.
func TestDeployableUnitCorrelationVerificationUnavailableRecordsNoFingerprint(t *testing.T) {
	t.Parallel()

	verifier := &stubDeployableUnitEdgeVerifier{err: fmt.Errorf("wrapped: %w", ErrDeployableUnitEdgeVerificationUnavailable)}
	handler, recorder, decisions, _ := verifyingDeployableUnitHandler(verifier)
	got, err := handler.Handle(context.Background(), claimedDeployableUnitIntent(time.Now().UTC()))
	if err != nil || got.Status != ResultStatusSucceeded {
		t.Fatalf("Handle() = %+v, %v; want succeeded", got, err)
	}
	if len(recorder.records) != 0 {
		t.Fatalf("fingerprint records = %+v, want none when verification is unavailable", recorder.records)
	}
	if len(decisions.calls) != 1 {
		t.Fatalf("admission decision writes = %d, want 1 (the write itself is unchanged)", len(decisions.calls))
	}
}

// An unverified fail-open run must say so on the log: without a debug
// record, a fleet whose executor never verifies keeps its unconditional
// reopen with no attributable signal (#8283 P2).
func TestDeployableUnitCorrelationLogsUnverifiedFailOpenRun(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		verifier DeployableUnitEdgeVerifier
	}{
		{"no_verifier_wired", nil},
		{"verification_unavailable", &stubDeployableUnitEdgeVerifier{err: fmt.Errorf("wrapped: %w", ErrDeployableUnitEdgeVerificationUnavailable)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			handler, _, _, _ := verifyingDeployableUnitHandler(tc.verifier)
			handler.Logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			if _, err := handler.Handle(context.Background(), claimedDeployableUnitIntent(time.Now().UTC())); err != nil {
				t.Fatalf("Handle() error = %v, want succeeded fail-open", err)
			}
			if !strings.Contains(logs.String(), "unverified") {
				t.Fatalf("logs = %q, want an unverified-run debug record", logs.String())
			}
		})
	}
}
