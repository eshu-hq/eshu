package reducer

import "testing"

func TestImplementedDefaultDomainDefinitionsOmitsIncidentRoutingWithoutWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		IncidentRoutingHandlers: IncidentRoutingHandlers{
			IncidentRoutingEvidenceLoader: stubIncidentRoutingEvidenceLoader{},
		},
	})
	for _, def := range definitions {
		if def.Domain == DomainIncidentRoutingMaterialization {
			t.Fatalf("incident_routing_materialization registered without writer; want omitted to avoid silent intent drops")
		}
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesIncidentRoutingWhenWired(t *testing.T) {
	t.Parallel()

	loader := stubIncidentRoutingEvidenceLoader{}
	writer := &recordingIncidentRoutingEvidenceWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		IncidentRoutingHandlers: IncidentRoutingHandlers{
			IncidentRoutingEvidenceLoader: loader,
			IncidentRoutingEvidenceWriter: writer,
		},
	})

	found := false
	for _, def := range definitions {
		if def.Domain != DomainIncidentRoutingMaterialization {
			continue
		}
		found = true
		handler, ok := def.Handler.(IncidentRoutingMaterializationHandler)
		if !ok {
			t.Fatalf("incident_routing_materialization handler type = %T, want IncidentRoutingMaterializationHandler", def.Handler)
		}
		if handler.Loader == nil {
			t.Fatal("incident_routing_materialization handler Loader was not wired")
		}
		if handler.Writer != writer {
			t.Fatal("incident_routing_materialization handler Writer was not wired")
		}
		if !def.Ownership.CanonicalWrite {
			t.Fatal("incident_routing_materialization must declare CanonicalWrite ownership")
		}
	}
	if !found {
		t.Fatal("incident_routing_materialization not registered after wiring loader+writer")
	}
}
