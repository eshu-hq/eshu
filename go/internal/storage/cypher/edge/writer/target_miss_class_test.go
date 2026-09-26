// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// failureClassOf reads the durable failure class the reducer queue would
// persist for err: queuestore.QueueFailureMetadata takes the first error in
// the chain that implements FailureClass().
func failureClassOf(err error) (string, bool) {
	var classed interface{ FailureClass() string }
	if errors.As(err, &classed) {
		return classed.FailureClass(), true
	}
	return "", false
}

// TestTargetMissingErrorCarriesNonCountingReadinessClass is the writer half of
// the #7268 regression. A deployable_unit_edges batch whose target Repository
// is absent is a timing state (another scope commits it later with no
// happens-before), so the guard's error must self-classify with the
// non-counting readiness class. Without a class the reducer queue counted each
// miss toward MaxAttempts and dead-lettered the deployable_unit_correlation
// intent, and a dead letter is never reopened. The error comes from the real
// WriteEdges path, not a stand-in.
func TestTargetMissingErrorCarriesNonCountingReadinessClass(t *testing.T) {
	t.Parallel()

	executor := &targetMissProbeExecutor{probeAllPresent: false}
	_, err := NewEdgeWriter(executor, 0).WriteEdges(
		context.Background(), reducer.DomainDeployableUnitEdges,
		[]reducer.SharedProjectionIntentRow{validDeployableUnitRow()},
		"reducer/deployable-unit-correlation",
	)
	if err == nil {
		t.Fatal("WriteEdges() with an absent target = nil error, want the target-missing deferral")
	}
	var missing *targetMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("WriteEdges() error = %#v, want a *targetMissingError", err)
	}
	class, ok := failureClassOf(err)
	if !ok || class != reducer.SharedEdgeTargetNotReadyFailureClass {
		t.Fatalf("failure class = %q (found=%v), want %q so the queue defers without spending the retry budget",
			class, ok, reducer.SharedEdgeTargetNotReadyFailureClass)
	}
	if !reducer.IsRetryable(err) {
		t.Fatalf("WriteEdges() error = %v, want retryable", err)
	}
	repoID, intentID := missing.SampleTarget()
	if repoID != "repo-app" || intentID != "i-du-1" {
		t.Fatalf("SampleTarget() = (%q, %q), want (repo-app, i-du-1) for the operator WARN", repoID, intentID)
	}
}

// TestTargetProbeErrorStaysCounting pins the fail-closed half: a probe that
// could not run is a backend fault, not a timing state, so its error carries
// no failure class and a persistent fault spends the normal retry budget and
// dead-letters loudly instead of deferring forever (#7268, same rule as the
// #6759 deployment-source probe).
func TestTargetProbeErrorStaysCounting(t *testing.T) {
	t.Parallel()

	executor := &targetMissProbeExecutor{probeErr: errors.New("probe timed out")}
	_, err := NewEdgeWriter(executor, 0).WriteEdges(
		context.Background(), reducer.DomainDeployableUnitEdges,
		[]reducer.SharedProjectionIntentRow{validDeployableUnitRow()},
		"reducer/deployable-unit-correlation",
	)
	if err == nil {
		t.Fatal("WriteEdges() with a failing probe = nil error, want the probe deferral")
	}
	var probeErr *targetProbeError
	if !errors.As(err, &probeErr) {
		t.Fatalf("WriteEdges() error = %#v, want a *targetProbeError", err)
	}
	if class, ok := failureClassOf(err); ok {
		t.Fatalf("probe error failure class = %q, want none: a probe fault must count toward the retry budget", class)
	}
	if !reducer.IsRetryable(err) {
		t.Fatalf("WriteEdges() error = %v, want retryable", err)
	}
	if executor.executeCalls != 0 {
		t.Fatalf("executor writes = %d, want 0 on an unverified batch", executor.executeCalls)
	}
}
