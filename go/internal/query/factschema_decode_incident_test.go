// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"
	"time"
)

// validIncidentRecordPayload returns a payload satisfying every required
// field of incidentv1.IncidentRecord.
func validIncidentRecordPayload() map[string]any {
	return map[string]any{
		"provider":             "pagerduty",
		"provider_incident_id": "PABC123",
	}
}

// TestDecodeIncidentContextIncidentDropsRowMissingBothIdentityFields proves
// the accuracy guarantee: a incident.record payload missing
// provider_incident_id AND carrying no usable source_record_id is classified
// input_invalid and dropped, never decoded to an empty-identity incident.
func TestDecodeIncidentContextIncidentDropsRowMissingBothIdentityFields(t *testing.T) {
	t.Parallel()

	payload := validIncidentRecordPayload()
	delete(payload, "provider_incident_id")
	row := incidentContextFactRow{
		FactID:        "malformed-incident",
		SchemaVersion: "1.0.0",
		Payload:       payload,
		// SourceRecordID intentionally empty: no fallback identity available.
	}

	incident, ok := decodeIncidentContextIncident(row)
	if ok {
		t.Fatalf("decodeIncidentContextIncident() ok = true, want false (dropped); got %+v", incident)
	}
	if incident.ProviderIncidentID != "" || incident.EvidenceFactID != "" {
		t.Fatalf("dropped incident is not zero-valued: %+v", incident)
	}
}

// TestDecodeIncidentContextIncidentFallsBackToSourceRecordID proves the
// documented, tested read path stays intact: a payload that omits
// provider_incident_id entirely, but whose fact carries a durable
// source_record_id, still decodes successfully using that id as the incident
// identity (mirrors
// TestPostgresIncidentContextStoreReadsCollectedPagerDutyIncidentBySourceRecordID
// at the store level).
func TestDecodeIncidentContextIncidentFallsBackToSourceRecordID(t *testing.T) {
	t.Parallel()

	payload := validIncidentRecordPayload()
	delete(payload, "provider_incident_id")
	row := incidentContextFactRow{
		FactID:         "fallback-incident",
		SchemaVersion:  "1.0.0",
		Payload:        payload,
		SourceRecordID: "PABC123",
	}

	incident, ok := decodeIncidentContextIncident(row)
	if !ok {
		t.Fatal("decodeIncidentContextIncident() ok = false, want true (source_record_id fallback)")
	}
	if got, want := incident.ProviderIncidentID, "PABC123"; got != want {
		t.Fatalf("ProviderIncidentID = %q, want %q", got, want)
	}
}

// validLifecycleEventPayload returns a payload satisfying every required
// field of incidentv1.LifecycleEvent.
func validLifecycleEventPayload() map[string]any {
	return map[string]any{
		"provider":             "pagerduty",
		"provider_event_id":    "log-1",
		"provider_incident_id": "PABC123",
	}
}

func TestDecodeIncidentContextTimelineEventDropsRowMissingBothIdentityFields(t *testing.T) {
	t.Parallel()

	payload := validLifecycleEventPayload()
	delete(payload, "provider_event_id")
	row := incidentContextFactRow{
		FactID:        "malformed-event",
		SchemaVersion: "1.0.0",
		Payload:       payload,
	}

	event, ok := decodeIncidentContextTimelineEvent(row)
	if ok {
		t.Fatalf("decodeIncidentContextTimelineEvent() ok = true, want false (dropped); got %+v", event)
	}
	if event.EventID != "" {
		t.Fatalf("dropped event is not zero-valued: %+v", event)
	}
}

func TestDecodeIncidentContextTimelineEventFallsBackToSourceRecordID(t *testing.T) {
	t.Parallel()

	payload := validLifecycleEventPayload()
	delete(payload, "provider_event_id")
	row := incidentContextFactRow{
		FactID:         "fallback-event",
		SchemaVersion:  "1.0.0",
		Payload:        payload,
		SourceRecordID: "log-1",
	}

	event, ok := decodeIncidentContextTimelineEvent(row)
	if !ok {
		t.Fatal("decodeIncidentContextTimelineEvent() ok = false, want true (source_record_id fallback)")
	}
	if got, want := event.EventID, "log-1"; got != want {
		t.Fatalf("EventID = %q, want %q", got, want)
	}
}

// validChangeRecordPayload returns a payload satisfying every required field
// of incidentv1.ChangeRecord.
func validChangeRecordPayload() map[string]any {
	return map[string]any{
		"provider":           "pagerduty",
		"provider_change_id": "change-1",
	}
}

func TestDecodeIncidentContextChangeCandidateDropsRowMissingBothIdentityFields(t *testing.T) {
	t.Parallel()

	payload := validChangeRecordPayload()
	delete(payload, "provider_change_id")
	row := incidentContextFactRow{
		FactID:        "malformed-change",
		SchemaVersion: "1.0.0",
		Payload:       payload,
	}

	change, ok := decodeIncidentContextChangeCandidate(row)
	if ok {
		t.Fatalf("decodeIncidentContextChangeCandidate() ok = true, want false (dropped); got %+v", change)
	}
	if change.ChangeID != "" {
		t.Fatalf("dropped change is not zero-valued: %+v", change)
	}
}

func TestDecodeIncidentContextChangeCandidateFallsBackToSourceRecordID(t *testing.T) {
	t.Parallel()

	payload := validChangeRecordPayload()
	delete(payload, "provider_change_id")
	row := incidentContextFactRow{
		FactID:         "fallback-change",
		SchemaVersion:  "1.0.0",
		Payload:        payload,
		SourceRecordID: "change-1",
	}

	change, ok := decodeIncidentContextChangeCandidate(row)
	if !ok {
		t.Fatal("decodeIncidentContextChangeCandidate() ok = false, want true (source_record_id fallback)")
	}
	if got, want := change.ChangeID, "change-1"; got != want {
		t.Fatalf("ChangeID = %q, want %q", got, want)
	}
}

// validAppliedPagerDutyResourcePayload returns a payload satisfying every
// required field of incidentv1.AppliedPagerDutyResource.
func validAppliedPagerDutyResourcePayload() map[string]any {
	return map[string]any{
		"source_class":            "applied",
		"source_kind":             "terraform_state",
		"outcome":                 "applied",
		"resource_class":          "service",
		"terraform_state_address": "pagerduty_service.checkout",
		"resource_type":           "pagerduty_service",
		"resource_name":           "checkout",
		"module_address":          "",
		"provider_address":        "registry.terraform.io/pagerduty/pagerduty",
		"scope_id":                "terraform:workspace:prod",
		"state_generation_id":     "generation-1",
		"state_lineage":           "lineage-1",
		"backend_kind":            "s3",
		"locator_hash":            "hash-1",
		"declared_match_state":    "not_compared",
		"redaction_state":         "redacted",
	}
}

func TestBuildIncidentAppliedPagerDutyRoutingDropsRowMissingRequiredField(t *testing.T) {
	t.Parallel()

	payload := validAppliedPagerDutyResourcePayload()
	delete(payload, "resource_class")
	row := incidentContextFactRow{
		FactID:        "malformed-applied-routing",
		SchemaVersion: "1.0.0",
		Payload:       payload,
	}

	routing, ok := buildIncidentAppliedPagerDutyRouting(row)
	if ok {
		t.Fatalf("buildIncidentAppliedPagerDutyRouting() ok = true, want false (dropped); got %+v", routing)
	}
	if routing.FactID != "" {
		t.Fatalf("dropped routing is not zero-valued: %+v", routing)
	}
}

func TestBuildIncidentAppliedPagerDutyRoutingDecodesValidPayload(t *testing.T) {
	t.Parallel()

	row := incidentContextFactRow{
		FactID:        "valid-applied-routing",
		SchemaVersion: "1.0.0",
		Payload:       validAppliedPagerDutyResourcePayload(),
		ObservedAt:    time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}

	routing, ok := buildIncidentAppliedPagerDutyRouting(row)
	if !ok {
		t.Fatal("buildIncidentAppliedPagerDutyRouting() ok = false, want true")
	}
	if got, want := routing.ResourceClass, "service"; got != want {
		t.Fatalf("ResourceClass = %q, want %q", got, want)
	}
	if got, want := routing.StateGenerationID, "generation-1"; got != want {
		t.Fatalf("StateGenerationID = %q, want %q", got, want)
	}
}

// validObservedPagerDutyServicePayload returns a payload satisfying every
// required field of incidentv1.ObservedPagerDutyService.
func validObservedPagerDutyServicePayload() map[string]any {
	return map[string]any{
		"provider":             "pagerduty",
		"source_class":         "observed",
		"source_kind":          "pagerduty_api",
		"outcome":              "observed",
		"resource_class":       "service",
		"provider_object_id":   "PSERVICE1",
		"scope_id":             "pagerduty:account:prod",
		"declared_match_state": "not_compared",
		"redaction_state":      "redacted",
		"service_id":           "PSERVICE1",
	}
}

func TestBuildIncidentObservedPagerDutyRoutingDropsRowMissingRequiredField(t *testing.T) {
	t.Parallel()

	payload := validObservedPagerDutyServicePayload()
	delete(payload, "service_id")
	row := incidentContextFactRow{
		FactID:        "malformed-observed-routing",
		SchemaVersion: "1.0.0",
		Payload:       payload,
	}

	routing, ok := buildIncidentObservedPagerDutyRouting(row)
	if ok {
		t.Fatalf("buildIncidentObservedPagerDutyRouting() ok = true, want false (dropped); got %+v", routing)
	}
	if routing.FactID != "" {
		t.Fatalf("dropped routing is not zero-valued: %+v", routing)
	}
}

func TestBuildIncidentObservedPagerDutyRoutingDecodesValidPayload(t *testing.T) {
	t.Parallel()

	row := incidentContextFactRow{
		FactID:        "valid-observed-routing",
		SchemaVersion: "1.0.0",
		Payload:       validObservedPagerDutyServicePayload(),
	}

	routing, ok := buildIncidentObservedPagerDutyRouting(row)
	if !ok {
		t.Fatal("buildIncidentObservedPagerDutyRouting() ok = false, want true")
	}
	if got, want := routing.ServiceID, "PSERVICE1"; got != want {
		t.Fatalf("ServiceID = %q, want %q", got, want)
	}
}

// validCoverageWarningPayload returns a payload satisfying every required
// field of incidentv1.CoverageWarning (the intersection both emitters set).
func validCoverageWarningPayload() map[string]any {
	return map[string]any{
		"source_class":         "observed",
		"source_kind":          "pagerduty_api",
		"outcome":              "partial",
		"scope_id":             "pagerduty:account:prod",
		"reason":               "insufficient permission to enumerate all services",
		"redaction_state":      "redacted",
		"declared_match_state": "not_compared",
	}
}

func TestBuildIncidentRoutingCoverageWarningDropsRowMissingRequiredField(t *testing.T) {
	t.Parallel()

	payload := validCoverageWarningPayload()
	delete(payload, "reason")
	row := incidentContextFactRow{
		FactID:        "malformed-coverage-warning",
		SchemaVersion: "1.0.0",
		Payload:       payload,
	}

	warning, ok := buildIncidentRoutingCoverageWarning(row)
	if ok {
		t.Fatalf("buildIncidentRoutingCoverageWarning() ok = true, want false (dropped); got %+v", warning)
	}
	if warning.FactID != "" {
		t.Fatalf("dropped warning is not zero-valued: %+v", warning)
	}
}

func TestBuildIncidentRoutingCoverageWarningDecodesValidPayload(t *testing.T) {
	t.Parallel()

	row := incidentContextFactRow{
		FactID:        "valid-coverage-warning",
		SchemaVersion: "1.0.0",
		Payload:       validCoverageWarningPayload(),
	}

	warning, ok := buildIncidentRoutingCoverageWarning(row)
	if !ok {
		t.Fatal("buildIncidentRoutingCoverageWarning() ok = false, want true")
	}
	if got, want := warning.Reason, "insufficient permission to enumerate all services"; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
}

// TestDecodeIncidentServiceCatalogOperationalLinkDecodesEmptyPayload proves
// every field of servicecatalogv1.OperationalLink is optional: an entirely
// empty payload still decodes successfully rather than dead-lettering, unlike
// the other converted kinds in this file.
func TestDecodeIncidentServiceCatalogOperationalLinkDecodesEmptyPayload(t *testing.T) {
	t.Parallel()

	row := incidentContextFactRow{
		FactID:        "operational-link",
		SchemaVersion: "1.0.0",
		Payload:       map[string]any{},
	}

	link, ok := decodeIncidentServiceCatalogOperationalLink(row)
	if !ok {
		t.Fatal("decodeIncidentServiceCatalogOperationalLink() ok = false, want true (every field optional)")
	}
	if link.FactID != "operational-link" {
		t.Fatalf("FactID = %q, want %q", link.FactID, "operational-link")
	}
}

// TestDecodeIncidentServiceCatalogOperationalLinkDropsUnsupportedSchemaMajor
// proves an unsupported schema major still dead-letters even for a kind with
// no required fields.
func TestDecodeIncidentServiceCatalogOperationalLinkDropsUnsupportedSchemaMajor(t *testing.T) {
	t.Parallel()

	row := incidentContextFactRow{
		FactID:        "operational-link-v2",
		SchemaVersion: "2.0.0",
		Payload:       map[string]any{"provider": "pagerduty"},
	}

	link, ok := decodeIncidentServiceCatalogOperationalLink(row)
	if ok {
		t.Fatalf("decodeIncidentServiceCatalogOperationalLink() ok = true, want false (unsupported schema major); got %+v", link)
	}
}
