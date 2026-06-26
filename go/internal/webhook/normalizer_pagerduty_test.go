// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

type pagerDutyExpected struct {
	Provider   string `json:"provider"`
	EventKind  string `json:"event_kind"`
	EventID    string `json:"event_id"`
	ScopeID    string `json:"scope_id"`
	ResourceID string `json:"resource_id"`
	ObservedAt string `json:"observed_at"`
}

func loadPagerDutyFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", path, err)
	}
	return data
}

func loadPagerDutyExpected(t *testing.T, path string) pagerDutyExpected {
	t.Helper()
	data := loadPagerDutyFixture(t, path)
	var exp pagerDutyExpected
	if err := json.Unmarshal(data, &exp); err != nil {
		t.Fatalf("failed to unmarshal expected %s: %v", path, err)
	}
	return exp
}

func assertPagerDutyTrigger(t *testing.T, got IncidentFreshnessTrigger, want pagerDutyExpected) {
	t.Helper()
	if string(got.Provider) != want.Provider {
		t.Fatalf("Provider = %q, want %q", got.Provider, want.Provider)
	}
	if got.EventKind != want.EventKind {
		t.Fatalf("EventKind = %q, want %q", got.EventKind, want.EventKind)
	}
	if want.EventID != "" && got.EventID != want.EventID {
		t.Fatalf("EventID = %q, want %q", got.EventID, want.EventID)
	}
	if want.ScopeID != "" && got.ScopeID != want.ScopeID {
		t.Fatalf("ScopeID = %q, want %q", got.ScopeID, want.ScopeID)
	}
	if got.ResourceID != want.ResourceID {
		t.Fatalf("ResourceID = %q, want %q", got.ResourceID, want.ResourceID)
	}
	if want.ObservedAt != "" {
		expectedTime, err := time.Parse(time.RFC3339, want.ObservedAt)
		if err != nil {
			t.Fatalf("failed to parse expected observed_at: %v", err)
		}
		if !got.ObservedAt.Equal(expectedTime) {
			t.Fatalf("ObservedAt = %s, want %s", got.ObservedAt, expectedTime)
		}
	}
}

func TestNormalizePagerDutyIncidentFreshnessFromFixture(t *testing.T) {
	t.Parallel()

	payload := loadPagerDutyFixture(t, "testdata/pagerduty/incident.json")
	expected := loadPagerDutyExpected(t, "testdata/pagerduty/incident_expected.json")

	receivedAt := time.Date(2026, time.June, 1, 12, 5, 0, 0, time.UTC)
	trigger, err := NormalizePagerDutyIncidentFreshness(expected.ScopeID, expected.EventID, payload, receivedAt)
	if err != nil {
		t.Fatalf("NormalizePagerDutyIncidentFreshness() error = %v, want nil", err)
	}

	assertPagerDutyTrigger(t, trigger, expected)
}

func TestNormalizePagerDutyIncidentFreshnessHandlesMissingEventID(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"event": {
			"event_type": "incident.acknowledged",
			"occurred_at": "2026-06-01T13:00:00Z",
			"data": {"id": "INC9999"}
		}
	}`)

	trigger, err := NormalizePagerDutyIncidentFreshness("pd:test", "delivery-fallback", payload, time.Date(2026, time.June, 1, 13, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("NormalizePagerDutyIncidentFreshness() error = %v, want nil", err)
	}

	if trigger.EventID != "delivery-fallback" {
		t.Fatalf("EventID = %q, want delivery fallback %q", trigger.EventID, "delivery-fallback")
	}
	if trigger.ResourceID != "INC9999" {
		t.Fatalf("ResourceID = %q, want %q", trigger.ResourceID, "INC9999")
	}
}

func TestNormalizePagerDutyIncidentFreshnessHandlesEmptyData(t *testing.T) {
	t.Parallel()

	payload := loadPagerDutyFixture(t, "testdata/pagerduty/incident_no_data.json")
	expected := loadPagerDutyExpected(t, "testdata/pagerduty/incident_no_data_expected.json")

	receivedAt := time.Date(2026, time.June, 1, 14, 35, 0, 0, time.UTC)
	trigger, err := NormalizePagerDutyIncidentFreshness(expected.ScopeID, expected.EventID, payload, receivedAt)
	if err != nil {
		t.Fatalf("NormalizePagerDutyIncidentFreshness() error = %v, want nil", err)
	}

	assertPagerDutyTrigger(t, trigger, expected)
}

func TestNormalizePagerDutyIncidentFreshnessRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	_, err := NormalizePagerDutyIncidentFreshness("pd:test", "delivery-malformed", []byte(`{`), time.Now())
	if err == nil {
		t.Fatal("NormalizePagerDutyIncidentFreshness() error = nil, want decode error")
	}
}

func TestNormalizePagerDutyIncidentFreshnessValidatesRequiredFields(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, time.June, 1, 12, 5, 0, 0, time.UTC)
	payload := []byte(`{
		"event": {
			"id": "evt-valid",
			"event_type": "incident.triggered",
			"occurred_at": "2026-06-01T12:00:00Z",
			"data": {"id": "INC1234"}
		}
	}`)

	trigger, err := NormalizePagerDutyIncidentFreshness("pd:test", "evt-valid", payload, receivedAt)
	if err != nil {
		t.Fatalf("NormalizePagerDutyIncidentFreshness() error = %v, want nil", err)
	}
	if err := trigger.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}
