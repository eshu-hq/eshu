// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"errors"
	"strings"
	"testing"
)

func TestGenerationNotYetActiveErrorDescribesFirstGeneration(t *testing.T) {
	err := GenerationNotYetActiveError{
		ScopeID: "scope-7916", GenerationID: "generation-7916",
	}
	if !errors.Is(err, ErrGenerationNotYetActive) || !err.Retryable() ||
		err.FailureClass() != GenerationActivationNotReadyFailureClass {
		t.Fatalf("first-generation error lost retry classification: %v", err)
	}
	if got := err.Error(); !strings.Contains(got, "no active generation") ||
		strings.Contains(got, "newer than the active generation") {
		t.Fatalf("first-generation error = %q, want pending with no active generation", got)
	}
}

func TestGenerationNotYetActiveErrorDescribesActivePredecessor(t *testing.T) {
	err := GenerationNotYetActiveError{
		ScopeID: "scope-7916", GenerationID: "generation-b",
		ActiveGenerationID: "generation-a",
	}
	if got := err.Error(); !strings.Contains(got, "active generation generation-a") {
		t.Fatalf("successor error = %q, want active predecessor", got)
	}
}
