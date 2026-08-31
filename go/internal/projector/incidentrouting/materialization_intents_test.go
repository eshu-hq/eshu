// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incidentrouting

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	testScopeID      = "pagerduty:account:example"
	testGenerationID = "pagerduty:generation-1"
)

func incidentEnvelope(factID, factKind, sourceSystem, collectorKind string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		FactKind:      factKind,
		CollectorKind: collectorKind,
		ObservedAt:    time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: sourceSystem,
		},
		Payload: map[string]any{
			"provider":   "pagerduty",
			"service_id": "PSERVICE1",
		},
	}
}

// TestBuildIncidentRoutingMaterializationReducerIntent proves the builder
// enqueues from incident.record or any incident_routing.* fact, anchors to the
// earliest candidate fact in original input order regardless of which kind it
// carries, keys the intent by scope, and falls back to CollectorKind when
// SourceRef's SourceSystem is blank.
func TestBuildIncidentRoutingMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues from incident.record, anchored to the earliest fact", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			incidentEnvelope("incident-fact-1", facts.IncidentRecordFactKind, "pagerduty", "pagerduty"),
			incidentEnvelope("incident-fact-2", facts.IncidentRecordFactKind, "pagerduty", "pagerduty"),
		})
		got, ok := BuildIncidentRoutingMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainIncidentRoutingMaterialization,
			EntityKey: "incident_routing_materialization:" + testScopeID,
			Reason:    "pagerduty incident-routing evidence observed",
			FactID:    "incident-fact-1", SourceSystem: "pagerduty",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("queues from each incident_routing source fact kind", func(t *testing.T) {
		t.Parallel()
		for _, kind := range facts.IncidentRoutingFactKinds() {
			lookup := projectorintent.NewFactLookup([]facts.Envelope{
				incidentEnvelope("routing-fact-1", kind, "pagerduty", "pagerduty"),
			})
			got, ok := BuildIncidentRoutingMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
			if !ok {
				t.Fatalf("kind %q: ok = false, want true", kind)
			}
			if got.FactID != "routing-fact-1" {
				t.Fatalf("kind %q: FactID = %q, want routing-fact-1", kind, got.FactID)
			}
		}
	})

	t.Run("anchors to the earliest fact across kinds, not the first-checked kind", func(t *testing.T) {
		t.Parallel()
		// The observed-service routing fact precedes incident.record in input
		// order, so it must win even though incident.record is the first
		// candidate kind the builder lists.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			incidentEnvelope("routing-fact-1", facts.IncidentRoutingObservedPagerDutyServiceFactKind, "pagerduty", "pagerduty"),
			incidentEnvelope("incident-fact-1", facts.IncidentRecordFactKind, "pagerduty", "pagerduty"),
		})
		got, ok := BuildIncidentRoutingMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "routing-fact-1" {
			t.Fatalf("FactID = %q, want routing-fact-1 (earliest in input order)", got.FactID)
		}
	})

	t.Run("falls back to CollectorKind when SourceSystem is blank", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			incidentEnvelope("incident-fact-1", facts.IncidentRecordFactKind, "", "pagerduty_collector"),
		})
		got, ok := BuildIncidentRoutingMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "pagerduty_collector" {
			t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "pagerduty_collector")
		}
	})

	t.Run("does not queue without incident-routing facts", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			incidentEnvelope("unrelated-1", facts.AWSResourceFactKind, "aws", "aws_cloud"),
		})
		got, ok := BuildIncidentRoutingMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t), want zero intent and false", got, ok)
		}
	})
}
