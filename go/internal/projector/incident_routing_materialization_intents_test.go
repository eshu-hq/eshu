// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildProjectionQueuesIncidentRoutingMaterializationForIncidentRecord(t *testing.T) {
	t.Parallel()

	scopeValue, generation := incidentRoutingProjectionScope()
	envelopes := []facts.Envelope{
		incidentRoutingIncidentEnvelope("incident-fact-1", scopeValue.ScopeID, generation.GenerationID),
		incidentRoutingIncidentEnvelope("incident-fact-2", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainIncidentRoutingMaterialization)
	if got, want := intent.EntityKey, "incident_routing_materialization:pagerduty:account:example"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "incident-fact-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first incident-routing fact", got)
	}
	if got, want := intent.SourceSystem, "pagerduty"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionQueuesIncidentRoutingMaterializationForRoutingFact(t *testing.T) {
	t.Parallel()

	scopeValue, generation := incidentRoutingProjectionScope()
	envelopes := []facts.Envelope{
		incidentRoutingObservedServiceEnvelope("routing-fact-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainIncidentRoutingMaterialization)
	if got, want := intent.EntityKey, "incident_routing_materialization:pagerduty:account:example"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "routing-fact-1"; got != want {
		t.Fatalf("intent.FactID = %q, want routing fact", got)
	}
	if got, want := intent.SourceSystem, "pagerduty"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionDoesNotQueueIncidentRoutingMaterializationWithoutIncidentRoutingFacts(t *testing.T) {
	t.Parallel()

	scopeValue, generation := incidentRoutingProjectionScope()
	projection, err := buildProjection(scopeValue, generation, nil)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainIncidentRoutingMaterialization {
			t.Fatalf("unexpected incident_routing_materialization intent without incident-routing facts")
		}
	}
}

func incidentRoutingProjectionScope() (scope.IngestionScope, scope.ScopeGeneration) {
	scopeValue := scope.IngestionScope{
		ScopeID:      "pagerduty:account:example",
		ScopeKind:    "pagerduty",
		SourceSystem: "pagerduty",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "pagerduty:generation-1",
		ObservedAt:   time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, 6, 1, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	return scopeValue, generation
}

func incidentRoutingIncidentEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.IncidentRecordFactKind,
		SchemaVersion:    facts.IncidentContextSchemaVersionV1,
		CollectorKind:    "pagerduty",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "pagerduty",
		},
		Payload: map[string]any{
			"provider":             "pagerduty",
			"provider_incident_id": "PINCIDENT1",
			"service_id":           "PSERVICE1",
		},
	}
}

func incidentRoutingObservedServiceEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.IncidentRoutingObservedPagerDutyServiceFactKind,
		SchemaVersion:    facts.IncidentRoutingSchemaVersionV1,
		CollectorKind:    "pagerduty",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "pagerduty",
		},
		Payload: map[string]any{
			"provider":           "pagerduty",
			"source_class":       "observed",
			"source_kind":        "pagerduty_api",
			"resource_class":     "service",
			"provider_object_id": "PSERVICE1",
			"service_id":         "PSERVICE1",
		},
	}
}
