// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestNewObservedPagerDutyServiceEnvelopeRedactsNamesAndKeepsObservedLane(t *testing.T) {
	t.Parallel()

	env, err := NewObservedPagerDutyServiceEnvelope(testEnvelopeContext(), ConfigService{
		ID:            "SVC1",
		Summary:       "checkout-api",
		Status:        "active",
		AlertCreation: "create_alerts_and_incidents",
		Escalation:    Reference{ID: "EP1", Summary: "platform escalation"},
		Teams:         []Reference{{ID: "TEAM1", Summary: "platform team"}},
		UpdatedAt:     testObservedAt(),
		HTMLURL:       "https://example.pagerduty.com/services/SVC1?token=secret",
		MatchState:    ConfigMatchStateNotCompared,
	})
	if err != nil {
		t.Fatalf("NewObservedPagerDutyServiceEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, facts.IncidentRoutingObservedPagerDutyServiceFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.SchemaVersion, facts.IncidentRoutingSchemaVersionV1; got != want {
		t.Fatalf("SchemaVersion = %q, want %q", got, want)
	}
	if got, want := env.SourceConfidence, facts.SourceConfidenceReported; got != want {
		t.Fatalf("SourceConfidence = %q, want %q", got, want)
	}
	if got, want := env.Payload["source_class"], ConfigSourceClassObserved; got != want {
		t.Fatalf("Payload[source_class] = %#v, want %#v", got, want)
	}
	if got, want := env.Payload["declared_match_state"], ConfigMatchStateNotCompared; got != want {
		t.Fatalf("Payload[declared_match_state] = %#v, want %#v", got, want)
	}
	if _, ok := env.Payload["name_fingerprint"].(string); !ok {
		t.Fatalf("Payload[name_fingerprint] = %#v, want string", env.Payload["name_fingerprint"])
	}
	payload := mustJSON(t, env.Payload)
	for _, secret := range []string{"checkout-api", "platform escalation", "platform team", "token=secret"} {
		if strings.Contains(payload, secret) {
			t.Fatalf("payload %s contains sensitive value %q", payload, secret)
		}
	}
	if strings.Contains(env.SourceRef.SourceURI, "token=secret") {
		t.Fatalf("SourceURI = %q, want sensitive query removed", env.SourceRef.SourceURI)
	}
}

func TestNewObservedPagerDutyIntegrationEnvelopeDropsRoutingKeys(t *testing.T) {
	t.Parallel()

	env, err := NewObservedPagerDutyIntegrationEnvelope(testEnvelopeContext(), ConfigIntegration{
		ID:              "INT1",
		ServiceID:       "SVC1",
		Summary:         "cloudwatch alerts",
		Type:            "events_api_v2_inbound_integration",
		VendorID:        "PAGERDUTY_VENDOR",
		HTMLURL:         "https://example.pagerduty.com/services/SVC1/integrations/INT1?routing_key=secret",
		MatchState:      ConfigMatchStateNotCompared,
		RoutingKey:      "routing-key-secret",
		ManuallyCreated: true,
	})
	if err != nil {
		t.Fatalf("NewObservedPagerDutyIntegrationEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, facts.IncidentRoutingObservedPagerDutyIntegrationFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.Payload["service_reference"], "SVC1"; got != want {
		t.Fatalf("Payload[service_reference] = %#v, want %#v", got, want)
	}
	if got, want := env.Payload["declared_match_state"], ConfigMatchStateNotCompared; got != want {
		t.Fatalf("Payload[declared_match_state] = %#v, want %#v", got, want)
	}
	payload := mustJSON(t, env.Payload)
	for _, secret := range []string{"routing-key-secret", "routing_key=secret", "cloudwatch alerts"} {
		if strings.Contains(payload, secret) {
			t.Fatalf("payload %s contains sensitive value %q", payload, secret)
		}
	}
	if got, want := env.Payload["routing_key_redacted"], true; got != want {
		t.Fatalf("Payload[routing_key_redacted] = %#v, want %#v", got, want)
	}
	if got, want := env.Payload["manually_created"], true; got != want {
		t.Fatalf("Payload[manually_created] = %#v, want %#v", got, want)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(encoded)
}
