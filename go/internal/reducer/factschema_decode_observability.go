// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
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

// decodeObservabilityDeclaredFolder decodes one observability.declared_folder envelope into the typed
// observabilityv1.DeclaredFolder struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func decodeObservabilityDeclaredFolder(env facts.Envelope) (observabilityv1.DeclaredFolder, error) {
	value, err := factschema.DecodeObservabilityDeclaredFolder(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredFolder{}, newFactDecodeError(factschema.FactKindObservabilityDeclaredFolder, err)
	}
	return value, nil
}

// decodeObservabilityDeclaredDashboard decodes one observability.declared_dashboard envelope into the typed
// observabilityv1.DeclaredDashboard struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func decodeObservabilityDeclaredDashboard(env facts.Envelope) (observabilityv1.DeclaredDashboard, error) {
	value, err := factschema.DecodeObservabilityDeclaredDashboard(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredDashboard{}, newFactDecodeError(factschema.FactKindObservabilityDeclaredDashboard, err)
	}
	return value, nil
}

// decodeObservabilityDeclaredDatasource decodes one observability.declared_datasource envelope into the typed
// observabilityv1.DeclaredDatasource struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func decodeObservabilityDeclaredDatasource(env facts.Envelope) (observabilityv1.DeclaredDatasource, error) {
	value, err := factschema.DecodeObservabilityDeclaredDatasource(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredDatasource{}, newFactDecodeError(factschema.FactKindObservabilityDeclaredDatasource, err)
	}
	return value, nil
}

// decodeObservabilityDeclaredAlertRule decodes one observability.declared_alert_rule envelope into the typed
// observabilityv1.DeclaredAlertRule struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func decodeObservabilityDeclaredAlertRule(env facts.Envelope) (observabilityv1.DeclaredAlertRule, error) {
	value, err := factschema.DecodeObservabilityDeclaredAlertRule(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredAlertRule{}, newFactDecodeError(factschema.FactKindObservabilityDeclaredAlertRule, err)
	}
	return value, nil
}

// decodeObservabilityDeclaredScrapeConfig decodes one observability.declared_scrape_config envelope into the typed
// observabilityv1.DeclaredScrapeConfig struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func decodeObservabilityDeclaredScrapeConfig(env facts.Envelope) (observabilityv1.DeclaredScrapeConfig, error) {
	value, err := factschema.DecodeObservabilityDeclaredScrapeConfig(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredScrapeConfig{}, newFactDecodeError(factschema.FactKindObservabilityDeclaredScrapeConfig, err)
	}
	return value, nil
}

// decodeObservabilityDeclaredMetricRule decodes one observability.declared_metric_rule envelope into the typed
// observabilityv1.DeclaredMetricRule struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func decodeObservabilityDeclaredMetricRule(env facts.Envelope) (observabilityv1.DeclaredMetricRule, error) {
	value, err := factschema.DecodeObservabilityDeclaredMetricRule(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredMetricRule{}, newFactDecodeError(factschema.FactKindObservabilityDeclaredMetricRule, err)
	}
	return value, nil
}

// decodeObservabilityDeclaredMetricRoute decodes one observability.declared_metric_route envelope into the typed
// observabilityv1.DeclaredMetricRoute struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func decodeObservabilityDeclaredMetricRoute(env facts.Envelope) (observabilityv1.DeclaredMetricRoute, error) {
	value, err := factschema.DecodeObservabilityDeclaredMetricRoute(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredMetricRoute{}, newFactDecodeError(factschema.FactKindObservabilityDeclaredMetricRoute, err)
	}
	return value, nil
}

// decodeObservabilityDeclaredLogRoute decodes one observability.declared_log_route envelope into the typed
// observabilityv1.DeclaredLogRoute struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func decodeObservabilityDeclaredLogRoute(env facts.Envelope) (observabilityv1.DeclaredLogRoute, error) {
	value, err := factschema.DecodeObservabilityDeclaredLogRoute(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredLogRoute{}, newFactDecodeError(factschema.FactKindObservabilityDeclaredLogRoute, err)
	}
	return value, nil
}

// decodeObservabilityDeclaredTraceRoute decodes one observability.declared_trace_route envelope into the typed
// observabilityv1.DeclaredTraceRoute struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func decodeObservabilityDeclaredTraceRoute(env facts.Envelope) (observabilityv1.DeclaredTraceRoute, error) {
	value, err := factschema.DecodeObservabilityDeclaredTraceRoute(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.DeclaredTraceRoute{}, newFactDecodeError(factschema.FactKindObservabilityDeclaredTraceRoute, err)
	}
	return value, nil
}

// decodeObservabilityAppliedResource decodes one observability.applied_resource envelope into the typed
// observabilityv1.AppliedResource struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func decodeObservabilityAppliedResource(env facts.Envelope) (observabilityv1.AppliedResource, error) {
	value, err := factschema.DecodeObservabilityAppliedResource(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.AppliedResource{}, newFactDecodeError(factschema.FactKindObservabilityAppliedResource, err)
	}
	return value, nil
}

// decodeObservabilityAppliedSyncState decodes one observability.applied_sync_state envelope into the typed
// observabilityv1.AppliedSyncState struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func decodeObservabilityAppliedSyncState(env facts.Envelope) (observabilityv1.AppliedSyncState, error) {
	value, err := factschema.DecodeObservabilityAppliedSyncState(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.AppliedSyncState{}, newFactDecodeError(factschema.FactKindObservabilityAppliedSyncState, err)
	}
	return value, nil
}

// decodeObservabilityObservedDashboard decodes one observability.observed_dashboard envelope into the typed
// observabilityv1.ObservedDashboard struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id (and provider_object_uid) field or is otherwise malformed.
func decodeObservabilityObservedDashboard(env facts.Envelope) (observabilityv1.ObservedDashboard, error) {
	value, err := factschema.DecodeObservabilityObservedDashboard(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.ObservedDashboard{}, newFactDecodeError(factschema.FactKindObservabilityObservedDashboard, err)
	}
	return value, nil
}

// decodeObservabilityObservedTarget decodes one observability.observed_target envelope into the typed
// observabilityv1.ObservedTarget struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id (and provider_object_uid) field or is otherwise malformed.
func decodeObservabilityObservedTarget(env facts.Envelope) (observabilityv1.ObservedTarget, error) {
	value, err := factschema.DecodeObservabilityObservedTarget(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.ObservedTarget{}, newFactDecodeError(factschema.FactKindObservabilityObservedTarget, err)
	}
	return value, nil
}

// decodeObservabilityObservedRule decodes one observability.observed_rule envelope into the typed
// observabilityv1.ObservedRule struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func decodeObservabilityObservedRule(env facts.Envelope) (observabilityv1.ObservedRule, error) {
	value, err := factschema.DecodeObservabilityObservedRule(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.ObservedRule{}, newFactDecodeError(factschema.FactKindObservabilityObservedRule, err)
	}
	return value, nil
}

// decodeObservabilityObservedLogSignal decodes one observability.observed_log_signal envelope into the typed
// observabilityv1.ObservedLogSignal struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id (and provider_object_uid) field or is otherwise malformed.
func decodeObservabilityObservedLogSignal(env facts.Envelope) (observabilityv1.ObservedLogSignal, error) {
	value, err := factschema.DecodeObservabilityObservedLogSignal(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.ObservedLogSignal{}, newFactDecodeError(factschema.FactKindObservabilityObservedLogSignal, err)
	}
	return value, nil
}

// decodeObservabilityObservedTraceSignal decodes one observability.observed_trace_signal envelope into the typed
// observabilityv1.ObservedTraceSignal struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id (and provider_object_uid) field or is otherwise malformed.
func decodeObservabilityObservedTraceSignal(env facts.Envelope) (observabilityv1.ObservedTraceSignal, error) {
	value, err := factschema.DecodeObservabilityObservedTraceSignal(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.ObservedTraceSignal{}, newFactDecodeError(factschema.FactKindObservabilityObservedTraceSignal, err)
	}
	return value, nil
}

// decodeObservabilityCoverageWarning decodes one observability.coverage_warning envelope into the typed
// observabilityv1.CoverageWarning struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing
// its required source_instance_id field or is otherwise malformed.
func decodeObservabilityCoverageWarning(env facts.Envelope) (observabilityv1.CoverageWarning, error) {
	value, err := factschema.DecodeObservabilityCoverageWarning(factschemaEnvelope(env))
	if err != nil {
		return observabilityv1.CoverageWarning{}, newFactDecodeError(factschema.FactKindObservabilityCoverageWarning, err)
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
