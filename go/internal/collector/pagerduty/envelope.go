// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
)

// NewIncidentRecordEnvelope converts one PagerDuty incident into an
// incident.record source fact.
func NewIncidentRecordEnvelope(ctx EnvelopeContext, incident Incident) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	if strings.TrimSpace(incident.ID) == "" {
		return facts.Envelope{}, fmt.Errorf("pagerduty incident id must not be blank")
	}
	stableKey := providerStableKey(facts.IncidentRecordFactKind, ctx.ScopeID, incident.ID)
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              ProviderPagerDuty,
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
		"source_url":            safeSourceURI(firstNonBlank(incident.HTMLURL, ctx.SourceURI)),
	}
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeIncidentRecord(incidentv1.IncidentRecord{
			Provider:            ProviderPagerDuty,
			ProviderIncidentID:  strings.TrimSpace(incident.ID),
			IncidentNumber:      int64Ptr(incident.IncidentNumber),
			Title:               stringPtr(strings.TrimSpace(incident.Title)),
			Status:              stringPtr(strings.TrimSpace(incident.Status)),
			Urgency:             stringPtr(strings.TrimSpace(incident.Urgency)),
			Priority:            serviceReferencePtr(incident.Priority),
			ServiceID:           stringPtr(strings.TrimSpace(incident.Service.ID)),
			Service:             serviceReferencePtr(incident.Service),
			EscalationPolicy:    serviceReferencePtr(incident.Escalation),
			Teams:               serviceReferences(incident.Teams),
			Assignments:         serviceReferences(incident.Assignments),
			CreatedAt:           stringPtr(timeString(incident.CreatedAt)),
			UpdatedAt:           stringPtr(timeString(incident.UpdatedAt)),
			ResolvedAt:          stringPtr(timeString(incident.ResolvedAt)),
			SourceURL:           stringPtr(safeSourceURI(firstNonBlank(incident.HTMLURL, ctx.SourceURI))),
			CollectorInstanceID: stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	return envelope(ctx, facts.IncidentRecordFactKind, stableKey, payload, firstNonBlank(incident.HTMLURL, ctx.SourceURI), incident.ID), nil
}

// NewLifecycleEventEnvelope converts one PagerDuty log entry into an
// incident.lifecycle_event source fact.
func NewLifecycleEventEnvelope(ctx EnvelopeContext, event LifecycleEvent) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	if strings.TrimSpace(event.ID) == "" {
		return facts.Envelope{}, fmt.Errorf("pagerduty lifecycle event id must not be blank")
	}
	if strings.TrimSpace(event.IncidentID) == "" {
		return facts.Envelope{}, fmt.Errorf("pagerduty lifecycle event incident_id must not be blank")
	}
	stableKey := facts.StableID(facts.IncidentLifecycleEventFactKind, map[string]any{
		"provider":    ProviderPagerDuty,
		"scope_id":    ctx.ScopeID,
		"incident_id": strings.TrimSpace(event.IncidentID),
		"event_id":    strings.TrimSpace(event.ID),
	})
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              ProviderPagerDuty,
		"provider_event_id":     strings.TrimSpace(event.ID),
		"provider_incident_id":  strings.TrimSpace(event.IncidentID),
		"event_type":            strings.TrimSpace(event.Type),
		"actor":                 referencePayload(event.Actor),
		"channel":               strings.TrimSpace(event.Channel),
		"summary":               strings.TrimSpace(event.Summary),
		"created_at":            timeString(event.CreatedAt),
		"source_url":            safeSourceURI(firstNonBlank(event.HTMLURL, ctx.SourceURI)),
	}
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeIncidentLifecycleEvent(incidentv1.LifecycleEvent{
			Provider:            ProviderPagerDuty,
			ProviderEventID:     strings.TrimSpace(event.ID),
			ProviderIncidentID:  strings.TrimSpace(event.IncidentID),
			EventType:           stringPtr(strings.TrimSpace(event.Type)),
			Actor:               serviceReferencePtr(event.Actor),
			Channel:             stringPtr(strings.TrimSpace(event.Channel)),
			Summary:             stringPtr(strings.TrimSpace(event.Summary)),
			CreatedAt:           stringPtr(timeString(event.CreatedAt)),
			SourceURL:           stringPtr(safeSourceURI(firstNonBlank(event.HTMLURL, ctx.SourceURI))),
			CollectorInstanceID: stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	return envelope(ctx, facts.IncidentLifecycleEventFactKind, stableKey, payload, firstNonBlank(event.HTMLURL, ctx.SourceURI), event.ID), nil
}

// NewChangeRecordEnvelope converts one PagerDuty related change event into a
// change.record source fact.
func NewChangeRecordEnvelope(ctx EnvelopeContext, change ChangeEvent) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	if strings.TrimSpace(change.ID) == "" {
		return facts.Envelope{}, fmt.Errorf("pagerduty change event id must not be blank")
	}
	stableKey := providerStableKey(facts.ChangeRecordFactKind, ctx.ScopeID, change.ID)
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              ProviderPagerDuty,
		"provider_change_id":    strings.TrimSpace(change.ID),
		"summary":               strings.TrimSpace(change.Summary),
		"source":                strings.TrimSpace(change.Source),
		"services":              referencesPayload(change.Services),
		"links":                 linksPayload(change.Links),
		"timestamp":             timeString(change.Timestamp),
		"source_url":            safeSourceURI(firstNonBlank(change.HTMLURL, ctx.SourceURI)),
	}
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeChangeRecord(incidentv1.ChangeRecord{
			Provider:            ProviderPagerDuty,
			ProviderChangeID:    strings.TrimSpace(change.ID),
			Summary:             stringPtr(strings.TrimSpace(change.Summary)),
			Source:              stringPtr(strings.TrimSpace(change.Source)),
			Services:            serviceReferences(change.Services),
			Links:               changeLinks(change.Links),
			Timestamp:           stringPtr(timeString(change.Timestamp)),
			SourceURL:           stringPtr(safeSourceURI(firstNonBlank(change.HTMLURL, ctx.SourceURI))),
			CollectorInstanceID: stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	return envelope(ctx, facts.ChangeRecordFactKind, stableKey, payload, firstNonBlank(change.HTMLURL, ctx.SourceURI), change.ID), nil
}

func validateEnvelopeContext(ctx EnvelopeContext) error {
	if strings.TrimSpace(ctx.ScopeID) == "" {
		return fmt.Errorf("pagerduty envelope scope_id must not be blank")
	}
	if strings.TrimSpace(ctx.GenerationID) == "" {
		return fmt.Errorf("pagerduty envelope generation_id must not be blank")
	}
	if strings.TrimSpace(ctx.CollectorInstanceID) == "" {
		return fmt.Errorf("pagerduty envelope collector_instance_id must not be blank")
	}
	return nil
}

func envelope(ctx EnvelopeContext, kind string, stableKey string, payload map[string]any, sourceURI string, recordID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID(kind, stableKey, ctx.ScopeID, ctx.GenerationID),
		ScopeID:          ctx.ScopeID,
		GenerationID:     ctx.GenerationID,
		FactKind:         kind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.IncidentContextSchemaVersionV1,
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

func providerStableKey(kind string, scopeID string, providerID string) string {
	return facts.StableID(kind, map[string]any{
		"provider":    ProviderPagerDuty,
		"scope_id":    strings.TrimSpace(scopeID),
		"provider_id": strings.TrimSpace(providerID),
	})
}

func factID(kind string, stableKey string, scopeID string, generationID string) string {
	return facts.StableID("IncidentContextFact", map[string]any{
		"fact_kind":       kind,
		"generation_id":   strings.TrimSpace(generationID),
		"scope_id":        strings.TrimSpace(scopeID),
		"stable_fact_key": strings.TrimSpace(stableKey),
	})
}

func referencePayload(ref Reference) map[string]string {
	payload := map[string]string{}
	if value := strings.TrimSpace(ref.ID); value != "" {
		payload["id"] = value
	}
	if value := strings.TrimSpace(ref.Type); value != "" {
		payload["type"] = value
	}
	if value := strings.TrimSpace(ref.Summary); value != "" {
		payload["summary"] = value
	}
	if value := safeSourceURI(ref.HTMLURL); value != "" {
		payload["url"] = value
	}
	if len(payload) == 0 {
		return nil
	}
	return payload
}

func referencesPayload(refs []Reference) []map[string]string {
	out := make([]map[string]string, 0, len(refs))
	for _, ref := range refs {
		if payload := referencePayload(ref); len(payload) > 0 {
			out = append(out, payload)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func linksPayload(links []Link) []map[string]string {
	out := make([]map[string]string, 0, len(links))
	for _, link := range links {
		href := safeSourceURI(link.Href)
		text := strings.TrimSpace(link.Text)
		if href == "" && text == "" {
			continue
		}
		out = append(out, map[string]string{"href": href, "text": text})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizedObservedAt(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func timeString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
