// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const listIncidentRoutingDeclaredEvidenceQuery = `
SELECT
    entity_id,
    repo_id,
    relative_path,
    entity_name,
    start_line,
    metadata
FROM content_entities
WHERE entity_type = 'PagerDutyDeclaration'
  AND metadata->>'source_class' = 'declared'
  AND lower(coalesce(metadata->>'service_name', '')) = ANY($1::text[])
ORDER BY repo_id ASC, relative_path ASC, start_line ASC, entity_id ASC
`

// LoadIncidentRoutingEvidence implements reducer.IncidentRoutingEvidenceLoader
// for PagerDuty incident-routing graph materialization.
func (s FactStore) LoadIncidentRoutingEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]reducer.IncidentRoutingEvidenceInput, error) {
	factKinds := append([]string{facts.IncidentRecordFactKind}, facts.IncidentRoutingFactKinds()...)
	envelopes, err := s.ListFactsByKind(ctx, scopeID, generationID, factKinds)
	if err != nil {
		return nil, err
	}

	incidents := make([]reducer.IncidentRoutingIncident, 0)
	applied := make([]reducer.IncidentRoutingAppliedEvidence, 0)
	observed := make([]reducer.IncidentRoutingObservedEvidence, 0)
	warnings := make([]reducer.IncidentRoutingCoverageWarning, 0)
	for _, envelope := range envelopes {
		if envelope.IsTombstone {
			continue
		}
		switch envelope.FactKind {
		case facts.IncidentRecordFactKind:
			if incident := incidentRoutingIncidentFromEnvelope(envelope); incident.ProviderIncidentID != "" {
				incidents = append(incidents, incident)
			}
		case facts.IncidentRoutingAppliedPagerDutyResourceFactKind:
			if appliedEvidence, ok := incidentRoutingAppliedFromEnvelope(envelope); ok {
				applied = append(applied, appliedEvidence)
			}
		case facts.IncidentRoutingObservedPagerDutyServiceFactKind:
			observed = append(observed, incidentRoutingObservedFromEnvelope(envelope))
		case facts.IncidentRoutingCoverageWarningFactKind:
			warnings = append(warnings, incidentRoutingWarningFromEnvelope(envelope))
		}
	}
	if len(incidents) == 0 {
		return nil, nil
	}

	declared, err := s.loadIncidentRoutingDeclaredEvidence(ctx, incidentRoutingServiceNameAllowlist(incidents))
	if err != nil {
		return nil, err
	}

	inputs := make([]reducer.IncidentRoutingEvidenceInput, 0, len(incidents))
	for _, incident := range incidents {
		inputs = append(inputs, reducer.IncidentRoutingEvidenceInput{
			Incident: incident,
			Declared: declared,
			Applied:  applied,
			Observed: observed,
			Warnings: warnings,
		})
	}
	return inputs, nil
}

func (s FactStore) loadIncidentRoutingDeclaredEvidence(
	ctx context.Context,
	serviceNames []string,
) ([]reducer.IncidentRoutingDeclaredEvidence, error) {
	if len(serviceNames) == 0 {
		return nil, nil
	}
	if s.db == nil {
		return nil, fmt.Errorf("incident routing declaration database is required")
	}

	rows, err := s.db.QueryContext(ctx, listIncidentRoutingDeclaredEvidenceQuery, serviceNames)
	if err != nil {
		return nil, fmt.Errorf("list incident routing declared evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]reducer.IncidentRoutingDeclaredEvidence, 0)
	for rows.Next() {
		var item reducer.IncidentRoutingDeclaredEvidence
		var metadataBytes []byte
		if err := rows.Scan(
			&item.EntityID,
			&item.RepoID,
			&item.RelativePath,
			&item.EntityName,
			&item.StartLine,
			&metadataBytes,
		); err != nil {
			return nil, fmt.Errorf("scan incident routing declared evidence: %w", err)
		}
		var metadata map[string]any
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			return nil, fmt.Errorf("decode incident routing declared evidence metadata: %w", err)
		}
		item.DeclarationKind = incidentRoutingPayloadString(metadata, "declaration_kind")
		item.SourceClass = incidentRoutingPayloadString(metadata, "source_class")
		item.Outcome = incidentRoutingPayloadString(metadata, "outcome")
		item.ServiceName = incidentRoutingPayloadString(metadata, "service_name")
		item.ServiceNameResolution = incidentRoutingPayloadString(metadata, "service_name_resolution")
		item.EscalationPolicy = incidentRoutingPayloadString(metadata, "escalation_policy")
		item.Environment = incidentRoutingPayloadString(metadata, "environment")
		item.Workspace = incidentRoutingPayloadString(metadata, "workspace")
		item.RedactionState = incidentRoutingPayloadString(metadata, "redaction_state")
		item.UnsupportedReason = incidentRoutingPayloadString(metadata, "unsupported_reason")
		item.DuplicateServiceName = incidentRoutingPayloadBool(metadata, "duplicate_service_name")
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate incident routing declared evidence: %w", err)
	}
	return out, nil
}

func incidentRoutingIncidentFromEnvelope(envelope facts.Envelope) reducer.IncidentRoutingIncident {
	service := incidentRoutingPayloadMap(envelope.Payload, "service")
	return reducer.IncidentRoutingIncident{
		Provider:           firstNonEmptyString(incidentRoutingPayloadString(envelope.Payload, "provider"), "pagerduty"),
		ProviderIncidentID: firstNonEmptyString(incidentRoutingPayloadString(envelope.Payload, "provider_incident_id"), envelope.SourceRef.SourceRecordID),
		ScopeID:            envelope.ScopeID,
		ServiceID:          firstNonEmptyString(incidentRoutingPayloadString(envelope.Payload, "service_id"), incidentRoutingPayloadString(service, "id")),
		ServiceName:        incidentRoutingPayloadString(service, "summary"),
		ServiceURL:         incidentRoutingPayloadString(service, "url"),
		EvidenceFactID:     envelope.FactID,
		SourceURL:          firstNonEmptyString(incidentRoutingPayloadString(envelope.Payload, "source_url"), envelope.SourceRef.SourceURI),
		SourceConfidence:   envelope.SourceConfidence,
		ObservedAt:         incidentRoutingFormatTime(envelope.ObservedAt),
	}
}

func incidentRoutingAppliedFromEnvelope(envelope facts.Envelope) (reducer.IncidentRoutingAppliedEvidence, bool) {
	if incidentRoutingPayloadString(envelope.Payload, "resource_class") != "service" {
		return reducer.IncidentRoutingAppliedEvidence{}, false
	}
	return reducer.IncidentRoutingAppliedEvidence{
		FactID:                    envelope.FactID,
		SourceClass:               incidentRoutingPayloadString(envelope.Payload, "source_class"),
		SourceKind:                incidentRoutingPayloadString(envelope.Payload, "source_kind"),
		Outcome:                   incidentRoutingPayloadString(envelope.Payload, "outcome"),
		ResourceClass:             incidentRoutingPayloadString(envelope.Payload, "resource_class"),
		ProviderObjectID:          incidentRoutingPayloadString(envelope.Payload, "provider_object_id"),
		NameFingerprint:           incidentRoutingPayloadString(envelope.Payload, "name_fingerprint"),
		EscalationPolicyReference: incidentRoutingPayloadString(envelope.Payload, "escalation_policy_reference"),
		TerraformStateAddress:     incidentRoutingPayloadString(envelope.Payload, "terraform_state_address"),
		ProviderAddress:           incidentRoutingPayloadString(envelope.Payload, "provider_address"),
		ModuleAddress:             incidentRoutingPayloadString(envelope.Payload, "module_address"),
		StateGenerationID:         incidentRoutingPayloadString(envelope.Payload, "state_generation_id"),
		DeclaredMatchState:        incidentRoutingPayloadString(envelope.Payload, "declared_match_state"),
		RedactionState:            incidentRoutingPayloadString(envelope.Payload, "redaction_state"),
		ObservedAt:                incidentRoutingFormatTime(envelope.ObservedAt),
	}, true
}

func incidentRoutingObservedFromEnvelope(envelope facts.Envelope) reducer.IncidentRoutingObservedEvidence {
	return reducer.IncidentRoutingObservedEvidence{
		FactID:                    envelope.FactID,
		SourceClass:               incidentRoutingPayloadString(envelope.Payload, "source_class"),
		SourceKind:                incidentRoutingPayloadString(envelope.Payload, "source_kind"),
		Outcome:                   incidentRoutingPayloadString(envelope.Payload, "outcome"),
		ServiceID:                 incidentRoutingPayloadString(envelope.Payload, "service_id"),
		ProviderObjectID:          incidentRoutingPayloadString(envelope.Payload, "provider_object_id"),
		NameFingerprint:           incidentRoutingPayloadString(envelope.Payload, "name_fingerprint"),
		Status:                    incidentRoutingPayloadString(envelope.Payload, "status"),
		EscalationPolicyReference: incidentRoutingPayloadString(envelope.Payload, "escalation_policy_reference"),
		DeclaredMatchState:        incidentRoutingPayloadString(envelope.Payload, "declared_match_state"),
		DriftCandidateReason:      incidentRoutingPayloadString(envelope.Payload, "drift_candidate_reason"),
		RedactionState:            incidentRoutingPayloadString(envelope.Payload, "redaction_state"),
		SourceURL:                 firstNonEmptyString(incidentRoutingPayloadString(envelope.Payload, "source_url"), envelope.SourceRef.SourceURI),
		Disabled:                  incidentRoutingPayloadBool(envelope.Payload, "disabled"),
		Deleted:                   incidentRoutingPayloadBool(envelope.Payload, "deleted"),
		ManuallyCreated:           incidentRoutingPayloadBool(envelope.Payload, "manually_created"),
		ObservedAt:                incidentRoutingFormatTime(envelope.ObservedAt),
	}
}

func incidentRoutingWarningFromEnvelope(envelope facts.Envelope) reducer.IncidentRoutingCoverageWarning {
	return reducer.IncidentRoutingCoverageWarning{
		FactID:           envelope.FactID,
		SourceClass:      incidentRoutingPayloadString(envelope.Payload, "source_class"),
		SourceKind:       incidentRoutingPayloadString(envelope.Payload, "source_kind"),
		Reason:           incidentRoutingPayloadString(envelope.Payload, "reason"),
		ResourceClass:    incidentRoutingPayloadString(envelope.Payload, "resource_class"),
		ProviderObjectID: incidentRoutingPayloadString(envelope.Payload, "provider_object_id"),
		ObservedAt:       incidentRoutingFormatTime(envelope.ObservedAt),
	}
}

func incidentRoutingServiceNameAllowlist(incidents []reducer.IncidentRoutingIncident) []string {
	seen := make(map[string]struct{}, len(incidents))
	for _, incident := range incidents {
		serviceName := strings.ToLower(strings.TrimSpace(incident.ServiceName))
		if serviceName == "" {
			continue
		}
		seen[serviceName] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for serviceName := range seen {
		out = append(out, serviceName)
	}
	sort.Strings(out)
	return out
}

func incidentRoutingPayloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	if typed, ok := value.(string); ok {
		return strings.TrimSpace(typed)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func incidentRoutingPayloadBool(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok || value == nil {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

func incidentRoutingPayloadMap(payload map[string]any, key string) map[string]any {
	value, ok := payload[key]
	if !ok || value == nil {
		return nil
	}
	typed, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return typed
}

func incidentRoutingFormatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
