// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"log/slog"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
	servicecatalogv1 "github.com/eshu-hq/eshu/sdk/go/factschema/servicecatalog/v1"
)

// This file holds the query-side decode wrappers for the incident-context
// read model (#4794 W2a): the incident.record, incident.lifecycle_event,
// change.record, incident_routing.applied_pagerduty_resource,
// incident_routing.observed_pagerduty_service, and
// incident_routing.coverage_warning fact kinds the PagerDuty incident-context
// read model (incident_context_*.go) reads, plus
// service_catalog.operational_link, which the same read model's runtime-
// evidence store reads. Each wraps the contracts-module Decode* seam and, on a
// classified *factschema.DecodeError, returns a *queryDecodeError so the
// caller drops the row (an input_invalid read-model outcome) instead of
// silently defaulting every field to "" — see factschema_decode_workitem.go
// for the template this mirrors.
//
// incident.lifecycle_event and change.record have no reducer decode call (see
// go/internal/payloadusage/schema.go), so this is their ONLY typed decode
// site; incident.record and the incident_routing.* kinds are ALSO decoded by
// the reducer (sdk/go/factschema/decode_incident.go via
// go/internal/reducer/factschema_decode_incident.go) for a different,
// unrelated purpose (deployment/repository correlation), so this file is a
// second, independent decode site for those kinds — both are gated by the
// merged reducer+query payload-usage manifest (payloadusage.Load).
// service_catalog.operational_link previously had no decode seam at all; its
// raw-SQL JSONB read in incident_context_runtime_store.go is the first.

// incidentContextDecodeInput carries one scanned incident-context fact row
// into a decode wrapper. Bundling FactID, SourceRecordID, SchemaVersion, and
// Payload into a single parameter keeps the one-argument shape the
// payload-usage manifest gate's seam parser recognizes (see
// workItemDecodeInput). SourceRecordID is used only by the three kinds whose
// collector-emitted identity field predates being made required
// (incidentIdentityFallback); it is unused, but harmless, for kinds with no
// fallback.
type incidentContextDecodeInput struct {
	FactID         string
	SourceRecordID string
	SchemaVersion  string
	Payload        map[string]any
}

// decodeIncidentRecord decodes one incident.record fact row into the typed
// struct through the contracts seam. A payload that omits provider_incident_id
// entirely falls back to the fact's durable source_record_id as the identity
// (see incidentIdentityFallback) — the documented, tested read path
// (TestPostgresIncidentContextStoreReadsCollectedPagerDutyIncidentBySourceRecordID)
// for a PagerDuty incident fact collected before the emitter always stamped
// the payload key. A payload missing BOTH provider_incident_id and a usable
// source_record_id still dead-letters as input_invalid.
func decodeIncidentRecord(in incidentContextDecodeInput) (incidentv1.IncidentRecord, error) {
	record, err := factschema.DecodeIncidentRecord(workItemSchemaEnvelope(factschema.FactKindIncidentRecord, in.SchemaVersion, in.Payload))
	if err == nil {
		return record, nil
	}
	if fallback := incidentIdentityFallback(err, "provider_incident_id", in.SourceRecordID); fallback != "" {
		payload := incidentPayloadWithFallbackIdentity(in.Payload, "provider_incident_id", fallback)
		retried, retryErr := factschema.DecodeIncidentRecord(workItemSchemaEnvelope(factschema.FactKindIncidentRecord, in.SchemaVersion, payload))
		if retryErr == nil {
			return retried, nil
		}
		err = retryErr
	}
	return incidentv1.IncidentRecord{}, newQueryDecodeError(factschema.FactKindIncidentRecord, in.FactID, err)
}

// decodeIncidentLifecycleEvent decodes one incident.lifecycle_event fact row
// into the typed struct. A payload that omits provider_event_id falls back to
// source_record_id, mirroring decodeIncidentRecord's fallback for the same
// reason (see incidentIdentityFallback).
func decodeIncidentLifecycleEvent(in incidentContextDecodeInput) (incidentv1.LifecycleEvent, error) {
	event, err := factschema.DecodeIncidentLifecycleEvent(workItemSchemaEnvelope(factschema.FactKindIncidentLifecycleEvent, in.SchemaVersion, in.Payload))
	if err == nil {
		return event, nil
	}
	if fallback := incidentIdentityFallback(err, "provider_event_id", in.SourceRecordID); fallback != "" {
		payload := incidentPayloadWithFallbackIdentity(in.Payload, "provider_event_id", fallback)
		retried, retryErr := factschema.DecodeIncidentLifecycleEvent(workItemSchemaEnvelope(factschema.FactKindIncidentLifecycleEvent, in.SchemaVersion, payload))
		if retryErr == nil {
			return retried, nil
		}
		err = retryErr
	}
	return incidentv1.LifecycleEvent{}, newQueryDecodeError(factschema.FactKindIncidentLifecycleEvent, in.FactID, err)
}

// decodeChangeRecord decodes one change.record fact row into the typed
// struct. A payload that omits provider_change_id falls back to
// source_record_id, mirroring decodeIncidentRecord's fallback for the same
// reason (see incidentIdentityFallback).
func decodeChangeRecord(in incidentContextDecodeInput) (incidentv1.ChangeRecord, error) {
	record, err := factschema.DecodeChangeRecord(workItemSchemaEnvelope(factschema.FactKindChangeRecord, in.SchemaVersion, in.Payload))
	if err == nil {
		return record, nil
	}
	if fallback := incidentIdentityFallback(err, "provider_change_id", in.SourceRecordID); fallback != "" {
		payload := incidentPayloadWithFallbackIdentity(in.Payload, "provider_change_id", fallback)
		retried, retryErr := factschema.DecodeChangeRecord(workItemSchemaEnvelope(factschema.FactKindChangeRecord, in.SchemaVersion, payload))
		if retryErr == nil {
			return retried, nil
		}
		err = retryErr
	}
	return incidentv1.ChangeRecord{}, newQueryDecodeError(factschema.FactKindChangeRecord, in.FactID, err)
}

// decodeIncidentRoutingAppliedPagerDutyResource decodes one
// incident_routing.applied_pagerduty_resource fact row into the typed struct.
// A missing required field (see incidentv1.AppliedPagerDutyResource) yields a
// self-classifying *queryDecodeError.
func decodeIncidentRoutingAppliedPagerDutyResource(in incidentContextDecodeInput) (incidentv1.AppliedPagerDutyResource, error) {
	resource, err := factschema.DecodeIncidentRoutingAppliedPagerDutyResource(workItemSchemaEnvelope(factschema.FactKindIncidentRoutingAppliedPagerDutyResource, in.SchemaVersion, in.Payload))
	if err != nil {
		return incidentv1.AppliedPagerDutyResource{}, newQueryDecodeError(factschema.FactKindIncidentRoutingAppliedPagerDutyResource, in.FactID, err)
	}
	return resource, nil
}

// decodeIncidentRoutingObservedPagerDutyService decodes one
// incident_routing.observed_pagerduty_service fact row into the typed struct.
// A missing required field (see incidentv1.ObservedPagerDutyService) yields a
// self-classifying *queryDecodeError.
func decodeIncidentRoutingObservedPagerDutyService(in incidentContextDecodeInput) (incidentv1.ObservedPagerDutyService, error) {
	service, err := factschema.DecodeIncidentRoutingObservedPagerDutyService(workItemSchemaEnvelope(factschema.FactKindIncidentRoutingObservedPagerDutyService, in.SchemaVersion, in.Payload))
	if err != nil {
		return incidentv1.ObservedPagerDutyService{}, newQueryDecodeError(factschema.FactKindIncidentRoutingObservedPagerDutyService, in.FactID, err)
	}
	return service, nil
}

// decodeIncidentRoutingCoverageWarning decodes one
// incident_routing.coverage_warning fact row into the typed struct. A missing
// required field (see incidentv1.CoverageWarning) yields a self-classifying
// *queryDecodeError.
func decodeIncidentRoutingCoverageWarning(in incidentContextDecodeInput) (incidentv1.CoverageWarning, error) {
	warning, err := factschema.DecodeIncidentRoutingCoverageWarning(workItemSchemaEnvelope(factschema.FactKindIncidentRoutingCoverageWarning, in.SchemaVersion, in.Payload))
	if err != nil {
		return incidentv1.CoverageWarning{}, newQueryDecodeError(factschema.FactKindIncidentRoutingCoverageWarning, in.FactID, err)
	}
	return warning, nil
}

// decodeServiceCatalogOperationalLink decodes one
// service_catalog.operational_link fact row into the typed struct. Every
// field of servicecatalogv1.OperationalLink is optional (see its doc comment),
// so this never dead-letters on a missing field; an unsupported schema major
// still returns a classified *queryDecodeError.
func decodeServiceCatalogOperationalLink(in incidentContextDecodeInput) (servicecatalogv1.OperationalLink, error) {
	link, err := factschema.DecodeServiceCatalogOperationalLink(workItemSchemaEnvelope(factschema.FactKindServiceCatalogOperationalLink, in.SchemaVersion, in.Payload))
	if err != nil {
		return servicecatalogv1.OperationalLink{}, newQueryDecodeError(factschema.FactKindServiceCatalogOperationalLink, in.FactID, err)
	}
	return link, nil
}

// incidentIdentityFallback returns sourceRecordID when err is a
// *factschema.DecodeError attributing the failure to exactly field (an absent
// or null required identity key) and sourceRecordID is non-empty, so the
// caller can retry the decode with the fallback identity injected into the
// payload. It returns "" for any other error shape, or when sourceRecordID is
// empty, so the caller dead-letters instead of masking a genuinely malformed
// fact.
func incidentIdentityFallback(err error, field, sourceRecordID string) string {
	if sourceRecordID == "" {
		return ""
	}
	var decodeErr *factschema.DecodeError
	if !errors.As(err, &decodeErr) {
		return ""
	}
	if decodeErr.Field != field {
		return ""
	}
	return sourceRecordID
}

// incidentPayloadWithFallbackIdentity returns a shallow copy of payload with
// field set to value, leaving the original map untouched. Used only to retry
// a decode after incidentIdentityFallback identifies a recoverable missing
// identity key — never to bypass the typed seam for a field the caller could
// otherwise read directly.
func incidentPayloadWithFallbackIdentity(payload map[string]any, field, value string) map[string]any {
	out := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		out[k] = v
	}
	out[field] = value
	return out
}

// logIncidentContextDecodeDrop emits an operator-diagnosable debug log for an
// incident-context evidence fact dropped from a read because its payload
// failed typed decode, mirroring logWorkItemEvidenceDecodeDrop for this read
// model's decode sites.
func logIncidentContextDecodeDrop(err error) {
	var decodeErr *queryDecodeError
	if !errors.As(err, &decodeErr) {
		slog.Debug("incident context fact dropped: decode error", slog.String("error", err.Error()))
		return
	}
	attrs := []any{
		slog.String("fact_id", decodeErr.FactID),
		slog.String("fact_kind", decodeErr.FactKind),
		slog.String("classification", decodeErr.Classification),
	}
	if decodeErr.Field != "" {
		attrs = append(attrs, slog.String("missing_field", decodeErr.Field))
	}
	slog.Debug("incident context fact dropped", attrs...)
}
