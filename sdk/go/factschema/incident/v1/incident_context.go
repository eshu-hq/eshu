// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// IncidentRecord is the schema-version-1 typed payload for the
// "incident.record" fact kind: one provider-reported operational incident.
//
// The required set matches the collector emitter
// (pagerduty.NewIncidentRecordEnvelope), which rejects a blank incident id and
// always stamps the provider token. Provider and ProviderIncidentID are the
// durable identity the reducer and the incident-context read model anchor on:
// ProviderIncidentID keys the incident node, so its silent absence would
// produce an empty-string graph identity — the exact accuracy hole Contract
// System v1 exists to close. Making it required means a provider that drops the
// incident id dead-letters as input_invalid instead. The emitter's
// SourceRecordID fallback for a blank id is dead code the reducer no longer
// needs once the field is required.
//
// Every other field is optional: the emitter builds Service, Priority,
// Escalation, Teams, and Assignments through referencePayload/referencesPayload,
// which return nil for an empty reference, so a required non-pointer would
// dead-letter every incident with no service, priority, or team. ServiceID is
// emitted but may be the empty string for an unserviced incident, so it is an
// optional pointer too.
type IncidentRecord struct {
	// Provider is the incident provider token (for example "pagerduty").
	// Required — the emitter always stamps it.
	Provider string `json:"provider"`

	// ProviderIncidentID is the provider-assigned incident id. Required — it is
	// the durable identity the incident node is keyed on, and the emitter
	// rejects a blank id.
	ProviderIncidentID string `json:"provider_incident_id"`

	// IncidentNumber is the provider's human-facing incident number. Optional:
	// a display property, not identity.
	IncidentNumber *int64 `json:"incident_number,omitempty"`

	// Title is the incident title. Optional: an observable property.
	Title *string `json:"title,omitempty"`

	// Status is the provider-reported incident status. Optional.
	Status *string `json:"status,omitempty"`

	// Urgency is the provider-reported incident urgency. Optional.
	Urgency *string `json:"urgency,omitempty"`

	// ServiceID is the affected service's provider id. Optional: the emitter
	// always emits the key but it may be the empty string for an unserviced
	// incident, and the incident-context read path falls back to Service.ID.
	ServiceID *string `json:"service_id,omitempty"`

	// Service is the affected service reference. Optional: the emitter emits nil
	// when the incident carries no service reference. The reducer reads
	// Service.ID, Service.Summary, and Service.URL for routing correlation.
	Service *ServiceReference `json:"service,omitempty"`

	// Priority is the incident priority reference. Optional.
	Priority *ServiceReference `json:"priority,omitempty"`

	// EscalationPolicy is the incident escalation-policy reference. Optional.
	EscalationPolicy *ServiceReference `json:"escalation_policy,omitempty"`

	// Teams are the responder team references. Optional.
	Teams []ServiceReference `json:"teams,omitempty"`

	// Assignments are the incident assignee references. Optional.
	Assignments []ServiceReference `json:"assignments,omitempty"`

	// CreatedAt is the incident creation timestamp (RFC 3339). Optional.
	CreatedAt *string `json:"created_at,omitempty"`

	// UpdatedAt is the incident last-update timestamp (RFC 3339). Optional.
	UpdatedAt *string `json:"updated_at,omitempty"`

	// ResolvedAt is the incident resolution timestamp (RFC 3339). Optional.
	ResolvedAt *string `json:"resolved_at,omitempty"`

	// SourceURL is the provider's incident URL. Optional: the reducer falls
	// back to the envelope SourceRef.SourceURI when this is absent, so an
	// absent value is a valid state.
	SourceURL *string `json:"source_url,omitempty"`

	// CollectorInstanceID is the collector boundary token the emitter stamps on
	// every payload. Optional: boundary metadata, not graph identity, carried
	// for parity with the emitted payload.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}

// ServiceReference is one provider entity reference (a service, priority,
// escalation policy, team, or assignee) as the incident-context emitter shapes
// it through referencePayload. Every field is optional: referencePayload emits
// only the non-empty keys, so a reference may carry any subset of id, type,
// summary, and url.
type ServiceReference struct {
	// ID is the referenced entity's provider id. Optional.
	ID *string `json:"id,omitempty"`

	// Type is the referenced entity's provider type token. Optional.
	Type *string `json:"type,omitempty"`

	// Summary is the referenced entity's human summary. Optional.
	Summary *string `json:"summary,omitempty"`

	// URL is the referenced entity's provider URL. Optional.
	URL *string `json:"url,omitempty"`
}

// LifecycleEvent is the schema-version-1 typed payload for the
// "incident.lifecycle_event" fact kind: one provider-reported incident timeline
// or log event.
//
// The required set matches the collector emitter
// (pagerduty.NewLifecycleEventEnvelope), which rejects a blank event id and a
// blank incident id and always stamps the provider token. Provider,
// ProviderEventID, and ProviderIncidentID are the durable identity. Every other
// field is optional: the emitter builds Actor through referencePayload (nil for
// an empty reference), and EventType, Channel, and Summary are observable
// properties emitted from possibly-empty source fields.
type LifecycleEvent struct {
	// Provider is the incident provider token. Required.
	Provider string `json:"provider"`

	// ProviderEventID is the provider-assigned log-entry id. Required — the
	// emitter rejects a blank event id.
	ProviderEventID string `json:"provider_event_id"`

	// ProviderIncidentID is the incident the event belongs to. Required — the
	// emitter rejects a blank incident id.
	ProviderIncidentID string `json:"provider_incident_id"`

	// EventType is the provider log-entry type. Optional.
	EventType *string `json:"event_type,omitempty"`

	// Actor is the entity that produced the event. Optional: nil when the
	// emitter observed no actor reference.
	Actor *ServiceReference `json:"actor,omitempty"`

	// Channel is the provider event channel token. Optional.
	Channel *string `json:"channel,omitempty"`

	// Summary is the event summary. Optional.
	Summary *string `json:"summary,omitempty"`

	// CreatedAt is the event timestamp (RFC 3339). Optional.
	CreatedAt *string `json:"created_at,omitempty"`

	// SourceURL is the provider's event URL. Optional.
	SourceURL *string `json:"source_url,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}

// ChangeRecord is the schema-version-1 typed payload for the "change.record"
// fact kind: one provider-reported operational change event related to an
// incident or service.
//
// The required set matches the collector emitter
// (pagerduty.NewChangeRecordEnvelope), which rejects a blank change id and
// always stamps the provider token. Provider and ProviderChangeID are the
// durable identity. Services and Links are built through referencesPayload/
// linksPayload (nil for an empty list), so they are optional; Summary, Source,
// and Timestamp are observable properties emitted from possibly-empty source
// fields.
type ChangeRecord struct {
	// Provider is the incident provider token. Required.
	Provider string `json:"provider"`

	// ProviderChangeID is the provider-assigned change-event id. Required — the
	// emitter rejects a blank change id.
	ProviderChangeID string `json:"provider_change_id"`

	// Summary is the change-event summary. Optional.
	Summary *string `json:"summary,omitempty"`

	// Source is the change-event source token. Optional.
	Source *string `json:"source,omitempty"`

	// Services are the services the change affected. Optional.
	Services []ServiceReference `json:"services,omitempty"`

	// Links are the change-event links (each carrying an href and text).
	// Optional.
	Links []ChangeLink `json:"links,omitempty"`

	// Timestamp is the change-event timestamp (RFC 3339). Optional.
	Timestamp *string `json:"timestamp,omitempty"`

	// SourceURL is the provider's change-event URL. Optional.
	SourceURL *string `json:"source_url,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}

// ChangeLink is one change-event link as the emitter shapes it through
// linksPayload: an href and a text label, either of which may be empty (the
// emitter drops a link only when both are empty). Both fields are optional.
type ChangeLink struct {
	// Href is the link target. Optional.
	Href *string `json:"href,omitempty"`

	// Text is the link display text. Optional.
	Text *string `json:"text,omitempty"`
}
