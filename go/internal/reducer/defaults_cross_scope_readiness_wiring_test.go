// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
)

// TestCICDRunCorrelationRegistrationCarriesTheReadinessSeam guards the wiring,
// not the logic.
//
// The readiness floor is only worth anything if the registered handler actually
// carries the seam. A floor that is correct in isolation and unwired in
// DefaultHandlers ships inert: every unit test passes and production behaviour
// is unchanged. This test fails if the field is dropped from the registration.
func TestCICDRunCorrelationRegistrationCarriesTheReadinessSeam(t *testing.T) {
	t.Parallel()

	readiness := &fixedCrossScopeReadiness{ready: true}
	definitions := appendCorrelationCoreAdditiveDomains(nil, DefaultHandlers{
		FactLoader:                  &stubCICDRunCorrelationFactLoader{},
		CICDRunCorrelationWriter:    &recordingCICDRunCorrelationWriter{},
		CrossScopeProducerReadiness: readiness,
	})

	var handler CICDRunCorrelationHandler
	found := false
	for _, definition := range definitions {
		if definition.Domain != DomainCICDRunCorrelation {
			continue
		}
		typed, ok := definition.Handler.(CICDRunCorrelationHandler)
		if !ok {
			t.Fatalf("handler for %s = %T, want CICDRunCorrelationHandler", definition.Domain, definition.Handler)
		}
		handler, found = typed, true
	}
	if !found {
		t.Fatalf("no %s registration found", DomainCICDRunCorrelation)
	}
	if handler.ProducerReadiness == nil {
		t.Fatal("ProducerReadiness is nil on the registered handler: the readiness floor would ship inert")
	}

	// Prove it is the seam we passed, not some other non-nil value, by
	// observing the call.
	if _, err := handler.ProducerReadiness.CrossScopeProducersReady(
		context.Background(), DomainCICDRunCorrelation, "scope:ci", "gen:ci",
	); err != nil {
		t.Fatalf("CrossScopeProducersReady() error = %v", err)
	}
	if readiness.calls != 1 {
		t.Fatalf("readiness lookup calls = %d, want 1: the registration wired a different seam", readiness.calls)
	}
}
