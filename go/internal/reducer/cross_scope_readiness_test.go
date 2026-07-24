// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
)

func TestCrossScopeProducerNotReadyError(t *testing.T) {
	t.Parallel()

	err := newCrossScopeProducerNotReadyError(
		DomainCICDRunCorrelation,
		"ci-scope",
		"gen-1",
		[]Domain{DomainContainerImageIdentity},
	)

	if !err.Retryable() {
		t.Fatal("cross-scope producer-not-ready must be retryable so the queue defers rather than dead-letters")
	}
	if err.FailureClass() != CrossScopeProducerNotReadyFailureClass {
		t.Fatalf("failure class = %q, want %q", err.FailureClass(), CrossScopeProducerNotReadyFailureClass)
	}
	msg := err.Error()
	for _, want := range []string{
		string(DomainCICDRunCorrelation),
		string(DomainContainerImageIdentity),
		"ci-scope",
		"gen-1",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q missing %q", msg, want)
		}
	}
}

// TestCrossScopeProducerNotReadyFailureClassIsStable guards the constant's
// value. It is a durable failure_class persisted on retrying fact_work_items and
// matched by the claim-SQL exempt predicate, so changing the string would strand
// in-flight rows under the old class. The actual enrollment in
// nonCountingReducerRetryFailureClasses (and the attempt_count freeze it
// produces) is asserted in the storage/postgres package, which owns that list —
// see TestCrossScopeProducerNotReadyIsNonCounting.
func TestCrossScopeProducerNotReadyFailureClassIsStable(t *testing.T) {
	t.Parallel()

	if got, want := CrossScopeProducerNotReadyFailureClass, "cross_scope_producer_not_ready"; got != want {
		t.Fatalf("cross-scope producer-not-ready failure class = %q, want stable %q", got, want)
	}
}
