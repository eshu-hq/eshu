// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestImplementedDefaultDomainDefinitionsWiresGCPResourceMaterializationInstruments(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingCloudResourceNodeWriter{}
	instruments := &telemetry.Instruments{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:              loader,
		CloudResourceNodeWriter: writer,
		Instruments:             instruments,
	})

	for _, def := range definitions {
		if def.Domain != DomainGCPResourceMaterialization {
			continue
		}
		handler, ok := def.Handler.(GCPResourceMaterializationHandler)
		if !ok {
			t.Fatalf("gcp_resource_materialization handler type = %T, want GCPResourceMaterializationHandler", def.Handler)
		}
		if handler.FactLoader != loader {
			t.Fatal("gcp_resource_materialization handler FactLoader was not wired")
		}
		if handler.NodeWriter != writer {
			t.Fatal("gcp_resource_materialization handler NodeWriter was not wired")
		}
		if handler.Instruments != instruments {
			t.Fatal("gcp_resource_materialization handler Instruments was not wired")
		}
		return
	}

	t.Fatal("gcp_resource_materialization not registered after wiring fact loader and node writer")
}

func TestImplementedDefaultDomainDefinitionsWiresGCPRelationshipMaterializationInstruments(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{}
	writer := &recordingCloudResourceEdgeWriter{}
	instruments := &telemetry.Instruments{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                 loader,
		GCPCloudResourceEdgeWriter: writer,
		ReadinessLookup:            readyLookup(true, true),
		Instruments:                instruments,
	})

	for _, def := range definitions {
		if def.Domain != DomainGCPRelationshipMaterialization {
			continue
		}
		handler, ok := def.Handler.(GCPRelationshipMaterializationHandler)
		if !ok {
			t.Fatalf("gcp_relationship_materialization handler type = %T, want GCPRelationshipMaterializationHandler", def.Handler)
		}
		if handler.FactLoader != loader {
			t.Fatal("gcp_relationship_materialization handler FactLoader was not wired")
		}
		if handler.EdgeWriter != writer {
			t.Fatal("gcp_relationship_materialization handler EdgeWriter was not wired")
		}
		if handler.ReadinessLookup == nil {
			t.Fatal("gcp_relationship_materialization handler ReadinessLookup was not wired")
		}
		if handler.Instruments != instruments {
			t.Fatal("gcp_relationship_materialization handler Instruments was not wired")
		}
		return
	}

	t.Fatal("gcp_relationship_materialization not registered after wiring fact loader and edge writer")
}
