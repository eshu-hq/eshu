// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestNewIncidentRecordEnvelopeKeepsUnlinkedPagerDutyIncidentUseful(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	incident := testIncident("P123")
	env, err := NewIncidentRecordEnvelope(ctx, incident)
	if err != nil {
		t.Fatalf("NewIncidentRecordEnvelope() error = %v, want nil", err)
	}

	if got, want := env.FactKind, facts.IncidentRecordFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.SourceConfidence, facts.SourceConfidenceReported; got != want {
		t.Fatalf("SourceConfidence = %q, want %q", got, want)
	}
	if got := env.SourceRef.SourceURI; strings.Contains(got, "token=secret") {
		t.Fatalf("SourceURI = %q, want sensitive query removed", got)
	}
	if _, ok := env.Payload["work_item_id"]; ok {
		t.Fatal("Payload unexpectedly linked the PagerDuty incident to a work item")
	}
	if got, want := env.Payload["provider_incident_id"], "P123"; got != want {
		t.Fatalf("Payload[provider_incident_id] = %#v, want %#v", got, want)
	}
	if got, want := env.Payload["service_id"], "SVC1"; got != want {
		t.Fatalf("Payload[service_id] = %#v, want %#v", got, want)
	}
}

func TestIncidentRecordStableKeyIsProviderNativeAcrossGenerations(t *testing.T) {
	t.Parallel()

	firstCtx := testEnvelopeContext()
	secondCtx := firstCtx
	secondCtx.GenerationID = "pagerduty:generation-2"
	incident := testIncident("P123")

	first, err := NewIncidentRecordEnvelope(firstCtx, incident)
	if err != nil {
		t.Fatalf("NewIncidentRecordEnvelope(first) error = %v", err)
	}
	second, err := NewIncidentRecordEnvelope(secondCtx, incident)
	if err != nil {
		t.Fatalf("NewIncidentRecordEnvelope(second) error = %v", err)
	}

	if first.StableFactKey != second.StableFactKey {
		t.Fatalf("StableFactKey changed across generations: got %q and %q", first.StableFactKey, second.StableFactKey)
	}
	if first.FactID == second.FactID {
		t.Fatal("FactID did not include generation identity")
	}
}

func TestNewLifecycleEventEnvelopeUsesLogEntryIdentity(t *testing.T) {
	t.Parallel()

	env, err := NewLifecycleEventEnvelope(testEnvelopeContext(), LifecycleEvent{
		ID:         "R1",
		IncidentID: "P123",
		Type:       "acknowledge_log_entry",
		Actor:      Reference{ID: "USR1", Summary: "oncall"},
		CreatedAt:  testObservedAt().Add(2 * time.Minute),
		HTMLURL:    "https://example.pagerduty.com/incidents/P123/log_entries/R1?api_key=secret",
	})
	if err != nil {
		t.Fatalf("NewLifecycleEventEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, facts.IncidentLifecycleEventFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.SourceRef.SourceRecordID, "R1"; got != want {
		t.Fatalf("SourceRecordID = %q, want %q", got, want)
	}
	if got := env.SourceRef.SourceURI; strings.Contains(got, "api_key=secret") {
		t.Fatalf("SourceURI = %q, want sensitive query removed", got)
	}
}

func TestNewChangeRecordEnvelopeRedactsLinkedURLs(t *testing.T) {
	t.Parallel()

	env, err := NewChangeRecordEnvelope(testEnvelopeContext(), ChangeEvent{
		ID:        "CE1",
		Summary:   "Deploy checkout-api",
		Source:    "github",
		Timestamp: testObservedAt().Add(-10 * time.Minute),
		HTMLURL:   "https://example.pagerduty.com/change_events/CE1?signature=secret",
		Links: []Link{{
			Href: "https://github.com/example/checkout-api/pull/42?token=secret",
			Text: "PR 42",
		}},
	})
	if err != nil {
		t.Fatalf("NewChangeRecordEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, facts.ChangeRecordFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	links, ok := env.Payload["links"].([]map[string]string)
	if !ok || len(links) != 1 {
		t.Fatalf("Payload[links] = %#v, want one redacted link", env.Payload["links"])
	}
	if strings.Contains(links[0]["href"], "token=secret") {
		t.Fatalf("link href = %q, want sensitive query removed", links[0]["href"])
	}
}

func testEnvelopeContext() EnvelopeContext {
	return EnvelopeContext{
		ScopeID:             "pagerduty:account:example",
		GenerationID:        "pagerduty:generation-1",
		CollectorInstanceID: "pagerduty-primary",
		FencingToken:        42,
		ObservedAt:          testObservedAt(),
		SourceURI:           "https://example.pagerduty.com/incidents/P123?token=secret",
	}
}

func testIncident(id string) Incident {
	return Incident{
		ID:             id,
		IncidentNumber: 123,
		Title:          "checkout-api latency",
		Status:         "triggered",
		Urgency:        "high",
		Service:        Reference{ID: "SVC1", Summary: "checkout-api", HTMLURL: "https://example.pagerduty.com/services/SVC1"},
		Assignments:    []Reference{{ID: "USR1", Summary: "primary oncall"}},
		CreatedAt:      testObservedAt().Add(-15 * time.Minute),
		UpdatedAt:      testObservedAt(),
		HTMLURL:        "https://example.pagerduty.com/incidents/" + id + "?token=secret",
	}
}

func testObservedAt() time.Time {
	return time.Date(2026, time.May, 31, 17, 0, 0, 0, time.UTC)
}
