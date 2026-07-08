// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
)

// DecodeIncidentRecord decodes env.Payload into the latest
// incidentv1.IncidentRecord struct for the "incident.record" fact kind,
// dispatching on env.SchemaVersion major per Contract System v1 §3.2. Callers
// (reducer handlers) receive either the decoded struct or a classified
// *DecodeError; they must never substitute a zero-value struct on error. A
// payload missing the required provider or provider_incident_id key dead-letters
// as input_invalid rather than producing an empty-string incident identity.
func DecodeIncidentRecord(env Envelope) (incidentv1.IncidentRecord, error) {
	return decodeLatestMajor[incidentv1.IncidentRecord](FactKindIncidentRecord, env)
}

// EncodeIncidentRecord marshals an incidentv1.IncidentRecord into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeIncidentRecord for schema-version-1 payloads, used by collectors
// emitting this fact kind and by this module's round-trip tests.
func EncodeIncidentRecord(record incidentv1.IncidentRecord) (map[string]any, error) {
	return encodeDirectPayload(record)
}

// DecodeIncidentLifecycleEvent decodes env.Payload into the latest
// incidentv1.LifecycleEvent struct for the "incident.lifecycle_event" fact kind.
// See DecodeIncidentRecord for the dispatch and error contract.
func DecodeIncidentLifecycleEvent(env Envelope) (incidentv1.LifecycleEvent, error) {
	return decodeLatestMajor[incidentv1.LifecycleEvent](FactKindIncidentLifecycleEvent, env)
}

// EncodeIncidentLifecycleEvent marshals an incidentv1.LifecycleEvent into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeIncidentLifecycleEvent for schema-version-1 payloads.
func EncodeIncidentLifecycleEvent(event incidentv1.LifecycleEvent) (map[string]any, error) {
	return encodeDirectPayload(event)
}

// DecodeChangeRecord decodes env.Payload into the latest incidentv1.ChangeRecord
// struct for the "change.record" fact kind. See DecodeIncidentRecord for the
// dispatch and error contract.
func DecodeChangeRecord(env Envelope) (incidentv1.ChangeRecord, error) {
	return decodeLatestMajor[incidentv1.ChangeRecord](FactKindChangeRecord, env)
}

// EncodeChangeRecord marshals an incidentv1.ChangeRecord into the map[string]any
// payload shape an Envelope carries. It is the inverse of DecodeChangeRecord for
// schema-version-1 payloads.
func EncodeChangeRecord(record incidentv1.ChangeRecord) (map[string]any, error) {
	return encodeDirectPayload(record)
}

// DecodeIncidentRoutingAppliedPagerDutyResource decodes env.Payload into the
// latest incidentv1.AppliedPagerDutyResource struct for the
// "incident_routing.applied_pagerduty_resource" fact kind. See
// DecodeIncidentRecord for the dispatch and error contract. A payload missing a
// required routing/locator field (resource_class, backend_kind, locator_hash,
// state_generation_id, source_class, source_kind, or outcome) dead-letters as
// input_invalid.
func DecodeIncidentRoutingAppliedPagerDutyResource(env Envelope) (incidentv1.AppliedPagerDutyResource, error) {
	return decodeLatestMajor[incidentv1.AppliedPagerDutyResource](FactKindIncidentRoutingAppliedPagerDutyResource, env)
}

// EncodeIncidentRoutingAppliedPagerDutyResource marshals an
// incidentv1.AppliedPagerDutyResource into the map[string]any payload shape an
// Envelope carries. It is the inverse of
// DecodeIncidentRoutingAppliedPagerDutyResource for schema-version-1 payloads.
func EncodeIncidentRoutingAppliedPagerDutyResource(resource incidentv1.AppliedPagerDutyResource) (map[string]any, error) {
	return encodeDirectPayload(resource)
}

// DecodeIncidentRoutingAppliedAlertRoute decodes env.Payload into the latest
// incidentv1.AppliedAlertRoute struct for the
// "incident_routing.applied_alert_route" fact kind. See DecodeIncidentRecord for
// the dispatch and error contract.
func DecodeIncidentRoutingAppliedAlertRoute(env Envelope) (incidentv1.AppliedAlertRoute, error) {
	return decodeLatestMajor[incidentv1.AppliedAlertRoute](FactKindIncidentRoutingAppliedAlertRoute, env)
}

// EncodeIncidentRoutingAppliedAlertRoute marshals an incidentv1.AppliedAlertRoute
// into the map[string]any payload shape an Envelope carries. It is the inverse
// of DecodeIncidentRoutingAppliedAlertRoute for schema-version-1 payloads.
func EncodeIncidentRoutingAppliedAlertRoute(route incidentv1.AppliedAlertRoute) (map[string]any, error) {
	return encodeDirectPayload(route)
}

// DecodeIncidentRoutingObservedPagerDutyService decodes env.Payload into the
// latest incidentv1.ObservedPagerDutyService struct for the
// "incident_routing.observed_pagerduty_service" fact kind. See
// DecodeIncidentRecord for the dispatch and error contract.
func DecodeIncidentRoutingObservedPagerDutyService(env Envelope) (incidentv1.ObservedPagerDutyService, error) {
	return decodeLatestMajor[incidentv1.ObservedPagerDutyService](FactKindIncidentRoutingObservedPagerDutyService, env)
}

// EncodeIncidentRoutingObservedPagerDutyService marshals an
// incidentv1.ObservedPagerDutyService into the map[string]any payload shape an
// Envelope carries. It is the inverse of
// DecodeIncidentRoutingObservedPagerDutyService for schema-version-1 payloads.
func EncodeIncidentRoutingObservedPagerDutyService(service incidentv1.ObservedPagerDutyService) (map[string]any, error) {
	return encodeDirectPayload(service)
}

// DecodeIncidentRoutingObservedPagerDutyIntegration decodes env.Payload into the
// latest incidentv1.ObservedPagerDutyIntegration struct for the
// "incident_routing.observed_pagerduty_integration" fact kind. See
// DecodeIncidentRecord for the dispatch and error contract.
func DecodeIncidentRoutingObservedPagerDutyIntegration(env Envelope) (incidentv1.ObservedPagerDutyIntegration, error) {
	return decodeLatestMajor[incidentv1.ObservedPagerDutyIntegration](FactKindIncidentRoutingObservedPagerDutyIntegration, env)
}

// EncodeIncidentRoutingObservedPagerDutyIntegration marshals an
// incidentv1.ObservedPagerDutyIntegration into the map[string]any payload shape
// an Envelope carries. It is the inverse of
// DecodeIncidentRoutingObservedPagerDutyIntegration for schema-version-1
// payloads.
func EncodeIncidentRoutingObservedPagerDutyIntegration(integration incidentv1.ObservedPagerDutyIntegration) (map[string]any, error) {
	return encodeDirectPayload(integration)
}

// DecodeIncidentRoutingCoverageWarning decodes env.Payload into the latest
// incidentv1.CoverageWarning struct for the
// "incident_routing.coverage_warning" fact kind. See DecodeIncidentRecord for
// the dispatch and error contract.
func DecodeIncidentRoutingCoverageWarning(env Envelope) (incidentv1.CoverageWarning, error) {
	return decodeLatestMajor[incidentv1.CoverageWarning](FactKindIncidentRoutingCoverageWarning, env)
}

// EncodeIncidentRoutingCoverageWarning marshals an incidentv1.CoverageWarning
// into the map[string]any payload shape an Envelope carries. It is the inverse
// of DecodeIncidentRoutingCoverageWarning for schema-version-1 payloads.
func EncodeIncidentRoutingCoverageWarning(warning incidentv1.CoverageWarning) (map[string]any, error) {
	return encodeDirectPayload(warning)
}
