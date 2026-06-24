// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

const (
	referenceFactKindIncident            = "dev.eshu.examples.pagerduty.incident"
	referenceFactKindLifecycleEvent      = "dev.eshu.examples.pagerduty.lifecycle_event"
	referenceFactKindChange              = "dev.eshu.examples.pagerduty.change"
	referenceFactKindObservedService     = "dev.eshu.examples.pagerduty.observed_service"
	referenceFactKindObservedIntegration = "dev.eshu.examples.pagerduty.observed_integration"
	referenceFactKindCoverageWarning     = "dev.eshu.examples.pagerduty.coverage_warning"
)

func TestReferenceComponentFixtureMatchesInTreePagerDutyContract(t *testing.T) {
	t.Parallel()

	result := readReferenceFixtureResult(t, "complete-result.json")
	validation, err := sdkcollector.NewValidator(referenceComponentContract()).ValidateResult(result)
	if err != nil {
		t.Fatalf("ValidateResult() error = %v, want nil", err)
	}
	if got, want := validation.FactCount, 6; got != want {
		t.Fatalf("FactCount = %d, want %d", got, want)
	}

	expected := referenceExpectedCoreFacts(t)
	if got, want := referenceFactKinds(result.Facts), []string{
		referenceFactKindIncident,
		referenceFactKindLifecycleEvent,
		referenceFactKindChange,
		referenceFactKindObservedService,
		referenceFactKindObservedIntegration,
		referenceFactKindCoverageWarning,
	}; !slices.Equal(got, want) {
		t.Fatalf("fact kinds = %v, want %v", got, want)
	}

	for i, fact := range result.Facts {
		core := expected[i]
		if got, want := fact.StableKey, core.StableFactKey; got != want {
			t.Fatalf("fact[%d].StableKey = %q, want in-tree key %q", i, got, want)
		}
		if got, want := fact.SchemaVersion, core.SchemaVersion; got != want {
			t.Fatalf("fact[%d].SchemaVersion = %q, want %q", i, got, want)
		}
		if got, want := string(fact.SourceConfidence), core.SourceConfidence; got != want {
			t.Fatalf("fact[%d].SourceConfidence = %q, want %q", i, got, want)
		}
		if got, want := fact.SourceRef.RecordID, core.SourceRef.SourceRecordID; got != want {
			t.Fatalf("fact[%d].SourceRef.RecordID = %q, want %q", i, got, want)
		}
		if got, want := fact.SourceRef.URI, core.SourceRef.SourceURI; got != want {
			t.Fatalf("fact[%d].SourceRef.URI = %q, want %q", i, got, want)
		}
		assertReferencePayloadMatches(t, i, fact, core.Payload)
	}
}

func readReferenceFixtureResult(t *testing.T, name string) sdkcollector.Result {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "..",
		"examples", "collector-extensions", "pagerduty", "testdata", "fixtures", name,
	))
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v, want nil", name, err)
	}
	var result sdkcollector.Result
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v, want nil", name, err)
	}
	return result
}

func referenceComponentContract() sdkcollector.Contract {
	return sdkcollector.Contract{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		Facts: []sdkcollector.FactDeclaration{
			referenceFactDeclaration(referenceFactKindIncident),
			referenceFactDeclaration(referenceFactKindLifecycleEvent),
			referenceFactDeclaration(referenceFactKindChange),
			referenceFactDeclaration(referenceFactKindObservedService),
			referenceFactDeclaration(referenceFactKindObservedIntegration),
			referenceFactDeclaration(referenceFactKindCoverageWarning),
		},
	}
}

func referenceFactDeclaration(kind string) sdkcollector.FactDeclaration {
	return sdkcollector.FactDeclaration{
		Kind:             kind,
		SchemaVersions:   []string{"1.0.0"},
		SourceConfidence: []sdkcollector.SourceConfidence{sdkcollector.SourceConfidenceReported},
	}
}

func referenceExpectedCoreFacts(t *testing.T) []facts.Envelope {
	t.Helper()

	ctx := EnvelopeContext{
		ScopeID:             "pagerduty:account:synthetic-reference",
		GenerationID:        "pagerduty-reference-generation-2026-06-14",
		CollectorInstanceID: "pagerduty-reference-local",
		ObservedAt:          referenceObservedAt(),
		SourceURI:           "https://example.com/eshu-fixtures/pagerduty/reference",
	}
	incident, err := NewIncidentRecordEnvelope(ctx, Incident{
		ID:             "pd_incident_synthetic_001",
		IncidentNumber: 1001,
		Status:         "triggered",
		Urgency:        "high",
		Service:        Reference{ID: "pd_service_synthetic_primary", Type: "service"},
		Escalation:     Reference{ID: "pd_policy_synthetic_primary", Type: "escalation_policy"},
		CreatedAt:      referenceObservedAt().Add(-20 * time.Minute),
		UpdatedAt:      referenceObservedAt().Add(-5 * time.Minute),
		HTMLURL:        "https://example.com/eshu-fixtures/pagerduty/incidents/pd_incident_synthetic_001",
	})
	if err != nil {
		t.Fatalf("NewIncidentRecordEnvelope() error = %v, want nil", err)
	}
	lifecycle, err := NewLifecycleEventEnvelope(ctx, LifecycleEvent{
		ID:         "pd_log_synthetic_001",
		IncidentID: "pd_incident_synthetic_001",
		Type:       "trigger_log_entry",
		Channel:    "api",
		CreatedAt:  referenceObservedAt().Add(-19 * time.Minute),
		HTMLURL:    "https://example.com/eshu-fixtures/pagerduty/log_entries/pd_log_synthetic_001",
	})
	if err != nil {
		t.Fatalf("NewLifecycleEventEnvelope() error = %v, want nil", err)
	}
	change, err := NewChangeRecordEnvelope(ctx, ChangeEvent{
		ID:        "pd_change_synthetic_001",
		Source:    "deployment",
		Services:  []Reference{{ID: "pd_service_synthetic_primary", Type: "service"}},
		Timestamp: referenceObservedAt().Add(-30 * time.Minute),
		HTMLURL:   "https://example.com/eshu-fixtures/pagerduty/change_events/pd_change_synthetic_001",
	})
	if err != nil {
		t.Fatalf("NewChangeRecordEnvelope() error = %v, want nil", err)
	}
	service, err := NewObservedPagerDutyServiceEnvelope(ctx, ConfigService{
		ID:            "pd_service_synthetic_primary",
		Status:        "active",
		AlertCreation: "create_alerts_and_incidents",
		Escalation:    Reference{ID: "pd_policy_synthetic_primary"},
		CreatedAt:     referenceObservedAt().Add(-24 * time.Hour),
		UpdatedAt:     referenceObservedAt().Add(-1 * time.Hour),
		HTMLURL:       "https://example.com/eshu-fixtures/pagerduty/services/pd_service_synthetic_primary",
	})
	if err != nil {
		t.Fatalf("NewObservedPagerDutyServiceEnvelope() error = %v, want nil", err)
	}
	integration, err := NewObservedPagerDutyIntegrationEnvelope(ctx, ConfigIntegration{
		ID:                 "pd_integration_synthetic_001",
		ServiceID:          "pd_service_synthetic_primary",
		Type:               "events_api_v2_inbound_integration",
		VendorID:           "pd_vendor_synthetic_cloudwatch",
		HTMLURL:            "https://example.com/eshu-fixtures/pagerduty/integrations/pd_integration_synthetic_001",
		CreatedAt:          referenceObservedAt().Add(-23 * time.Hour),
		UpdatedAt:          referenceObservedAt().Add(-2 * time.Hour),
		RoutingKeyRedacted: true,
	})
	if err != nil {
		t.Fatalf("NewObservedPagerDutyIntegrationEnvelope() error = %v, want nil", err)
	}
	warning, err := NewPagerDutyConfigCoverageWarningEnvelope(ctx, ConfigWarning{
		ResourceClass: ConfigResourceClassRelatedChangeEvent,
		ResourceID:    "pd_incident_synthetic_001",
		Reason:        ConfigWarningPermissionHidden,
	})
	if err != nil {
		t.Fatalf("NewPagerDutyConfigCoverageWarningEnvelope() error = %v, want nil", err)
	}
	return []facts.Envelope{incident, lifecycle, change, service, integration, warning}
}

func referenceObservedAt() time.Time {
	return time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC)
}

func referenceFactKinds(facts []sdkcollector.Fact) []string {
	kinds := make([]string, 0, len(facts))
	for _, fact := range facts {
		kinds = append(kinds, fact.Kind)
	}
	return kinds
}

func assertReferencePayloadMatches(t *testing.T, index int, fact sdkcollector.Fact, want map[string]any) {
	t.Helper()

	gotPayload := fact.Payload
	wantPayload := referenceJSONPayload(t, want)
	if _, ok := wantPayload["routing_key_redacted"]; ok {
		delete(wantPayload, "routing_key_redacted")
		assertReferenceRedaction(t, index, fact, "routing_key", "secret_value")
	}
	if !reflect.DeepEqual(gotPayload, wantPayload) {
		t.Fatalf("fact[%d].Payload = %#v, want %#v", index, gotPayload, wantPayload)
	}
}

func referenceJSONPayload(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload) error = %v, want nil", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v, want nil", err)
	}
	return out
}

func assertReferenceRedaction(t *testing.T, index int, fact sdkcollector.Fact, field string, reason string) {
	t.Helper()

	for _, redaction := range fact.Redactions {
		if redaction.Field == field && redaction.Reason == reason {
			return
		}
	}
	t.Fatalf("fact[%d].Redactions = %#v, want %s/%s", index, fact.Redactions, field, reason)
}
