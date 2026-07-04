// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
)

// decodeIncidentRecord decodes one incident.record envelope into the typed
// incidentv1.IncidentRecord struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required field
// (provider, provider_incident_id) or is otherwise malformed. It is the single
// reducer decode site for the incident.record kind: the incident-routing
// evidence builder decodes through here, and a missing required field is routed
// through partitionDecodeFailures so it dead-letters as a per-fact input_invalid
// quarantine rather than a silent empty-string incident identity.
func decodeIncidentRecord(env facts.Envelope) (incidentv1.IncidentRecord, error) {
	record, err := factschema.DecodeIncidentRecord(factschemaEnvelope(env))
	if err != nil {
		return incidentv1.IncidentRecord{}, newFactDecodeError(factschema.FactKindIncidentRecord, err)
	}
	return record, nil
}

// decodeIncidentRoutingAppliedPagerDutyResource decodes one
// incident_routing.applied_pagerduty_resource envelope into the typed
// incidentv1.AppliedPagerDutyResource struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing a
// required field (source_class, source_kind, outcome, resource_class,
// terraform_state_address, resource_type, resource_name, module_address,
// provider_address, scope_id, state_generation_id, state_lineage, backend_kind,
// locator_hash, declared_match_state, redaction_state). It is the single reducer
// decode site for this kind.
func decodeIncidentRoutingAppliedPagerDutyResource(env facts.Envelope) (incidentv1.AppliedPagerDutyResource, error) {
	resource, err := factschema.DecodeIncidentRoutingAppliedPagerDutyResource(factschemaEnvelope(env))
	if err != nil {
		return incidentv1.AppliedPagerDutyResource{}, newFactDecodeError(factschema.FactKindIncidentRoutingAppliedPagerDutyResource, err)
	}
	return resource, nil
}

// decodeIncidentRoutingObservedPagerDutyService decodes one
// incident_routing.observed_pagerduty_service envelope into the typed
// incidentv1.ObservedPagerDutyService struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing a
// required field (provider, source_class, source_kind, outcome, resource_class,
// provider_object_id, scope_id, declared_match_state, redaction_state,
// service_id). It is the single reducer decode site for this kind.
func decodeIncidentRoutingObservedPagerDutyService(env facts.Envelope) (incidentv1.ObservedPagerDutyService, error) {
	service, err := factschema.DecodeIncidentRoutingObservedPagerDutyService(factschemaEnvelope(env))
	if err != nil {
		return incidentv1.ObservedPagerDutyService{}, newFactDecodeError(factschema.FactKindIncidentRoutingObservedPagerDutyService, err)
	}
	return service, nil
}

// decodeIncidentRoutingCoverageWarning decodes one
// incident_routing.coverage_warning envelope into the typed
// incidentv1.CoverageWarning struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required field
// (source_class, source_kind, outcome, resource_class, scope_id, reason,
// redaction_state, declared_match_state). It is the single reducer decode site
// for this kind.
func decodeIncidentRoutingCoverageWarning(env facts.Envelope) (incidentv1.CoverageWarning, error) {
	warning, err := factschema.DecodeIncidentRoutingCoverageWarning(factschemaEnvelope(env))
	if err != nil {
		return incidentv1.CoverageWarning{}, newFactDecodeError(factschema.FactKindIncidentRoutingCoverageWarning, err)
	}
	return warning, nil
}
