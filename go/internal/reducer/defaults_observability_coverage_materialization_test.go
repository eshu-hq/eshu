// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
)

// recordingObservabilityCoverageEdgeWriter is a minimal
// [ObservabilityCoverageEdgeWriter] double for wiring tests: it only needs to
// satisfy the interface for a pointer-identity check, never called here.
// Package-local copy of the obscoverage test package's fuller recorder of the
// same name (issue #6061) -- that one is unexported and cannot cross the
// package boundary.
type recordingObservabilityCoverageEdgeWriter struct{}

func (*recordingObservabilityCoverageEdgeWriter) WriteObservabilityCoverageEdges(
	context.Context, []map[string]any, string, string, string,
) error {
	return nil
}

func (*recordingObservabilityCoverageEdgeWriter) RetractObservabilityCoverageEdges(
	context.Context, []string, string, string,
) error {
	return nil
}

func (*recordingObservabilityCoverageEdgeWriter) RetractObservabilityCoverageEdgesByUIDs(
	context.Context, []string, []string, string,
) error {
	return nil
}

// TestImplementedDefaultDomainDefinitionsOmitsObservabilityCoverageMaterializationWithoutEdgeWriter
// proves the additive registration gate: with a FactLoader but no coverage edge
// writer the COVERS edge domain must stay unregistered, mirroring the
// aws_relationship_materialization gate, so intents are never silently dropped.
func TestImplementedDefaultDomainDefinitionsOmitsObservabilityCoverageMaterializationWithoutEdgeWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader: &stubFactLoader{},
	})
	for _, def := range definitions {
		if def.Domain == DomainObservabilityCoverageMaterialization {
			t.Fatalf("observability_coverage_materialization registered without edge writer; want omitted to avoid silent intent drops")
		}
	}
}

// TestImplementedDefaultDomainDefinitionsIncludesObservabilityCoverageMaterializationWhenWired
// proves the COVERS edge domain registers with the gated handler once both the
// FactLoader and the edge writer are wired, including the readiness lookup that
// holds edges until the canonical nodes commit.
func TestImplementedDefaultDomainDefinitionsIncludesObservabilityCoverageMaterializationWhenWired(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingObservabilityCoverageEdgeWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                      loader,
		ObservabilityCoverageEdgeWriter: writer,
		ReadinessLookup:                 readyLookup(true, true),
	})
	found := false
	for _, def := range definitions {
		if def.Domain != DomainObservabilityCoverageMaterialization {
			continue
		}
		found = true
		handler, ok := def.Handler.(ObservabilityCoverageMaterializationHandler)
		if !ok {
			t.Fatalf("observability_coverage_materialization handler type = %T, want ObservabilityCoverageMaterializationHandler", def.Handler)
		}
		if handler.FactLoader != loader {
			t.Fatal("observability_coverage_materialization handler FactLoader was not wired")
		}
		if handler.EdgeWriter != writer {
			t.Fatal("observability_coverage_materialization handler EdgeWriter was not wired")
		}
		if handler.ReadinessLookup == nil {
			t.Fatal("observability_coverage_materialization handler ReadinessLookup was not wired")
		}
		if !def.Ownership.CanonicalWrite {
			t.Fatal("observability_coverage_materialization must declare CanonicalWrite ownership")
		}
	}
	if !found {
		t.Fatal("observability_coverage_materialization not registered after wiring loader+edge writer")
	}
}
