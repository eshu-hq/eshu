// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
)

// decodeIncidentContextIncident decodes one incident.record fact row into the
// read model's IncidentContextIncident through the typed
// sdk/go/factschema/incident/v1 seam (decodeIncidentRecord). ok is false when
// the fact failed decode — a payload missing BOTH provider_incident_id and a
// usable source_record_id, or an unsupported schema major — and the caller
// must drop the row rather than emit an empty-identity incident.
func decodeIncidentContextIncident(row incidentContextFactRow) (model.IncidentContextIncident, bool) {
	record, err := decodeIncidentRecord(incidentContextDecodeInput{
		FactID:         row.FactID,
		SourceRecordID: row.SourceRecordID,
		SchemaVersion:  row.SchemaVersion,
		Payload:        row.Payload,
	})
	if err != nil {
		logIncidentContextDecodeDrop(err)
		return model.IncidentContextIncident{}, false
	}
	return model.IncidentContextIncident{
		Provider:           querycontract.FirstNonEmpty(record.Provider, model.ProviderPagerDuty),
		ProviderIncidentID: record.ProviderIncidentID,
		ScopeID:            row.ScopeID,
		IncidentNumber:     incidentDerefInt64(record.IncidentNumber),
		Title:              workItemDerefString(record.Title),
		Status:             workItemDerefString(record.Status),
		Urgency:            workItemDerefString(record.Urgency),
		Priority:           incidentContextServiceReference(record.Priority),
		Service:            incidentContextServiceReference(record.Service),
		EscalationPolicy:   incidentContextServiceReference(record.EscalationPolicy),
		Teams:              incidentContextServiceReferences(record.Teams),
		Assignments:        incidentContextServiceReferences(record.Assignments),
		CreatedAt:          workItemDerefString(record.CreatedAt),
		UpdatedAt:          workItemDerefString(record.UpdatedAt),
		ResolvedAt:         workItemDerefString(record.ResolvedAt),
		SourceURL:          querycontract.FirstNonEmpty(workItemDerefString(record.SourceURL), row.SourceURI),
		EvidenceFactID:     row.FactID,
		SourceConfidence:   row.SourceConfidence,
		ObservedAt:         formatIncidentContextTime(row.ObservedAt),
	}, true
}

// decodeIncidentContextTimelineEvent decodes one incident.lifecycle_event fact
// row through the typed seam (decodeIncidentLifecycleEvent). ok is false when
// the fact failed decode; the caller drops the event rather than emit an
// empty-identity timeline entry.
func decodeIncidentContextTimelineEvent(
	row incidentContextFactRow,
) (model.IncidentContextTimelineEvent, bool) {
	event, err := decodeIncidentLifecycleEvent(incidentContextDecodeInput{
		FactID:         row.FactID,
		SourceRecordID: row.SourceRecordID,
		SchemaVersion:  row.SchemaVersion,
		Payload:        row.Payload,
	})
	if err != nil {
		logIncidentContextDecodeDrop(err)
		return model.IncidentContextTimelineEvent{}, false
	}
	return model.IncidentContextTimelineEvent{
		EventID:          event.ProviderEventID,
		EventType:        workItemDerefString(event.EventType),
		Actor:            incidentContextServiceReference(event.Actor),
		Channel:          workItemDerefString(event.Channel),
		Summary:          workItemDerefString(event.Summary),
		CreatedAt:        workItemDerefString(event.CreatedAt),
		SourceURL:        querycontract.FirstNonEmpty(workItemDerefString(event.SourceURL), row.SourceURI),
		EvidenceFactID:   row.FactID,
		SourceConfidence: row.SourceConfidence,
		ObservedAt:       formatIncidentContextTime(row.ObservedAt),
	}, true
}

// decodeIncidentContextChangeCandidate decodes one change.record fact row
// through the typed seam (decodeChangeRecord). ok is false when the fact
// failed decode; the caller drops the candidate rather than emit an
// empty-identity change.
func decodeIncidentContextChangeCandidate(
	row incidentContextFactRow,
) (model.IncidentContextChangeCandidate, bool) {
	record, err := decodeChangeRecord(incidentContextDecodeInput{
		FactID:         row.FactID,
		SourceRecordID: row.SourceRecordID,
		SchemaVersion:  row.SchemaVersion,
		Payload:        row.Payload,
	})
	if err != nil {
		logIncidentContextDecodeDrop(err)
		return model.IncidentContextChangeCandidate{}, false
	}
	return model.IncidentContextChangeCandidate{
		ChangeID:         record.ProviderChangeID,
		Summary:          workItemDerefString(record.Summary),
		Source:           workItemDerefString(record.Source),
		Services:         incidentContextServiceReferences(record.Services),
		Links:            incidentContextChangeLinks(record.Links),
		Timestamp:        workItemDerefString(record.Timestamp),
		SourceURL:        querycontract.FirstNonEmpty(workItemDerefString(record.SourceURL), row.SourceURI),
		TruthLabel:       model.IncidentTruthFallback,
		Explanation:      "candidate matched PagerDuty service and incident time window",
		EvidenceFactID:   row.FactID,
		SourceConfidence: row.SourceConfidence,
		ObservedAt:       formatIncidentContextTime(row.ObservedAt),
	}, true
}

// incidentContextCandidates builds the ambiguous-incident candidate list for
// every row that decodes successfully. A row that fails typed decode is
// dropped from the candidate list rather than shown with an empty identity.
func incidentContextCandidates(
	rows []incidentContextFactRow,
) []model.IncidentContextIncidentCandidate {
	out := make([]model.IncidentContextIncidentCandidate, 0, len(rows))
	for _, row := range rows {
		incident, ok := decodeIncidentContextIncident(row)
		if !ok {
			continue
		}
		out = append(out, model.IncidentContextIncidentCandidate{
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

// incidentContextServiceReference adapts a typed incidentv1.ServiceReference
// pointer (nil when the emitter observed no reference) into the read model's
// IncidentContextReference, matching the pre-typing incidentContextReference
// helper's zero-value-on-absence behavior.
func incidentContextServiceReference(ref *incidentv1.ServiceReference) model.IncidentContextReference {
	if ref == nil {
		return model.IncidentContextReference{}
	}
	return model.IncidentContextReference{
		ID:      workItemDerefString(ref.ID),
		Type:    workItemDerefString(ref.Type),
		Summary: workItemDerefString(ref.Summary),
		URL:     workItemDerefString(ref.URL),
	}
}

// incidentContextServiceReferences adapts a typed []incidentv1.ServiceReference
// slice into the read model's []IncidentContextReference, matching the
// pre-typing incidentContextReferences helper's nil-on-empty behavior.
func incidentContextServiceReferences(refs []incidentv1.ServiceReference) []model.IncidentContextReference {
	if len(refs) == 0 {
		return nil
	}
	out := make([]model.IncidentContextReference, 0, len(refs))
	for _, ref := range refs {
		out = append(out, model.IncidentContextReference{
			ID:      workItemDerefString(ref.ID),
			Type:    workItemDerefString(ref.Type),
			Summary: workItemDerefString(ref.Summary),
			URL:     workItemDerefString(ref.URL),
		})
	}
	return out
}

// incidentContextChangeLinks adapts a typed []incidentv1.ChangeLink slice into
// the read model's []IncidentContextLink, matching the pre-typing
// incidentContextLinks helper's nil-on-empty behavior.
func incidentContextChangeLinks(links []incidentv1.ChangeLink) []model.IncidentContextLink {
	if len(links) == 0 {
		return nil
	}
	out := make([]model.IncidentContextLink, 0, len(links))
	for _, link := range links {
		out = append(out, model.IncidentContextLink{
			Href: workItemDerefString(link.Href),
			Text: workItemDerefString(link.Text),
		})
	}
	return out
}

// incidentDerefInt64 returns the value an *int64 points at, or 0 when it is
// nil, matching the pre-typing incidentContextInt64(payload["incident_number"])
// behavior for an absent or non-numeric field.
func incidentDerefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func formatIncidentContextTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
