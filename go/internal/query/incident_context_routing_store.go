// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (s PostgresIncidentContextStore) readIncidentRoutingEvidence(
	ctx context.Context,
	incident IncidentContextIncident,
) ([]IncidentContextEvidenceEdge, error) {
	if strings.TrimSpace(incident.Service.ID) == "" && strings.TrimSpace(incident.Service.Summary) == "" {
		return nil, nil
	}
	declared, err := s.readIncidentDeclaredPagerDutyRouting(ctx, incident)
	if err != nil {
		return nil, err
	}
	applied, err := s.readIncidentAppliedPagerDutyRouting(ctx, incident)
	if err != nil {
		return nil, err
	}
	observed, err := s.readIncidentObservedPagerDutyRouting(ctx, incident)
	if err != nil {
		return nil, err
	}
	warnings, err := s.readIncidentRoutingCoverageWarnings(ctx, incident)
	if err != nil {
		return nil, err
	}
	return buildIncidentRoutingEvidence(incidentRoutingEvidenceInput{
		Incident: incident,
		Declared: declared,
		Applied:  applied,
		Observed: observed,
		Warnings: warnings,
	}), nil
}

func (s PostgresIncidentContextStore) readIncidentDeclaredPagerDutyRouting(
	ctx context.Context,
	incident IncidentContextIncident,
) ([]incidentDeclaredPagerDutyRouting, error) {
	serviceName := strings.TrimSpace(incident.Service.Summary)
	if serviceName == "" {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(
		ctx,
		listIncidentDeclaredPagerDutyRoutingQuery,
		serviceName,
		incidentRuntimeEvidenceLimit+1,
	)
	if err != nil {
		return nil, fmt.Errorf("list incident declared pagerduty routing: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]incidentDeclaredPagerDutyRouting, 0)
	for rows.Next() {
		var item incidentDeclaredPagerDutyRouting
		var metadataBytes []byte
		var endLine int
		if err := rows.Scan(
			&item.EntityID,
			&item.RepoID,
			&item.RelativePath,
			&item.EntityName,
			&item.StartLine,
			&endLine,
			&metadataBytes,
		); err != nil {
			return nil, fmt.Errorf("scan declared pagerduty routing: %w", err)
		}
		var metadata map[string]any
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			return nil, fmt.Errorf("decode declared pagerduty routing metadata: %w", err)
		}
		item.DeclarationKind = StringVal(metadata, "declaration_kind")
		item.SourceClass = StringVal(metadata, "source_class")
		item.Outcome = StringVal(metadata, "outcome")
		item.ServiceName = StringVal(metadata, "service_name")
		item.ServiceNameResolution = StringVal(metadata, "service_name_resolution")
		item.EscalationPolicy = StringVal(metadata, "escalation_policy")
		item.Environment = StringVal(metadata, "environment")
		item.Workspace = StringVal(metadata, "workspace")
		item.RedactionState = StringVal(metadata, "redaction_state")
		item.UnsupportedReason = StringVal(metadata, "unsupported_reason")
		item.DuplicateServiceName = BoolVal(metadata, "duplicate_service_name")
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan declared pagerduty routing rows: %w", err)
	}
	return out, nil
}

func (s PostgresIncidentContextStore) readIncidentAppliedPagerDutyRouting(
	ctx context.Context,
	incident IncidentContextIncident,
) ([]incidentAppliedPagerDutyRouting, error) {
	serviceID := strings.TrimSpace(incident.Service.ID)
	serviceFingerprint := incidentRoutingShortFingerprint(incident.Service.Summary)
	if serviceID == "" && serviceFingerprint == "" {
		return nil, nil
	}
	rows, err := s.queryIncidentContextRows(
		ctx,
		listIncidentAppliedPagerDutyRoutingQuery,
		serviceID,
		serviceFingerprint,
		incidentRuntimeEvidenceLimit+1,
	)
	if err != nil {
		return nil, fmt.Errorf("list incident applied pagerduty routing: %w", err)
	}
	out := make([]incidentAppliedPagerDutyRouting, 0, len(rows))
	for _, row := range rows {
		out = append(out, decodeIncidentAppliedPagerDutyRouting(row))
	}
	return out, nil
}

func (s PostgresIncidentContextStore) readIncidentObservedPagerDutyRouting(
	ctx context.Context,
	incident IncidentContextIncident,
) ([]incidentObservedPagerDutyRouting, error) {
	serviceID := strings.TrimSpace(incident.Service.ID)
	serviceFingerprint := incidentRoutingConfigFingerprint(incident.Service.Summary)
	if serviceID == "" && serviceFingerprint == "" {
		return nil, nil
	}
	rows, err := s.queryIncidentContextRows(
		ctx,
		listIncidentObservedPagerDutyRoutingQuery,
		serviceID,
		serviceFingerprint,
		incidentRuntimeEvidenceLimit+1,
	)
	if err != nil {
		return nil, fmt.Errorf("list incident observed pagerduty routing: %w", err)
	}
	out := make([]incidentObservedPagerDutyRouting, 0, len(rows))
	for _, row := range rows {
		out = append(out, decodeIncidentObservedPagerDutyRouting(row))
	}
	return out, nil
}

func (s PostgresIncidentContextStore) readIncidentRoutingCoverageWarnings(
	ctx context.Context,
	incident IncidentContextIncident,
) ([]incidentRoutingCoverageWarning, error) {
	if strings.TrimSpace(incident.ScopeID) == "" {
		return nil, nil
	}
	rows, err := s.queryIncidentContextRows(
		ctx,
		listIncidentRoutingCoverageWarningsQuery,
		incident.ScopeID,
		incident.Service.ID,
		incidentRuntimeEvidenceLimit+1,
	)
	if err != nil {
		return nil, fmt.Errorf("list incident routing coverage warnings: %w", err)
	}
	out := make([]incidentRoutingCoverageWarning, 0, len(rows))
	for _, row := range rows {
		out = append(out, decodeIncidentRoutingCoverageWarning(row))
	}
	return out, nil
}

func decodeIncidentAppliedPagerDutyRouting(row incidentContextFactRow) incidentAppliedPagerDutyRouting {
	return incidentAppliedPagerDutyRouting{
		FactID:                    row.FactID,
		SourceClass:               StringVal(row.Payload, "source_class"),
		SourceKind:                StringVal(row.Payload, "source_kind"),
		Outcome:                   StringVal(row.Payload, "outcome"),
		ResourceClass:             StringVal(row.Payload, "resource_class"),
		ProviderObjectID:          StringVal(row.Payload, "provider_object_id"),
		NameFingerprint:           StringVal(row.Payload, "name_fingerprint"),
		EscalationPolicyReference: StringVal(row.Payload, "escalation_policy_reference"),
		TerraformStateAddress:     StringVal(row.Payload, "terraform_state_address"),
		ProviderAddress:           StringVal(row.Payload, "provider_address"),
		ModuleAddress:             StringVal(row.Payload, "module_address"),
		StateGenerationID:         StringVal(row.Payload, "state_generation_id"),
		DeclaredMatchState:        StringVal(row.Payload, "declared_match_state"),
		RedactionState:            StringVal(row.Payload, "redaction_state"),
		ObservedAt:                formatIncidentContextTime(row.ObservedAt),
	}
}

func decodeIncidentObservedPagerDutyRouting(row incidentContextFactRow) incidentObservedPagerDutyRouting {
	return incidentObservedPagerDutyRouting{
		FactID:                    row.FactID,
		SourceClass:               StringVal(row.Payload, "source_class"),
		SourceKind:                StringVal(row.Payload, "source_kind"),
		Outcome:                   StringVal(row.Payload, "outcome"),
		ServiceID:                 StringVal(row.Payload, "service_id"),
		ProviderObjectID:          StringVal(row.Payload, "provider_object_id"),
		NameFingerprint:           StringVal(row.Payload, "name_fingerprint"),
		Status:                    StringVal(row.Payload, "status"),
		EscalationPolicyReference: StringVal(row.Payload, "escalation_policy_reference"),
		DeclaredMatchState:        StringVal(row.Payload, "declared_match_state"),
		DriftCandidateReason:      StringVal(row.Payload, "drift_candidate_reason"),
		RedactionState:            StringVal(row.Payload, "redaction_state"),
		SourceURL:                 StringVal(row.Payload, "source_url"),
		Disabled:                  BoolVal(row.Payload, "disabled"),
		Deleted:                   BoolVal(row.Payload, "deleted"),
		ManuallyCreated:           BoolVal(row.Payload, "manually_created"),
		ObservedAt:                formatIncidentContextTime(row.ObservedAt),
	}
}

func decodeIncidentRoutingCoverageWarning(row incidentContextFactRow) incidentRoutingCoverageWarning {
	return incidentRoutingCoverageWarning{
		FactID:           row.FactID,
		SourceClass:      StringVal(row.Payload, "source_class"),
		SourceKind:       StringVal(row.Payload, "source_kind"),
		Reason:           StringVal(row.Payload, "reason"),
		ResourceClass:    StringVal(row.Payload, "resource_class"),
		ProviderObjectID: StringVal(row.Payload, "provider_object_id"),
		ObservedAt:       formatIncidentContextTime(row.ObservedAt),
	}
}
