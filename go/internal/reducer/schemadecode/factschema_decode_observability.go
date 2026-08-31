// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	observabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/observability/v1"
)

// This file is the reducer-side decode seam for the observability fact family
// (Contract System v1 Wave 4e, #4582). Each decode<Kind> wrapper routes one
// envelope through the contracts module's typed decode and re-wraps any error
// as a self-classifying *factDecodeError, so the coverage-metadata classifier
// can partition a missing-required-field decode failure into a per-fact
// input_invalid quarantine (partitionDecodeFailures) rather than reading raw
// payloadString map lookups. source_instance_id is required on every kind; the
// four observed kinds whose sole emitter always writes it also require
// provider_object_uid (observability/v1/doc.go).

// DecodeObservabilityDeclaredFolder decodes one observability.declared_folder envelope into the typed
// observabilityv1.DeclaredFolder struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func DecodeObservabilityDeclaredFolder(env facts.Envelope) (observabilityv1.DeclaredFolder, error) {
	value, err := factschema.DecodeObservabilityDeclaredFolder(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredFolder{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityDeclaredFolder, err)
	}
	return value, nil
}

// DecodeObservabilityDeclaredDashboard decodes one observability.declared_dashboard envelope into the typed
// observabilityv1.DeclaredDashboard struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func DecodeObservabilityDeclaredDashboard(env facts.Envelope) (observabilityv1.DeclaredDashboard, error) {
	value, err := factschema.DecodeObservabilityDeclaredDashboard(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredDashboard{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityDeclaredDashboard, err)
	}
	return value, nil
}

// DecodeObservabilityDeclaredDatasource decodes one observability.declared_datasource envelope into the typed
// observabilityv1.DeclaredDatasource struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func DecodeObservabilityDeclaredDatasource(env facts.Envelope) (observabilityv1.DeclaredDatasource, error) {
	value, err := factschema.DecodeObservabilityDeclaredDatasource(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredDatasource{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityDeclaredDatasource, err)
	}
	return value, nil
}

// DecodeObservabilityDeclaredAlertRule decodes one observability.declared_alert_rule envelope into the typed
// observabilityv1.DeclaredAlertRule struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func DecodeObservabilityDeclaredAlertRule(env facts.Envelope) (observabilityv1.DeclaredAlertRule, error) {
	value, err := factschema.DecodeObservabilityDeclaredAlertRule(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredAlertRule{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityDeclaredAlertRule, err)
	}
	return value, nil
}

// DecodeObservabilityDeclaredScrapeConfig decodes one observability.declared_scrape_config envelope into the typed
// observabilityv1.DeclaredScrapeConfig struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func DecodeObservabilityDeclaredScrapeConfig(env facts.Envelope) (observabilityv1.DeclaredScrapeConfig, error) {
	value, err := factschema.DecodeObservabilityDeclaredScrapeConfig(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredScrapeConfig{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityDeclaredScrapeConfig, err)
	}
	return value, nil
}

// DecodeObservabilityDeclaredMetricRule decodes one observability.declared_metric_rule envelope into the typed
// observabilityv1.DeclaredMetricRule struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func DecodeObservabilityDeclaredMetricRule(env facts.Envelope) (observabilityv1.DeclaredMetricRule, error) {
	value, err := factschema.DecodeObservabilityDeclaredMetricRule(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredMetricRule{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityDeclaredMetricRule, err)
	}
	return value, nil
}

// DecodeObservabilityDeclaredMetricRoute decodes one observability.declared_metric_route envelope into the typed
// observabilityv1.DeclaredMetricRoute struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func DecodeObservabilityDeclaredMetricRoute(env facts.Envelope) (observabilityv1.DeclaredMetricRoute, error) {
	value, err := factschema.DecodeObservabilityDeclaredMetricRoute(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredMetricRoute{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityDeclaredMetricRoute, err)
	}
	return value, nil
}

// DecodeObservabilityDeclaredLogRoute decodes one observability.declared_log_route envelope into the typed
// observabilityv1.DeclaredLogRoute struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func DecodeObservabilityDeclaredLogRoute(env facts.Envelope) (observabilityv1.DeclaredLogRoute, error) {
	value, err := factschema.DecodeObservabilityDeclaredLogRoute(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredLogRoute{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityDeclaredLogRoute, err)
	}
	return value, nil
}

// DecodeObservabilityDeclaredTraceRoute decodes one observability.declared_trace_route envelope into the typed
// observabilityv1.DeclaredTraceRoute struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func DecodeObservabilityDeclaredTraceRoute(env facts.Envelope) (observabilityv1.DeclaredTraceRoute, error) {
	value, err := factschema.DecodeObservabilityDeclaredTraceRoute(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredTraceRoute{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityDeclaredTraceRoute, err)
	}
	return value, nil
}

// DecodeObservabilityAppliedResource decodes one observability.applied_resource envelope into the typed
// observabilityv1.AppliedResource struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func DecodeObservabilityAppliedResource(env facts.Envelope) (observabilityv1.AppliedResource, error) {
	value, err := factschema.DecodeObservabilityAppliedResource(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.AppliedResource{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityAppliedResource, err)
	}
	return value, nil
}

// DecodeObservabilityAppliedSyncState decodes one observability.applied_sync_state envelope into the typed
// observabilityv1.AppliedSyncState struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func DecodeObservabilityAppliedSyncState(env facts.Envelope) (observabilityv1.AppliedSyncState, error) {
	value, err := factschema.DecodeObservabilityAppliedSyncState(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.AppliedSyncState{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityAppliedSyncState, err)
	}
	return value, nil
}

// DecodeObservabilityObservedDashboard decodes one observability.observed_dashboard envelope into the typed
// observabilityv1.ObservedDashboard struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id (and provider_object_uid) field or is otherwise malformed.
func DecodeObservabilityObservedDashboard(env facts.Envelope) (observabilityv1.ObservedDashboard, error) {
	value, err := factschema.DecodeObservabilityObservedDashboard(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.ObservedDashboard{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityObservedDashboard, err)
	}
	return value, nil
}

// DecodeObservabilityObservedTarget decodes one observability.observed_target envelope into the typed
// observabilityv1.ObservedTarget struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id (and provider_object_uid) field or is otherwise malformed.
func DecodeObservabilityObservedTarget(env facts.Envelope) (observabilityv1.ObservedTarget, error) {
	value, err := factschema.DecodeObservabilityObservedTarget(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.ObservedTarget{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityObservedTarget, err)
	}
	return value, nil
}

// DecodeObservabilityObservedRule decodes one observability.observed_rule envelope into the typed
// observabilityv1.ObservedRule struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func DecodeObservabilityObservedRule(env facts.Envelope) (observabilityv1.ObservedRule, error) {
	value, err := factschema.DecodeObservabilityObservedRule(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.ObservedRule{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityObservedRule, err)
	}
	return value, nil
}

// DecodeObservabilityObservedLogSignal decodes one observability.observed_log_signal envelope into the typed
// observabilityv1.ObservedLogSignal struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id (and provider_object_uid) field or is otherwise malformed.
func DecodeObservabilityObservedLogSignal(env facts.Envelope) (observabilityv1.ObservedLogSignal, error) {
	value, err := factschema.DecodeObservabilityObservedLogSignal(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.ObservedLogSignal{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityObservedLogSignal, err)
	}
	return value, nil
}

// DecodeObservabilityObservedTraceSignal decodes one observability.observed_trace_signal envelope into the typed
// observabilityv1.ObservedTraceSignal struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id (and provider_object_uid) field or is otherwise malformed.
func DecodeObservabilityObservedTraceSignal(env facts.Envelope) (observabilityv1.ObservedTraceSignal, error) {
	value, err := factschema.DecodeObservabilityObservedTraceSignal(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.ObservedTraceSignal{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityObservedTraceSignal, err)
	}
	return value, nil
}

// DecodeObservabilityCoverageWarning decodes one observability.coverage_warning envelope into the typed
// observabilityv1.CoverageWarning struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func DecodeObservabilityCoverageWarning(env facts.Envelope) (observabilityv1.CoverageWarning, error) {
	value, err := factschema.DecodeObservabilityCoverageWarning(FactschemaEnvelope(env))
	if err != nil {
		return observabilityv1.CoverageWarning{}, factdecode.NewFactDecodeError(factschema.FactKindObservabilityCoverageWarning, err)
	}
	return value, nil
}

// observability.source_instance intentionally has NO reducer decode wrapper: the
// coverage-metadata classifier skips that kind (it carries no coverage object),
// so no reducer read path decodes it. Its typed struct, schema, and contracts
// Decode function still exist for a uniform family surface and round-trip tests,
// mirroring how the sbom family leaves its unconsumed kinds typed-but-unwired on
// the reducer side. Because no reducer decode seam reads it,
// FactKindObservabilitySourceInstance is also intentionally absent from the
// payload-usage manifest's factKindSchemaFile map.
