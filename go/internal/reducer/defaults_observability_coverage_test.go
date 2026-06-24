// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestImplementedDefaultDomainDefinitionsOmitsObservabilityCoverageWithoutAdapters(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{})
	for _, def := range definitions {
		if def.Domain == DomainObservabilityCoverageCorrelation {
			t.Fatalf("observability_coverage_correlation registered without adapters; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesObservabilityCoverageWhenAdaptersPresent(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingObservabilityCoverageCorrelationWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                             loader,
		ObservabilityCoverageCorrelationWriter: writer,
	})
	found := false
	for _, def := range definitions {
		if def.Domain == DomainObservabilityCoverageCorrelation {
			found = true
			handler, ok := def.Handler.(ObservabilityCoverageCorrelationHandler)
			if !ok {
				t.Fatalf("observability_coverage_correlation handler type = %T, want ObservabilityCoverageCorrelationHandler", def.Handler)
			}
			if handler.FactLoader != loader {
				t.Fatal("observability_coverage_correlation handler FactLoader was not wired")
			}
			if handler.Writer != writer {
				t.Fatal("observability_coverage_correlation handler Writer was not wired")
			}
		}
	}
	if !found {
		t.Fatal("observability_coverage_correlation not registered after wiring loader+writer")
	}
}
