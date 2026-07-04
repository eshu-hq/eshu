// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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

// LoadIncidentRoutingRawEvidence implements reducer.IncidentRoutingEvidenceLoader
// for PagerDuty incident-routing graph materialization. It returns the RAW
// incident-context and incident-routing fact envelopes (payloads undecoded, for
// the reducer to decode through the typed contracts seam) plus the declared
// PagerDuty routing evidence read from content_entities metadata.
//
// The declared read needs a bounded service-name allowlist, derived here by
// peeking at the incident.record facts' service summaries. That peek is only a
// query bound, never authoritative correlation truth: the reducer re-matches
// declared candidates against the DECODED incident service name, so a missing or
// malformed service summary here can only narrow the declared candidate set,
// never invent a wrong correlation. The fact payloads themselves are handed back
// undecoded so a malformed required field surfaces as a per-fact input_invalid
// dead-letter in the reducer, not a silent empty read here.
func (s FactStore) LoadIncidentRoutingRawEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) (reducer.IncidentRoutingRawEvidence, error) {
	factKinds := append([]string{facts.IncidentRecordFactKind}, facts.IncidentRoutingFactKinds()...)
	envelopes, err := s.ListFactsByKind(ctx, scopeID, generationID, factKinds)
	if err != nil {
		return reducer.IncidentRoutingRawEvidence{}, err
	}

	serviceNames := incidentRoutingServiceNameAllowlistFromEnvelopes(envelopes)
	if len(serviceNames) == 0 {
		// No incident.record anchor with a service name: no declared read. The
		// reducer builds no packets without an incident anchor, so return the
		// (possibly non-empty) envelopes without the declaration round trip.
		return reducer.IncidentRoutingRawEvidence{Facts: envelopes}, nil
	}

	declared, err := s.loadIncidentRoutingDeclaredEvidence(ctx, serviceNames)
	if err != nil {
		return reducer.IncidentRoutingRawEvidence{}, err
	}

	return reducer.IncidentRoutingRawEvidence{
		Facts:    envelopes,
		Declared: declared,
	}, nil
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

// incidentRoutingServiceNameAllowlistFromEnvelopes derives the bounded,
// lowercased service-name allowlist for the declared-evidence query by peeking at
// the incident.record facts' service summaries. This is deliberately a raw peek,
// not a typed decode: the allowlist is only a query bound (which declared rows to
// read), never authoritative correlation truth, and the reducer re-matches
// declared candidates against the DECODED incident service name. Reading a
// malformed or missing service summary here can only narrow the candidate set, so
// there is no accuracy hazard in reading it without the dead-letter apparatus.
// Tombstoned facts are excluded, matching the reducer's decode-side skip.
func incidentRoutingServiceNameAllowlistFromEnvelopes(envelopes []facts.Envelope) []string {
	seen := make(map[string]struct{})
	for _, envelope := range envelopes {
		if envelope.IsTombstone || envelope.FactKind != facts.IncidentRecordFactKind {
			continue
		}
		service := incidentRoutingPayloadMap(envelope.Payload, "service")
		serviceName := strings.ToLower(incidentRoutingPayloadString(service, "summary"))
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
