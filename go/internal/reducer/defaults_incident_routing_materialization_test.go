// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/incident"
)

// stubIncidentRoutingEvidenceLoader is a no-op IncidentRoutingEvidenceLoader
// used only to satisfy the non-nil gate in implementedDefaultDomainDefinitions;
// it is a local copy scoped to this registration-gate test (issue #6061), not
// the incident package's own richer test double.
type stubIncidentRoutingEvidenceLoader struct{}

func (stubIncidentRoutingEvidenceLoader) LoadIncidentRoutingRawEvidence(
	context.Context, string, string,
) (incident.IncidentRoutingRawEvidence, error) {
	return incident.IncidentRoutingRawEvidence{}, nil
}

// recordingIncidentRoutingEvidenceWriter is a no-op
// IncidentRoutingEvidenceWriter used only to satisfy the non-nil gate in
// implementedDefaultDomainDefinitions.
type recordingIncidentRoutingEvidenceWriter struct{}

func (recordingIncidentRoutingEvidenceWriter) WriteIncidentRoutingEvidence(
	context.Context, []map[string]any, string, string, string,
) error {
	return nil
}

func (recordingIncidentRoutingEvidenceWriter) RetractIncidentRoutingEvidence(
	context.Context, []string, string, string,
) error {
	return nil
}

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
	writer := recordingIncidentRoutingEvidenceWriter{}
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
		handler, ok := def.Handler.(incident.IncidentRoutingMaterializationHandler)
		if !ok {
			t.Fatalf("incident_routing_materialization handler type = %T, want incident.IncidentRoutingMaterializationHandler", def.Handler)
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
