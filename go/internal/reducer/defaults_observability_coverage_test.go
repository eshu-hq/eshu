// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
)

// recordingObservabilityCoverageCorrelationWriter is a minimal
// [ObservabilityCoverageCorrelationWriter] double for wiring tests: it only
// needs to satisfy the interface for a pointer-identity check, never called
// here. Package-local copy of the obscoverage test package's fuller recorder
// of the same name (issue #6061) -- that one is unexported and cannot cross
// the package boundary.
type recordingObservabilityCoverageCorrelationWriter struct{}

func (*recordingObservabilityCoverageCorrelationWriter) WriteObservabilityCoverageCorrelations(
	context.Context, ObservabilityCoverageCorrelationWrite,
) (ObservabilityCoverageCorrelationWriteResult, error) {
	return ObservabilityCoverageCorrelationWriteResult{}, nil
}

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
