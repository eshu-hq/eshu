// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	observabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/observability/v1"
)

// DecodeObservabilityDeclaredFolder decodes env.Payload into the latest
// observabilityv1.DeclaredFolder struct for the 'observability.declared_folder' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityDeclaredFolder(env Envelope) (observabilityv1.DeclaredFolder, error) {
	return decodeLatestMajor[observabilityv1.DeclaredFolder](FactKindObservabilityDeclaredFolder, env)
}

// EncodeObservabilityDeclaredFolder marshals an observabilityv1.DeclaredFolder into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityDeclaredFolder for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityDeclaredFolder(value observabilityv1.DeclaredFolder) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityDeclaredDashboard decodes env.Payload into the latest
// observabilityv1.DeclaredDashboard struct for the 'observability.declared_dashboard' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityDeclaredDashboard(env Envelope) (observabilityv1.DeclaredDashboard, error) {
	return decodeLatestMajor[observabilityv1.DeclaredDashboard](FactKindObservabilityDeclaredDashboard, env)
}

// EncodeObservabilityDeclaredDashboard marshals an observabilityv1.DeclaredDashboard into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityDeclaredDashboard for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityDeclaredDashboard(value observabilityv1.DeclaredDashboard) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityDeclaredDatasource decodes env.Payload into the latest
// observabilityv1.DeclaredDatasource struct for the 'observability.declared_datasource' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityDeclaredDatasource(env Envelope) (observabilityv1.DeclaredDatasource, error) {
	return decodeLatestMajor[observabilityv1.DeclaredDatasource](FactKindObservabilityDeclaredDatasource, env)
}

// EncodeObservabilityDeclaredDatasource marshals an observabilityv1.DeclaredDatasource into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityDeclaredDatasource for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityDeclaredDatasource(value observabilityv1.DeclaredDatasource) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityDeclaredAlertRule decodes env.Payload into the latest
// observabilityv1.DeclaredAlertRule struct for the 'observability.declared_alert_rule' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityDeclaredAlertRule(env Envelope) (observabilityv1.DeclaredAlertRule, error) {
	return decodeLatestMajor[observabilityv1.DeclaredAlertRule](FactKindObservabilityDeclaredAlertRule, env)
}

// EncodeObservabilityDeclaredAlertRule marshals an observabilityv1.DeclaredAlertRule into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityDeclaredAlertRule for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityDeclaredAlertRule(value observabilityv1.DeclaredAlertRule) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityDeclaredScrapeConfig decodes env.Payload into the latest
// observabilityv1.DeclaredScrapeConfig struct for the 'observability.declared_scrape_config' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityDeclaredScrapeConfig(env Envelope) (observabilityv1.DeclaredScrapeConfig, error) {
	return decodeLatestMajor[observabilityv1.DeclaredScrapeConfig](FactKindObservabilityDeclaredScrapeConfig, env)
}

// EncodeObservabilityDeclaredScrapeConfig marshals an observabilityv1.DeclaredScrapeConfig into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityDeclaredScrapeConfig for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityDeclaredScrapeConfig(value observabilityv1.DeclaredScrapeConfig) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityDeclaredMetricRule decodes env.Payload into the latest
// observabilityv1.DeclaredMetricRule struct for the 'observability.declared_metric_rule' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityDeclaredMetricRule(env Envelope) (observabilityv1.DeclaredMetricRule, error) {
	return decodeLatestMajor[observabilityv1.DeclaredMetricRule](FactKindObservabilityDeclaredMetricRule, env)
}

// EncodeObservabilityDeclaredMetricRule marshals an observabilityv1.DeclaredMetricRule into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityDeclaredMetricRule for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityDeclaredMetricRule(value observabilityv1.DeclaredMetricRule) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityDeclaredMetricRoute decodes env.Payload into the latest
// observabilityv1.DeclaredMetricRoute struct for the 'observability.declared_metric_route' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityDeclaredMetricRoute(env Envelope) (observabilityv1.DeclaredMetricRoute, error) {
	return decodeLatestMajor[observabilityv1.DeclaredMetricRoute](FactKindObservabilityDeclaredMetricRoute, env)
}

// EncodeObservabilityDeclaredMetricRoute marshals an observabilityv1.DeclaredMetricRoute into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityDeclaredMetricRoute for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityDeclaredMetricRoute(value observabilityv1.DeclaredMetricRoute) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityDeclaredLogRoute decodes env.Payload into the latest
// observabilityv1.DeclaredLogRoute struct for the 'observability.declared_log_route' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityDeclaredLogRoute(env Envelope) (observabilityv1.DeclaredLogRoute, error) {
	return decodeLatestMajor[observabilityv1.DeclaredLogRoute](FactKindObservabilityDeclaredLogRoute, env)
}

// EncodeObservabilityDeclaredLogRoute marshals an observabilityv1.DeclaredLogRoute into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityDeclaredLogRoute for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityDeclaredLogRoute(value observabilityv1.DeclaredLogRoute) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityDeclaredTraceRoute decodes env.Payload into the latest
// observabilityv1.DeclaredTraceRoute struct for the 'observability.declared_trace_route' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityDeclaredTraceRoute(env Envelope) (observabilityv1.DeclaredTraceRoute, error) {
	return decodeLatestMajor[observabilityv1.DeclaredTraceRoute](FactKindObservabilityDeclaredTraceRoute, env)
}

// EncodeObservabilityDeclaredTraceRoute marshals an observabilityv1.DeclaredTraceRoute into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityDeclaredTraceRoute for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityDeclaredTraceRoute(value observabilityv1.DeclaredTraceRoute) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityAppliedResource decodes env.Payload into the latest
// observabilityv1.AppliedResource struct for the 'observability.applied_resource' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityAppliedResource(env Envelope) (observabilityv1.AppliedResource, error) {
	return decodeLatestMajor[observabilityv1.AppliedResource](FactKindObservabilityAppliedResource, env)
}

// EncodeObservabilityAppliedResource marshals an observabilityv1.AppliedResource into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityAppliedResource for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityAppliedResource(value observabilityv1.AppliedResource) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityAppliedSyncState decodes env.Payload into the latest
// observabilityv1.AppliedSyncState struct for the 'observability.applied_sync_state' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityAppliedSyncState(env Envelope) (observabilityv1.AppliedSyncState, error) {
	return decodeLatestMajor[observabilityv1.AppliedSyncState](FactKindObservabilityAppliedSyncState, env)
}

// EncodeObservabilityAppliedSyncState marshals an observabilityv1.AppliedSyncState into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityAppliedSyncState for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityAppliedSyncState(value observabilityv1.AppliedSyncState) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityObservedDashboard decodes env.Payload into the latest
// observabilityv1.ObservedDashboard struct for the 'observability.observed_dashboard' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// source_instance_id and provider_object_uid fields yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityObservedDashboard(env Envelope) (observabilityv1.ObservedDashboard, error) {
	return decodeLatestMajor[observabilityv1.ObservedDashboard](FactKindObservabilityObservedDashboard, env)
}

// EncodeObservabilityObservedDashboard marshals an observabilityv1.ObservedDashboard into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityObservedDashboard for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityObservedDashboard(value observabilityv1.ObservedDashboard) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityObservedTarget decodes env.Payload into the latest
// observabilityv1.ObservedTarget struct for the 'observability.observed_target' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// source_instance_id and provider_object_uid fields yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityObservedTarget(env Envelope) (observabilityv1.ObservedTarget, error) {
	return decodeLatestMajor[observabilityv1.ObservedTarget](FactKindObservabilityObservedTarget, env)
}

// EncodeObservabilityObservedTarget marshals an observabilityv1.ObservedTarget into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityObservedTarget for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityObservedTarget(value observabilityv1.ObservedTarget) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityObservedRule decodes env.Payload into the latest
// observabilityv1.ObservedRule struct for the 'observability.observed_rule' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityObservedRule(env Envelope) (observabilityv1.ObservedRule, error) {
	return decodeLatestMajor[observabilityv1.ObservedRule](FactKindObservabilityObservedRule, env)
}

// EncodeObservabilityObservedRule marshals an observabilityv1.ObservedRule into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityObservedRule for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityObservedRule(value observabilityv1.ObservedRule) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityObservedLogSignal decodes env.Payload into the latest
// observabilityv1.ObservedLogSignal struct for the 'observability.observed_log_signal' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// source_instance_id and provider_object_uid fields yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityObservedLogSignal(env Envelope) (observabilityv1.ObservedLogSignal, error) {
	return decodeLatestMajor[observabilityv1.ObservedLogSignal](FactKindObservabilityObservedLogSignal, env)
}

// EncodeObservabilityObservedLogSignal marshals an observabilityv1.ObservedLogSignal into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityObservedLogSignal for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityObservedLogSignal(value observabilityv1.ObservedLogSignal) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityObservedTraceSignal decodes env.Payload into the latest
// observabilityv1.ObservedTraceSignal struct for the 'observability.observed_trace_signal' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// source_instance_id and provider_object_uid fields yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityObservedTraceSignal(env Envelope) (observabilityv1.ObservedTraceSignal, error) {
	return decodeLatestMajor[observabilityv1.ObservedTraceSignal](FactKindObservabilityObservedTraceSignal, env)
}

// EncodeObservabilityObservedTraceSignal marshals an observabilityv1.ObservedTraceSignal into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityObservedTraceSignal for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityObservedTraceSignal(value observabilityv1.ObservedTraceSignal) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilityCoverageWarning decodes env.Payload into the latest
// observabilityv1.CoverageWarning struct for the 'observability.coverage_warning' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilityCoverageWarning(env Envelope) (observabilityv1.CoverageWarning, error) {
	return decodeLatestMajor[observabilityv1.CoverageWarning](FactKindObservabilityCoverageWarning, env)
}

// EncodeObservabilityCoverageWarning marshals an observabilityv1.CoverageWarning into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilityCoverageWarning for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilityCoverageWarning(value observabilityv1.CoverageWarning) (map[string]any, error) {
	return encodeToPayload(value)
}

// DecodeObservabilitySourceInstance decodes env.Payload into the latest
// observabilityv1.SourceInstance struct for the 'observability.source_instance' fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. A payload missing its
// required source_instance_id field yields a classified *DecodeError; callers
// (reducer handlers) must never substitute a zero-value struct on error.
func DecodeObservabilitySourceInstance(env Envelope) (observabilityv1.SourceInstance, error) {
	return decodeLatestMajor[observabilityv1.SourceInstance](FactKindObservabilitySourceInstance, env)
}

// EncodeObservabilitySourceInstance marshals an observabilityv1.SourceInstance into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeObservabilitySourceInstance for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeObservabilitySourceInstance(value observabilityv1.SourceInstance) (map[string]any, error) {
	return encodeToPayload(value)
}
