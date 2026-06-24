// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

// Contract returns the SDK fact families accepted for this reference package.
func Contract() sdk.Contract {
	return sdk.Contract{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Facts: []sdk.FactDeclaration{
			factDeclaration(FactKindIncident),
			factDeclaration(FactKindLifecycleEvent),
			factDeclaration(FactKindChange),
			factDeclaration(FactKindObservedService),
			factDeclaration(FactKindObservedIntegration),
			factDeclaration(FactKindCoverageWarning),
		},
	}
}

// LoadObservation decodes one redacted PagerDuty fixture from r.
func LoadObservation(r io.Reader) (Observation, error) {
	var observation Observation
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&observation); err != nil {
		return Observation{}, fmt.Errorf("decode pagerduty observation: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Observation{}, fmt.Errorf("decode pagerduty observation: trailing JSON value")
		}
		return Observation{}, fmt.Errorf("decode pagerduty observation trailer: %w", err)
	}
	if observation.ObservedAt.IsZero() {
		return Observation{}, fmt.Errorf("observed_at is required")
	}
	return observation, nil
}

// Collect converts one redacted fixture into an SDK result for claim.
func Collect(claim sdk.Claim, observation Observation) (sdk.Result, error) {
	if err := validateClaim(claim); err != nil {
		return sdk.Result{}, err
	}
	sourceURI := strings.TrimSpace(observation.SourceURI)
	if sourceURI == "" {
		sourceURI = "https://example.com/eshu-fixtures/pagerduty/reference"
	}
	result := sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           sdk.ResultComplete,
		Claim:           claim,
		Generation: sdk.Generation{
			ID:            claim.GenerationID,
			ObservedAt:    observation.ObservedAt.UTC(),
			FreshnessHint: "fixture_only",
		},
	}
	for _, incident := range observation.Incidents {
		result.Facts = append(result.Facts, incidentFact(claim, observation.ObservedAt, sourceURI, incident))
	}
	for _, event := range observation.LifecycleEvents {
		result.Facts = append(result.Facts, lifecycleEventFact(claim, observation.ObservedAt, sourceURI, event))
	}
	for _, change := range observation.Changes {
		result.Facts = append(result.Facts, changeFact(claim, observation.ObservedAt, sourceURI, change))
	}
	for _, service := range observation.Services {
		result.Facts = append(result.Facts, serviceFact(claim, observation.ObservedAt, sourceURI, service))
	}
	for _, integration := range observation.Integrations {
		result.Facts = append(result.Facts, integrationFact(claim, observation.ObservedAt, sourceURI, integration))
	}
	for _, warning := range observation.Warnings {
		result.Facts = append(result.Facts, warningFact(claim, observation.ObservedAt, sourceURI, warning))
	}
	result.Statuses = []sdk.Status{{Class: sdk.StatusComplete, FactCount: len(result.Facts)}}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		return sdk.Result{}, fmt.Errorf("validate pagerduty reference result: %w", err)
	}
	return result, nil
}

func factDeclaration(kind string) sdk.FactDeclaration {
	return sdk.FactDeclaration{
		Kind:             kind,
		SchemaVersions:   []string{"1.0.0"},
		SourceConfidence: []sdk.SourceConfidence{sdk.SourceConfidenceReported},
	}
}

func validateClaim(claim sdk.Claim) error {
	if claim.ComponentID != ComponentID {
		return fmt.Errorf("claim component_id %q does not match %q", claim.ComponentID, ComponentID)
	}
	if claim.CollectorKind != CollectorKind {
		return fmt.Errorf("claim collector_kind %q does not match %q", claim.CollectorKind, CollectorKind)
	}
	if claim.SourceSystem != SourceSystem {
		return fmt.Errorf("claim source_system %q does not match %q", claim.SourceSystem, SourceSystem)
	}
	if strings.TrimSpace(claim.Scope.ID) == "" {
		return fmt.Errorf("claim scope.id is required")
	}
	if strings.TrimSpace(claim.GenerationID) == "" {
		return fmt.Errorf("claim generation_id is required")
	}
	return nil
}

func incidentFact(claim sdk.Claim, observedAt time.Time, sourceURI string, incident Incident) sdk.Fact {
	stableKey := providerStableKey(coreFactKindIncident, claim.Scope.ID, incident.ID)
	payload := map[string]any{
		"collector_instance_id": claim.InstanceID,
		"provider":              SourceSystem,
		"provider_incident_id":  strings.TrimSpace(incident.ID),
		"incident_number":       incident.IncidentNumber,
		"title":                 strings.TrimSpace(incident.Title),
		"status":                strings.TrimSpace(incident.Status),
		"urgency":               strings.TrimSpace(incident.Urgency),
		"priority":              referencePayload(incident.Priority),
		"service_id":            strings.TrimSpace(incident.Service.ID),
		"service":               referencePayload(incident.Service),
		"escalation_policy":     referencePayload(incident.Escalation),
		"teams":                 referencesPayload(incident.Teams),
		"assignments":           referencesPayload(incident.Assignments),
		"created_at":            timeString(incident.CreatedAt),
		"updated_at":            timeString(incident.UpdatedAt),
		"resolved_at":           timeString(incident.ResolvedAt),
		"source_url":            safeSourceURI(firstNonBlank(incident.HTMLURL, sourceURI)),
	}
	return sdkFact(claim, FactKindIncident, stableKey, observedAt, sourceURI, incident.HTMLURL, incident.ID, payload)
}

func lifecycleEventFact(claim sdk.Claim, observedAt time.Time, sourceURI string, event LifecycleEvent) sdk.Fact {
	stableKey := stableID(coreFactKindLifecycleEvent, map[string]any{
		"provider":    SourceSystem,
		"scope_id":    claim.Scope.ID,
		"incident_id": strings.TrimSpace(event.IncidentID),
		"event_id":    strings.TrimSpace(event.ID),
	})
	payload := map[string]any{
		"collector_instance_id": claim.InstanceID,
		"provider":              SourceSystem,
		"provider_event_id":     strings.TrimSpace(event.ID),
		"provider_incident_id":  strings.TrimSpace(event.IncidentID),
		"event_type":            strings.TrimSpace(event.Type),
		"actor":                 referencePayload(event.Actor),
		"channel":               strings.TrimSpace(event.Channel),
		"summary":               strings.TrimSpace(event.Summary),
		"created_at":            timeString(event.CreatedAt),
		"source_url":            safeSourceURI(firstNonBlank(event.HTMLURL, sourceURI)),
	}
	return sdkFact(claim, FactKindLifecycleEvent, stableKey, observedAt, sourceURI, event.HTMLURL, event.ID, payload)
}

func changeFact(claim sdk.Claim, observedAt time.Time, sourceURI string, change ChangeEvent) sdk.Fact {
	stableKey := providerStableKey(coreFactKindChange, claim.Scope.ID, change.ID)
	payload := map[string]any{
		"collector_instance_id": claim.InstanceID,
		"provider":              SourceSystem,
		"provider_change_id":    strings.TrimSpace(change.ID),
		"summary":               strings.TrimSpace(change.Summary),
		"source":                strings.TrimSpace(change.Source),
		"services":              referencesPayload(change.Services),
		"links":                 linksPayload(change.Links),
		"timestamp":             timeString(change.Timestamp),
		"source_url":            safeSourceURI(firstNonBlank(change.HTMLURL, sourceURI)),
	}
	return sdkFact(claim, FactKindChange, stableKey, observedAt, sourceURI, change.HTMLURL, change.ID, payload)
}

func serviceFact(claim sdk.Claim, observedAt time.Time, sourceURI string, service ConfigService) sdk.Fact {
	payload := observedConfigBasePayload(claim, "service", service.ID, service.MatchState)
	payload["service_id"] = strings.TrimSpace(service.ID)
	setString(payload, "status", service.Status)
	setString(payload, "alert_creation", service.AlertCreation)
	setReferenceID(payload, "escalation_policy_reference", service.Escalation)
	setReferenceIDs(payload, "team_references", service.Teams)
	setSensitiveFingerprint(payload, "name_fingerprint", service.Summary)
	setTimePayload(payload, "created_at", service.CreatedAt)
	setTimePayload(payload, "updated_at", service.UpdatedAt)
	setConfigBooleans(payload, service.Disabled, service.Deleted, service.ManuallyCreated)
	setString(payload, "drift_candidate_reason", service.DriftReason)
	setRedactionState(payload)
	setString(payload, "source_url", safeSourceURI(firstNonBlank(service.HTMLURL, sourceURI)))

	stableKey := providerStableKey(coreFactKindObservedService, claim.Scope.ID, service.ID)
	return sdkFact(claim, FactKindObservedService, stableKey, observedAt, sourceURI, service.HTMLURL, service.ID, payload)
}

func integrationFact(claim sdk.Claim, observedAt time.Time, sourceURI string, integration Integration) sdk.Fact {
	payload := observedConfigBasePayload(claim, "service_integration", integration.ID, integration.MatchState)
	payload["integration_id"] = strings.TrimSpace(integration.ID)
	setString(payload, "service_reference", integration.ServiceID)
	setString(payload, "integration_type", integration.Type)
	setString(payload, "vendor_reference", integration.VendorID)
	setSensitiveFingerprint(payload, "name_fingerprint", integration.Summary)
	setTimePayload(payload, "created_at", integration.CreatedAt)
	setTimePayload(payload, "updated_at", integration.UpdatedAt)
	setConfigBooleans(payload, integration.Disabled, integration.Deleted, integration.ManuallyCreated)
	setString(payload, "drift_candidate_reason", integration.DriftReason)
	setRedactionState(payload)
	if integration.RoutingKeyRedacted {
		payload["redaction_state"] = "applied"
	}
	setString(payload, "source_url", safeSourceURI(firstNonBlank(integration.HTMLURL, sourceURI)))

	stableKey := providerStableKey(
		coreFactKindObservedIntegration,
		claim.Scope.ID,
		firstNonBlank(integration.ServiceID, "service")+":"+integration.ID,
	)
	fact := sdkFact(
		claim,
		FactKindObservedIntegration,
		stableKey,
		observedAt,
		sourceURI,
		integration.HTMLURL,
		integration.ID,
		payload,
	)
	if integration.RoutingKeyRedacted {
		fact.Redactions = append(fact.Redactions, sdk.Redaction{Field: "routing_key", Reason: "secret_value"})
	}
	return fact
}

func warningFact(claim sdk.Claim, observedAt time.Time, sourceURI string, warning ConfigWarning) sdk.Fact {
	reason := firstNonBlank(warning.Reason, "partial")
	resourceClass := firstNonBlank(warning.ResourceClass, "unknown")
	resourceID := strings.TrimSpace(warning.ResourceID)
	payload := observedConfigBasePayload(claim, resourceClass, resourceID, "not_compared")
	payload["outcome"] = "partial"
	payload["reason"] = reason
	payload["redaction_state"] = "none"
	stableKey := stableID(coreFactKindCoverageWarning, map[string]any{
		"provider":       SourceSystem,
		"scope_id":       claim.Scope.ID,
		"resource_class": resourceClass,
		"resource_id":    resourceID,
		"reason":         reason,
	})
	return sdkFact(
		claim,
		FactKindCoverageWarning,
		stableKey,
		observedAt,
		sourceURI,
		sourceURI,
		firstNonBlank(resourceID, reason),
		payload,
	)
}

func sdkFact(
	claim sdk.Claim,
	kind string,
	stableKey string,
	observedAt time.Time,
	defaultSourceURI string,
	sourceURI string,
	recordID string,
	payload map[string]any,
) sdk.Fact {
	refURI := safeSourceURI(firstNonBlank(sourceURI, defaultSourceURI))
	return sdk.Fact{
		Kind:             kind,
		SchemaVersion:    "1.0.0",
		StableKey:        stableKey,
		SourceConfidence: sdk.SourceConfidenceReported,
		ObservedAt:       observedAt.UTC(),
		SourceRef: sdk.SourceRef{
			SourceSystem: claim.SourceSystem,
			ScopeID:      claim.Scope.ID,
			GenerationID: claim.GenerationID,
			FactKey:      stableKey,
			URI:          refURI,
			RecordID:     strings.TrimSpace(recordID),
		},
		Payload: payload,
	}
}

func observedConfigBasePayload(claim sdk.Claim, resourceClass, resourceID, matchState string) map[string]any {
	return map[string]any{
		"collector_instance_id": claim.InstanceID,
		"provider":              SourceSystem,
		"source_class":          "observed",
		"source_kind":           "pagerduty_api",
		"outcome":               "observed",
		"resource_class":        strings.TrimSpace(resourceClass),
		"provider_object_id":    strings.TrimSpace(resourceID),
		"scope_id":              claim.Scope.ID,
		"declared_match_state":  firstNonBlank(matchState, "not_compared"),
	}
}
