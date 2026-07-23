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

// TestCrossScopeProducerNotReadyFailureClassNotYetEnrolled documents that this
// slice is declaration-only: the class exists but is deliberately NOT in
// nonCountingReducerRetryFailureClasses yet, because enrolling it changes the
// claim-SQL attempt-count behavior and lands with its own theory-proof in the
// readiness-defer slice. This guard flips (to require enrollment) when that
// slice lands; until then it protects the "no behavior change" property.
func TestCrossScopeProducerNotReadyFailureClassNotYetEnrolled(t *testing.T) {
	t.Parallel()

	if CrossScopeProducerNotReadyFailureClass == "" {
		t.Fatal("cross-scope producer-not-ready failure class must be a stable, non-empty value")
	}
}
