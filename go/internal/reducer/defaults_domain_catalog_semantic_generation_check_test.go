// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
)

// TestImplementedDefaultDomainDefinitionsWiresSemanticEntityGenerationCheck is
// the #8611 wiring RED/GREEN pair: when DefaultHandlers carries a
// GenerationCheck, the built semantic handler must run its freshness fence on
// that exact check, so a superseded generation exits before the graph write
// instead of dead-lettering as a derivation bug.
func TestImplementedDefaultDomainDefinitionsWiresSemanticEntityGenerationCheck(t *testing.T) {
	t.Parallel()

	probed := false
	check := GenerationFreshnessCheck(
		func(_ context.Context, scopeID, generationID string) (bool, error) {
			probed = true
			if scopeID != "scope-1" || generationID != "generation-1" {
				t.Errorf("GenerationCheck called with scope %q generation %q, want scope-1/generation-1", scopeID, generationID)
			}
			return true, nil
		},
	)
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		GenerationCheck: check,
	})

	handler := semanticEntityMaterializationHandler(t, definitions)
	if handler.GenerationCheck == nil {
		t.Fatal("GenerationCheck = nil, want the DefaultHandlers GenerationCheck wired through")
	}
	current, err := handler.GenerationCheck(context.Background(), "scope-1", "generation-1")
	if err != nil {
		t.Fatalf("GenerationCheck() error = %v, want nil", err)
	}
	if !current || !probed {
		t.Fatalf("GenerationCheck() = (%v, %v) probed=%v, want the exact wired check to run", current, err, probed)
	}
}

// TestImplementedDefaultDomainDefinitionsLeavesSemanticEntityGenerationCheckNilWithoutRootCheck
// pins the nil-guard: without a root GenerationCheck the handler keeps its
// legacy run-to-completion behavior instead of carrying a non-nil fence that
// can never answer.
func TestImplementedDefaultDomainDefinitionsLeavesSemanticEntityGenerationCheckNilWithoutRootCheck(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{})

	handler := semanticEntityMaterializationHandler(t, definitions)
	if handler.GenerationCheck != nil {
		t.Fatalf("GenerationCheck != nil, want nil when DefaultHandlers carries no GenerationCheck")
	}
}
