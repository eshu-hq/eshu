// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestImplementedDefaultDomainDefinitionsWiresAzureResourceMaterialization(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:              &stubFactLoader{},
		CloudResourceNodeWriter: &recordingCloudResourceNodeWriter{},
	})

	for _, def := range definitions {
		if def.Domain != DomainAzureResourceMaterialization {
			continue
		}
		handler, ok := def.Handler.(AzureResourceMaterializationHandler)
		if !ok {
			t.Fatalf("azure_resource_materialization handler type = %T, want AzureResourceMaterializationHandler", def.Handler)
		}
		if handler.FactLoader == nil || handler.NodeWriter == nil {
			t.Fatal("azure_resource_materialization dependencies were not wired")
		}
		return
	}
	t.Fatal("azure_resource_materialization not registered after wiring fact loader and node writer")
}

func TestImplementedDefaultDomainDefinitionsWiresAzureRelationshipMaterialization(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                   &stubFactLoader{},
		AzureCloudResourceEdgeWriter: writer,
		ReadinessLookup:              readyLookup(true, true),
	})

	for _, def := range definitions {
		if def.Domain != DomainAzureRelationshipMaterialization {
			continue
		}
		handler, ok := def.Handler.(AzureRelationshipMaterializationHandler)
		if !ok {
			t.Fatalf("azure_relationship_materialization handler type = %T, want AzureRelationshipMaterializationHandler", def.Handler)
		}
		if handler.FactLoader == nil || handler.EdgeWriter == nil || handler.ReadinessLookup == nil {
			t.Fatal("azure_relationship_materialization dependencies were not wired")
		}
		return
	}
	t.Fatal("azure_relationship_materialization not registered after wiring fact loader and edge writer")
}

func TestImplementedDefaultDomainDefinitionsOmitsAzureRelationshipWithoutEdgeWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{FactLoader: &stubFactLoader{}})
	for _, def := range definitions {
		if def.Domain == DomainAzureRelationshipMaterialization {
			t.Fatal("azure_relationship_materialization registered without edge writer")
		}
	}
}
