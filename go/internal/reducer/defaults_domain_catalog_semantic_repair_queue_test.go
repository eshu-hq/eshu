// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/semanticentity"
)

// semanticEntityMaterializationHandler finds the DomainSemanticEntityMaterialization
// definition implementedDefaultDomainDefinitions built and returns its handler,
// failing the test if the domain or its handler type is missing.
func semanticEntityMaterializationHandler(t *testing.T, definitions []DomainDefinition) semanticentity.SemanticEntityMaterializationHandler {
	t.Helper()

	for _, def := range definitions {
		if def.Domain != DomainSemanticEntityMaterialization {
			continue
		}
		handler, ok := def.Handler.(semanticentity.SemanticEntityMaterializationHandler)
		if !ok {
			t.Fatalf("DomainSemanticEntityMaterialization handler type = %T, want semanticentity.SemanticEntityMaterializationHandler", def.Handler)
		}
		return handler
	}

	t.Fatal("implementedDefaultDomainDefinitions() did not return a DomainSemanticEntityMaterialization definition")
	return semanticentity.SemanticEntityMaterializationHandler{}
}

// TestImplementedDefaultDomainDefinitionsLeavesSemanticEntityRepairQueueNilWithoutRootQueue
// covers the #6061 nil-guard in defaults_domain_catalog.go: when
// DefaultHandlers.GraphProjectionRepairQueue is nil, the built handler's
// RepairQueue must stay nil as an interface value, not a non-nil interface
// wrapping a nil semanticEntityRepairQueueAdapter.queue. Without the guard,
// assigning semanticEntityRepairQueueAdapter{queue: nil} unconditionally
// produces exactly that typed-nil-in-interface: RepairQueue != nil would be
// true, and the handler would dereference the nil queue on publish failure.
func TestImplementedDefaultDomainDefinitionsLeavesSemanticEntityRepairQueueNilWithoutRootQueue(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{})

	handler := semanticEntityMaterializationHandler(t, definitions)
	if handler.RepairQueue != nil {
		t.Fatalf("RepairQueue = %#v, want nil interface when GraphProjectionRepairQueue was not wired", handler.RepairQueue)
	}
}

// TestImplementedDefaultDomainDefinitionsWiresSemanticEntityRepairQueueWhenRootQueueSet
// covers the complementary path: when GraphProjectionRepairQueue is set, the
// built handler's RepairQueue must be non-nil and wrap the provided queue via
// semanticEntityRepairQueueAdapter.
func TestImplementedDefaultDomainDefinitionsWiresSemanticEntityRepairQueueWhenRootQueueSet(t *testing.T) {
	t.Parallel()

	rootQueue := &recordingSemanticEntityRepairQueue{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		GraphProjectionRepairQueue: rootQueue,
	})

	handler := semanticEntityMaterializationHandler(t, definitions)
	if handler.RepairQueue == nil {
		t.Fatal("RepairQueue = nil, want a non-nil adapter wrapping the provided GraphProjectionRepairQueue")
	}
	adapter, ok := handler.RepairQueue.(semanticEntityRepairQueueAdapter)
	if !ok {
		t.Fatalf("RepairQueue type = %T, want semanticEntityRepairQueueAdapter", handler.RepairQueue)
	}
	if adapter.queue != rootQueue {
		t.Fatalf("adapter.queue = %#v, want the exact GraphProjectionRepairQueue passed in DefaultHandlers", adapter.queue)
	}
}
