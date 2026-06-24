// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "time"

func decodeIncidentContextIncident(row incidentContextFactRow) IncidentContextIncident {
	payload := row.Payload
	return IncidentContextIncident{
		Provider:           firstNonEmpty(StringVal(payload, "provider"), incidentContextProviderPagerDuty),
		ProviderIncidentID: firstNonEmpty(StringVal(payload, "provider_incident_id"), row.SourceRecordID),
		ScopeID:            row.ScopeID,
		IncidentNumber:     incidentContextInt64(payload["incident_number"]),
		Title:              StringVal(payload, "title"),
		Status:             StringVal(payload, "status"),
		Urgency:            StringVal(payload, "urgency"),
		Priority:           incidentContextReference(payload, "priority"),
		Service:            incidentContextReference(payload, "service"),
		EscalationPolicy:   incidentContextReference(payload, "escalation_policy"),
		Teams:              incidentContextReferences(payload, "teams"),
		Assignments:        incidentContextReferences(payload, "assignments"),
		CreatedAt:          StringVal(payload, "created_at"),
		UpdatedAt:          StringVal(payload, "updated_at"),
		ResolvedAt:         StringVal(payload, "resolved_at"),
		SourceURL:          firstNonEmpty(StringVal(payload, "source_url"), row.SourceURI),
		EvidenceFactID:     row.FactID,
		SourceConfidence:   row.SourceConfidence,
		ObservedAt:         formatIncidentContextTime(row.ObservedAt),
	}
}

func decodeIncidentContextTimelineEvent(
	row incidentContextFactRow,
) IncidentContextTimelineEvent {
	payload := row.Payload
	return IncidentContextTimelineEvent{
		EventID:          firstNonEmpty(StringVal(payload, "provider_event_id"), row.SourceRecordID),
		EventType:        StringVal(payload, "event_type"),
		Actor:            incidentContextReference(payload, "actor"),
		Channel:          StringVal(payload, "channel"),
		Summary:          StringVal(payload, "summary"),
		CreatedAt:        StringVal(payload, "created_at"),
		SourceURL:        firstNonEmpty(StringVal(payload, "source_url"), row.SourceURI),
		EvidenceFactID:   row.FactID,
		SourceConfidence: row.SourceConfidence,
		ObservedAt:       formatIncidentContextTime(row.ObservedAt),
	}
}

func decodeIncidentContextChangeCandidate(
	row incidentContextFactRow,
) IncidentContextChangeCandidate {
	payload := row.Payload
	return IncidentContextChangeCandidate{
		ChangeID:         firstNonEmpty(StringVal(payload, "provider_change_id"), row.SourceRecordID),
		Summary:          StringVal(payload, "summary"),
		Source:           StringVal(payload, "source"),
		Services:         incidentContextReferences(payload, "services"),
		Links:            incidentContextLinks(payload, "links"),
		Timestamp:        StringVal(payload, "timestamp"),
		SourceURL:        firstNonEmpty(StringVal(payload, "source_url"), row.SourceURI),
		TruthLabel:       IncidentTruthFallback,
		Explanation:      "candidate matched PagerDuty service and incident time window",
		EvidenceFactID:   row.FactID,
		SourceConfidence: row.SourceConfidence,
		ObservedAt:       formatIncidentContextTime(row.ObservedAt),
	}
}

func incidentContextCandidates(
	rows []incidentContextFactRow,
) []IncidentContextIncidentCandidate {
	out := make([]IncidentContextIncidentCandidate, 0, len(rows))
	for _, row := range rows {
		incident := decodeIncidentContextIncident(row)
		out = append(out, IncidentContextIncidentCandidate{
			Provider:           incident.Provider,
			ProviderIncidentID: incident.ProviderIncidentID,
			ScopeID:            row.ScopeID,
			ServiceID:          incident.Service.ID,
			ServiceName:        incident.Service.Summary,
			SourceURL:          incident.SourceURL,
			EvidenceFactID:     row.FactID,
		})
	}
	return out
}

func incidentContextReference(
	payload map[string]any,
	key string,
) IncidentContextReference {
	raw, ok := payload[key].(map[string]any)
	if !ok || len(raw) == 0 {
		return IncidentContextReference{}
	}
	return IncidentContextReference{
		ID:      StringVal(raw, "id"),
		Type:    StringVal(raw, "type"),
		Summary: StringVal(raw, "summary"),
		URL:     StringVal(raw, "url"),
	}
}

func incidentContextReferences(
	payload map[string]any,
	key string,
) []IncidentContextReference {
	raw, ok := payload[key].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([]IncidentContextReference, 0, len(raw))
	for _, item := range raw {
		ref, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, IncidentContextReference{
			ID:      StringVal(ref, "id"),
			Type:    StringVal(ref, "type"),
			Summary: StringVal(ref, "summary"),
			URL:     StringVal(ref, "url"),
		})
	}
	return out
}

func incidentContextLinks(payload map[string]any, key string) []IncidentContextLink {
	raw, ok := payload[key].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([]IncidentContextLink, 0, len(raw))
	for _, item := range raw {
		link, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, IncidentContextLink{
			Href: StringVal(link, "href"),
			Text: StringVal(link, "text"),
		})
	}
	return out
}

func incidentContextInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func formatIncidentContextTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
