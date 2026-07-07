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
		decoded, ok := buildIncidentAppliedPagerDutyRouting(row)
		if !ok {
			continue
		}
		out = append(out, decoded)
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
		decoded, ok := buildIncidentObservedPagerDutyRouting(row)
		if !ok {
			continue
		}
		out = append(out, decoded)
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
		decoded, ok := buildIncidentRoutingCoverageWarning(row)
		if !ok {
			continue
		}
		out = append(out, decoded)
	}
	return out, nil
}

// buildIncidentAppliedPagerDutyRouting decodes one
// incident_routing.applied_pagerduty_resource fact row through the typed
// sdk/go/factschema/incident/v1 seam (decodeIncidentRoutingAppliedPagerDutyResource)
// and shapes it into the read model's incidentAppliedPagerDutyRouting. ok is
// false when the fact failed typed decode (a missing required routing field);
// the caller drops the row rather than emit an empty-identity routing entry.
func buildIncidentAppliedPagerDutyRouting(row incidentContextFactRow) (incidentAppliedPagerDutyRouting, bool) {
	resource, err := decodeIncidentRoutingAppliedPagerDutyResource(incidentContextDecodeInput{
		FactID: row.FactID, SchemaVersion: row.SchemaVersion, Payload: row.Payload,
	})
	if err != nil {
		logIncidentContextDecodeDrop(err)
		return incidentAppliedPagerDutyRouting{}, false
	}
	return incidentAppliedPagerDutyRouting{
		FactID:                    row.FactID,
		SourceClass:               resource.SourceClass,
		SourceKind:                resource.SourceKind,
		Outcome:                   resource.Outcome,
		ResourceClass:             resource.ResourceClass,
		ProviderObjectID:          workItemDerefString(resource.ProviderObjectID),
		NameFingerprint:           workItemDerefString(resource.NameFingerprint),
		EscalationPolicyReference: workItemDerefString(resource.EscalationPolicyReference),
		TerraformStateAddress:     resource.TerraformStateAddress,
		ProviderAddress:           resource.ProviderAddress,
		ModuleAddress:             resource.ModuleAddress,
		StateGenerationID:         resource.StateGenerationID,
		DeclaredMatchState:        resource.DeclaredMatchState,
		RedactionState:            resource.RedactionState,
		ObservedAt:                formatIncidentContextTime(row.ObservedAt),
	}, true
}

// buildIncidentObservedPagerDutyRouting decodes one
// incident_routing.observed_pagerduty_service fact row through the typed seam
// (decodeIncidentRoutingObservedPagerDutyService) and shapes it into the read
// model's incidentObservedPagerDutyRouting. ok is false when the fact failed
// typed decode; the caller drops the row.
func buildIncidentObservedPagerDutyRouting(row incidentContextFactRow) (incidentObservedPagerDutyRouting, bool) {
	service, err := decodeIncidentRoutingObservedPagerDutyService(incidentContextDecodeInput{
		FactID: row.FactID, SchemaVersion: row.SchemaVersion, Payload: row.Payload,
	})
	if err != nil {
		logIncidentContextDecodeDrop(err)
		return incidentObservedPagerDutyRouting{}, false
	}
	return incidentObservedPagerDutyRouting{
		FactID:                    row.FactID,
		SourceClass:               service.SourceClass,
		SourceKind:                service.SourceKind,
		Outcome:                   service.Outcome,
		ServiceID:                 service.ServiceID,
		ProviderObjectID:          service.ProviderObjectID,
		NameFingerprint:           workItemDerefString(service.NameFingerprint),
		Status:                    workItemDerefString(service.Status),
		EscalationPolicyReference: workItemDerefString(service.EscalationPolicyReference),
		DeclaredMatchState:        service.DeclaredMatchState,
		DriftCandidateReason:      workItemDerefString(service.DriftCandidateReason),
		RedactionState:            service.RedactionState,
		SourceURL:                 workItemDerefString(service.SourceURL),
		Disabled:                  workItemDerefBool(service.Disabled),
		Deleted:                   workItemDerefBool(service.Deleted),
		ManuallyCreated:           workItemDerefBool(service.ManuallyCreated),
		ObservedAt:                formatIncidentContextTime(row.ObservedAt),
	}, true
}

// buildIncidentRoutingCoverageWarning decodes one
// incident_routing.coverage_warning fact row through the typed seam
// (decodeIncidentRoutingCoverageWarning) and shapes it into the read model's
// incidentRoutingCoverageWarning. ok is false when the fact failed typed
// decode; the caller drops the row.
func buildIncidentRoutingCoverageWarning(row incidentContextFactRow) (incidentRoutingCoverageWarning, bool) {
	warning, err := decodeIncidentRoutingCoverageWarning(incidentContextDecodeInput{
		FactID: row.FactID, SchemaVersion: row.SchemaVersion, Payload: row.Payload,
	})
	if err != nil {
		logIncidentContextDecodeDrop(err)
		return incidentRoutingCoverageWarning{}, false
	}
	return incidentRoutingCoverageWarning{
		FactID:           row.FactID,
		SourceClass:      warning.SourceClass,
		SourceKind:       warning.SourceKind,
		Reason:           warning.Reason,
		ResourceClass:    workItemDerefString(warning.ResourceClass),
		ProviderObjectID: workItemDerefString(warning.ProviderObjectID),
		ObservedAt:       formatIncidentContextTime(row.ObservedAt),
	}, true
}
