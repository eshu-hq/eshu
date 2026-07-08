// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
)

// NewObservedPagerDutyServiceEnvelope converts one live PagerDuty service into
// an incident-routing source fact.
func NewObservedPagerDutyServiceEnvelope(ctx EnvelopeContext, service ConfigService) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	if strings.TrimSpace(service.ID) == "" {
		return facts.Envelope{}, fmt.Errorf("pagerduty observed service id must not be blank")
	}
	payload := observedConfigBasePayload(ctx, ConfigResourceClassService, service.ID, service.MatchState)
	payload["service_id"] = strings.TrimSpace(service.ID)
	if value := strings.TrimSpace(service.Status); value != "" {
		payload["status"] = value
	}
	if value := strings.TrimSpace(service.AlertCreation); value != "" {
		payload["alert_creation"] = value
	}
	if value := strings.TrimSpace(service.Escalation.ID); value != "" {
		payload["escalation_policy_reference"] = value
	}
	if teams := referenceIDs(service.Teams); len(teams) > 0 {
		payload["team_references"] = teams
	}
	setSensitiveFingerprint(payload, "name_fingerprint", service.Summary)
	setTimePayload(payload, "created_at", service.CreatedAt)
	setTimePayload(payload, "updated_at", service.UpdatedAt)
	setConfigBooleans(payload, service.Disabled, service.Deleted, service.ManuallyCreated)
	setDriftCandidate(payload, service.DriftReason)
	setRedactionState(payload)

	sourceURI := firstNonBlank(service.HTMLURL, ctx.SourceURI)
	if value := safeSourceURI(sourceURI); value != "" {
		payload["source_url"] = value
	}
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeIncidentRoutingObservedPagerDutyService(incidentv1.ObservedPagerDutyService{
			Provider:                  ProviderPagerDuty,
			SourceClass:               ConfigSourceClassObserved,
			SourceKind:                ConfigSourceKindPagerDutyAPI,
			Outcome:                   "observed",
			ResourceClass:             ConfigResourceClassService,
			ProviderObjectID:          strings.TrimSpace(service.ID),
			ScopeID:                   ctx.ScopeID,
			DeclaredMatchState:        payload["declared_match_state"].(string),
			RedactionState:            payload["redaction_state"].(string),
			ServiceID:                 strings.TrimSpace(service.ID),
			Status:                    optionalStringPtrFromPayload(payload, "status"),
			AlertCreation:             optionalStringPtrFromPayload(payload, "alert_creation"),
			EscalationPolicyReference: optionalStringPtrFromPayload(payload, "escalation_policy_reference"),
			TeamReferences:            referenceIDs(service.Teams),
			NameFingerprint:           optionalStringPtrFromPayload(payload, "name_fingerprint"),
			CreatedAt:                 optionalStringPtrFromPayload(payload, "created_at"),
			UpdatedAt:                 optionalStringPtrFromPayload(payload, "updated_at"),
			Disabled:                  optionalBoolPtrFromPayload(payload, "disabled"),
			Deleted:                   optionalBoolPtrFromPayload(payload, "deleted"),
			ManuallyCreated:           optionalBoolPtrFromPayload(payload, "manually_created"),
			DriftCandidateReason:      optionalStringPtrFromPayload(payload, "drift_candidate_reason"),
			SourceURL:                 optionalStringPtrFromPayload(payload, "source_url"),
			CollectorInstanceID:       stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := providerStableKey(facts.IncidentRoutingObservedPagerDutyServiceFactKind, ctx.ScopeID, service.ID)
	return incidentRoutingEnvelope(
		ctx,
		facts.IncidentRoutingObservedPagerDutyServiceFactKind,
		stableKey,
		payload,
		sourceURI,
		service.ID,
	), nil
}

// NewObservedPagerDutyIntegrationEnvelope converts one live PagerDuty service
// integration into an incident-routing source fact.
func NewObservedPagerDutyIntegrationEnvelope(
	ctx EnvelopeContext,
	integration ConfigIntegration,
) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	if strings.TrimSpace(integration.ID) == "" {
		return facts.Envelope{}, fmt.Errorf("pagerduty observed integration id must not be blank")
	}
	payload := observedConfigBasePayload(
		ctx,
		ConfigResourceClassServiceIntegration,
		integration.ID,
		integration.MatchState,
	)
	payload["integration_id"] = strings.TrimSpace(integration.ID)
	if value := strings.TrimSpace(integration.ServiceID); value != "" {
		payload["service_reference"] = value
	}
	if value := strings.TrimSpace(integration.Type); value != "" {
		payload["integration_type"] = value
	}
	if value := strings.TrimSpace(integration.VendorID); value != "" {
		payload["vendor_reference"] = value
	}
	if strings.TrimSpace(integration.RoutingKey) != "" || integration.RoutingKeyRedacted {
		payload["routing_key_redacted"] = true
	}
	setSensitiveFingerprint(payload, "name_fingerprint", integration.Summary)
	setTimePayload(payload, "created_at", integration.CreatedAt)
	setTimePayload(payload, "updated_at", integration.UpdatedAt)
	setConfigBooleans(payload, integration.Disabled, integration.Deleted, integration.ManuallyCreated)
	setDriftCandidate(payload, integration.DriftReason)
	setRedactionState(payload)

	sourceURI := firstNonBlank(integration.HTMLURL, ctx.SourceURI)
	if value := safeSourceURI(sourceURI); value != "" {
		payload["source_url"] = value
	}
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeIncidentRoutingObservedPagerDutyIntegration(incidentv1.ObservedPagerDutyIntegration{
			Provider:             ProviderPagerDuty,
			SourceClass:          ConfigSourceClassObserved,
			SourceKind:           ConfigSourceKindPagerDutyAPI,
			Outcome:              "observed",
			ResourceClass:        ConfigResourceClassServiceIntegration,
			ProviderObjectID:     strings.TrimSpace(integration.ID),
			ScopeID:              ctx.ScopeID,
			DeclaredMatchState:   payload["declared_match_state"].(string),
			RedactionState:       payload["redaction_state"].(string),
			IntegrationID:        strings.TrimSpace(integration.ID),
			ServiceReference:     optionalStringPtrFromPayload(payload, "service_reference"),
			IntegrationType:      optionalStringPtrFromPayload(payload, "integration_type"),
			VendorReference:      optionalStringPtrFromPayload(payload, "vendor_reference"),
			RoutingKeyRedacted:   optionalBoolPtrFromPayload(payload, "routing_key_redacted"),
			NameFingerprint:      optionalStringPtrFromPayload(payload, "name_fingerprint"),
			CreatedAt:            optionalStringPtrFromPayload(payload, "created_at"),
			UpdatedAt:            optionalStringPtrFromPayload(payload, "updated_at"),
			Disabled:             optionalBoolPtrFromPayload(payload, "disabled"),
			Deleted:              optionalBoolPtrFromPayload(payload, "deleted"),
			ManuallyCreated:      optionalBoolPtrFromPayload(payload, "manually_created"),
			DriftCandidateReason: optionalStringPtrFromPayload(payload, "drift_candidate_reason"),
			SourceURL:            optionalStringPtrFromPayload(payload, "source_url"),
			CollectorInstanceID:  stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := providerStableKey(
		facts.IncidentRoutingObservedPagerDutyIntegrationFactKind,
		ctx.ScopeID,
		firstNonBlank(integration.ServiceID, "service")+":"+integration.ID,
	)
	return incidentRoutingEnvelope(
		ctx,
		facts.IncidentRoutingObservedPagerDutyIntegrationFactKind,
		stableKey,
		payload,
		sourceURI,
		integration.ID,
	), nil
}

// NewPagerDutyConfigCoverageWarningEnvelope converts one live-config warning
// into an incident-routing coverage warning fact.
func NewPagerDutyConfigCoverageWarningEnvelope(ctx EnvelopeContext, warning ConfigWarning) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	reason := strings.TrimSpace(warning.Reason)
	if reason == "" {
		reason = ConfigWarningPartial
	}
	resourceClass := strings.TrimSpace(warning.ResourceClass)
	if resourceClass == "" {
		resourceClass = "unknown"
	}
	resourceID := strings.TrimSpace(warning.ResourceID)
	payload := observedConfigBasePayload(ctx, resourceClass, resourceID, ConfigMatchStateNotCompared)
	payload["outcome"] = "partial"
	payload["reason"] = reason
	payload["redaction_state"] = "none"
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeIncidentRoutingCoverageWarning(incidentv1.CoverageWarning{
			Provider:            stringPtr(ProviderPagerDuty),
			SourceClass:         ConfigSourceClassObserved,
			SourceKind:          ConfigSourceKindPagerDutyAPI,
			Outcome:             "partial",
			ScopeID:             ctx.ScopeID,
			Reason:              reason,
			RedactionState:      "none",
			DeclaredMatchState:  payload["declared_match_state"].(string),
			ResourceClass:       stringPtr(resourceClass),
			ProviderObjectID:    stringPtr(resourceID),
			CollectorInstanceID: stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.IncidentRoutingCoverageWarningFactKind, map[string]any{
		"provider":       ProviderPagerDuty,
		"scope_id":       ctx.ScopeID,
		"resource_class": resourceClass,
		"resource_id":    resourceID,
		"reason":         reason,
	})
	return incidentRoutingEnvelope(
		ctx,
		facts.IncidentRoutingCoverageWarningFactKind,
		stableKey,
		payload,
		ctx.SourceURI,
		firstNonBlank(resourceID, reason),
	), nil
}

func incidentRoutingEnvelope(
	ctx EnvelopeContext,
	kind string,
	stableKey string,
	payload map[string]any,
	sourceURI string,
	recordID string,
) facts.Envelope {
	version, _ := facts.IncidentRoutingSchemaVersion(kind)
	return facts.Envelope{
		FactID: facts.StableID("IncidentRoutingFact", map[string]any{
			"fact_kind":     kind,
			"stable_key":    stableKey,
			"scope_id":      ctx.ScopeID,
			"generation_id": ctx.GenerationID,
		}),
		ScopeID:          ctx.ScopeID,
		GenerationID:     ctx.GenerationID,
		FactKind:         kind,
		StableFactKey:    stableKey,
		SchemaVersion:    version,
		CollectorKind:    string(scope.CollectorPagerDuty),
		FencingToken:     ctx.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       normalizedObservedAt(ctx.ObservedAt),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   ProviderPagerDuty,
			ScopeID:        ctx.ScopeID,
			GenerationID:   ctx.GenerationID,
			FactKey:        stableKey,
			SourceURI:      safeSourceURI(sourceURI),
			SourceRecordID: strings.TrimSpace(recordID),
		},
	}
}

func observedConfigBasePayload(ctx EnvelopeContext, resourceClass, resourceID, matchState string) map[string]any {
	matchState = strings.TrimSpace(matchState)
	if matchState == "" {
		matchState = ConfigMatchStateNotCompared
	}
	return map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              ProviderPagerDuty,
		"source_class":          ConfigSourceClassObserved,
		"source_kind":           ConfigSourceKindPagerDutyAPI,
		"outcome":               "observed",
		"resource_class":        strings.TrimSpace(resourceClass),
		"provider_object_id":    strings.TrimSpace(resourceID),
		"scope_id":              ctx.ScopeID,
		"declared_match_state":  matchState,
	}
}

func referenceIDs(refs []Reference) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if value := strings.TrimSpace(ref.ID); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func setSensitiveFingerprint(payload map[string]any, key string, value string) {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		payload[key] = pagerDutyConfigFingerprint(trimmed)
	}
}

func setTimePayload(payload map[string]any, key string, value time.Time) {
	if !value.IsZero() {
		payload[key] = value.UTC().Format(time.RFC3339Nano)
	}
}

func setConfigBooleans(payload map[string]any, disabled, deleted, manuallyCreated bool) {
	if disabled {
		payload["disabled"] = true
	}
	if deleted {
		payload["deleted"] = true
	}
	if manuallyCreated {
		payload["manually_created"] = true
	}
}

func setDriftCandidate(payload map[string]any, reason string) {
	if value := strings.TrimSpace(reason); value != "" {
		payload["drift_candidate_reason"] = value
	}
}

func setRedactionState(payload map[string]any) {
	for key := range payload {
		if strings.HasSuffix(key, "_fingerprint") || strings.HasSuffix(key, "_redacted") {
			payload["redaction_state"] = "applied"
			return
		}
	}
	payload["redaction_state"] = "none"
}

func pagerDutyConfigFingerprint(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}
