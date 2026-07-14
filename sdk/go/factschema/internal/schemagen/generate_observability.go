// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	observabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/observability/v1"
)

// ObservabilityDeclaredFolderSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_folder' payload.
const ObservabilityDeclaredFolderSchemaID = schemaBaseID + "observability/v1/declared_folder.schema.json"

// ObservabilityDeclaredFolderSchema returns the JSON Schema bytes for observabilityv1.DeclaredFolder.
func ObservabilityDeclaredFolderSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredFolderSchemaID, "Eshu observability.declared_folder Payload (schema version 1)", &observabilityv1.DeclaredFolder{})
}

// ObservabilityDeclaredDashboardSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_dashboard' payload.
const ObservabilityDeclaredDashboardSchemaID = schemaBaseID + "observability/v1/declared_dashboard.schema.json"

// ObservabilityDeclaredDashboardSchema returns the JSON Schema bytes for observabilityv1.DeclaredDashboard.
func ObservabilityDeclaredDashboardSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredDashboardSchemaID, "Eshu observability.declared_dashboard Payload (schema version 1)", &observabilityv1.DeclaredDashboard{})
}

// ObservabilityDeclaredDatasourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_datasource' payload.
const ObservabilityDeclaredDatasourceSchemaID = schemaBaseID + "observability/v1/declared_datasource.schema.json"

// ObservabilityDeclaredDatasourceSchema returns the JSON Schema bytes for observabilityv1.DeclaredDatasource.
func ObservabilityDeclaredDatasourceSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredDatasourceSchemaID, "Eshu observability.declared_datasource Payload (schema version 1)", &observabilityv1.DeclaredDatasource{})
}

// ObservabilityDeclaredAlertRuleSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_alert_rule' payload.
const ObservabilityDeclaredAlertRuleSchemaID = schemaBaseID + "observability/v1/declared_alert_rule.schema.json"

// ObservabilityDeclaredAlertRuleSchema returns the JSON Schema bytes for observabilityv1.DeclaredAlertRule.
func ObservabilityDeclaredAlertRuleSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredAlertRuleSchemaID, "Eshu observability.declared_alert_rule Payload (schema version 1)", &observabilityv1.DeclaredAlertRule{})
}

// ObservabilityDeclaredScrapeConfigSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_scrape_config' payload.
const ObservabilityDeclaredScrapeConfigSchemaID = schemaBaseID + "observability/v1/declared_scrape_config.schema.json"

// ObservabilityDeclaredScrapeConfigSchema returns the JSON Schema bytes for observabilityv1.DeclaredScrapeConfig.
func ObservabilityDeclaredScrapeConfigSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredScrapeConfigSchemaID, "Eshu observability.declared_scrape_config Payload (schema version 1)", &observabilityv1.DeclaredScrapeConfig{})
}

// ObservabilityDeclaredMetricRuleSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_metric_rule' payload.
const ObservabilityDeclaredMetricRuleSchemaID = schemaBaseID + "observability/v1/declared_metric_rule.schema.json"

// ObservabilityDeclaredMetricRuleSchema returns the JSON Schema bytes for observabilityv1.DeclaredMetricRule.
func ObservabilityDeclaredMetricRuleSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredMetricRuleSchemaID, "Eshu observability.declared_metric_rule Payload (schema version 1)", &observabilityv1.DeclaredMetricRule{})
}

// ObservabilityDeclaredMetricRouteSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_metric_route' payload.
const ObservabilityDeclaredMetricRouteSchemaID = schemaBaseID + "observability/v1/declared_metric_route.schema.json"

// ObservabilityDeclaredMetricRouteSchema returns the JSON Schema bytes for observabilityv1.DeclaredMetricRoute.
func ObservabilityDeclaredMetricRouteSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredMetricRouteSchemaID, "Eshu observability.declared_metric_route Payload (schema version 1)", &observabilityv1.DeclaredMetricRoute{})
}

// ObservabilityDeclaredLogRouteSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_log_route' payload.
const ObservabilityDeclaredLogRouteSchemaID = schemaBaseID + "observability/v1/declared_log_route.schema.json"

// ObservabilityDeclaredLogRouteSchema returns the JSON Schema bytes for observabilityv1.DeclaredLogRoute.
func ObservabilityDeclaredLogRouteSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredLogRouteSchemaID, "Eshu observability.declared_log_route Payload (schema version 1)", &observabilityv1.DeclaredLogRoute{})
}

// ObservabilityDeclaredTraceRouteSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.declared_trace_route' payload.
const ObservabilityDeclaredTraceRouteSchemaID = schemaBaseID + "observability/v1/declared_trace_route.schema.json"

// ObservabilityDeclaredTraceRouteSchema returns the JSON Schema bytes for observabilityv1.DeclaredTraceRoute.
func ObservabilityDeclaredTraceRouteSchema() ([]byte, error) {
	return reflectSchema(ObservabilityDeclaredTraceRouteSchemaID, "Eshu observability.declared_trace_route Payload (schema version 1)", &observabilityv1.DeclaredTraceRoute{})
}

// ObservabilityAppliedResourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.applied_resource' payload.
const ObservabilityAppliedResourceSchemaID = schemaBaseID + "observability/v1/applied_resource.schema.json"

// ObservabilityAppliedResourceSchema returns the JSON Schema bytes for observabilityv1.AppliedResource.
func ObservabilityAppliedResourceSchema() ([]byte, error) {
	return reflectSchema(ObservabilityAppliedResourceSchemaID, "Eshu observability.applied_resource Payload (schema version 1)", &observabilityv1.AppliedResource{})
}

// ObservabilityAppliedSyncStateSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.applied_sync_state' payload.
const ObservabilityAppliedSyncStateSchemaID = schemaBaseID + "observability/v1/applied_sync_state.schema.json"

// ObservabilityAppliedSyncStateSchema returns the JSON Schema bytes for observabilityv1.AppliedSyncState.
func ObservabilityAppliedSyncStateSchema() ([]byte, error) {
	return reflectSchema(ObservabilityAppliedSyncStateSchemaID, "Eshu observability.applied_sync_state Payload (schema version 1)", &observabilityv1.AppliedSyncState{})
}

// ObservabilityObservedDashboardSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.observed_dashboard' payload.
const ObservabilityObservedDashboardSchemaID = schemaBaseID + "observability/v1/observed_dashboard.schema.json"

// ObservabilityObservedDashboardSchema returns the JSON Schema bytes for observabilityv1.ObservedDashboard.
func ObservabilityObservedDashboardSchema() ([]byte, error) {
	return reflectSchema(ObservabilityObservedDashboardSchemaID, "Eshu observability.observed_dashboard Payload (schema version 1)", &observabilityv1.ObservedDashboard{})
}

// ObservabilityObservedTargetSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.observed_target' payload.
const ObservabilityObservedTargetSchemaID = schemaBaseID + "observability/v1/observed_target.schema.json"

// ObservabilityObservedTargetSchema returns the JSON Schema bytes for observabilityv1.ObservedTarget.
func ObservabilityObservedTargetSchema() ([]byte, error) {
	return reflectSchema(ObservabilityObservedTargetSchemaID, "Eshu observability.observed_target Payload (schema version 1)", &observabilityv1.ObservedTarget{})
}

// ObservabilityObservedRuleSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.observed_rule' payload.
const ObservabilityObservedRuleSchemaID = schemaBaseID + "observability/v1/observed_rule.schema.json"

// ObservabilityObservedRuleSchema returns the JSON Schema bytes for observabilityv1.ObservedRule.
func ObservabilityObservedRuleSchema() ([]byte, error) {
	return reflectSchema(ObservabilityObservedRuleSchemaID, "Eshu observability.observed_rule Payload (schema version 1)", &observabilityv1.ObservedRule{})
}

// ObservabilityObservedLogSignalSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.observed_log_signal' payload.
const ObservabilityObservedLogSignalSchemaID = schemaBaseID + "observability/v1/observed_log_signal.schema.json"

// ObservabilityObservedLogSignalSchema returns the JSON Schema bytes for observabilityv1.ObservedLogSignal.
func ObservabilityObservedLogSignalSchema() ([]byte, error) {
	return reflectSchema(ObservabilityObservedLogSignalSchemaID, "Eshu observability.observed_log_signal Payload (schema version 1)", &observabilityv1.ObservedLogSignal{})
}

// ObservabilityObservedTraceSignalSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.observed_trace_signal' payload.
const ObservabilityObservedTraceSignalSchemaID = schemaBaseID + "observability/v1/observed_trace_signal.schema.json"

// ObservabilityObservedTraceSignalSchema returns the JSON Schema bytes for observabilityv1.ObservedTraceSignal.
func ObservabilityObservedTraceSignalSchema() ([]byte, error) {
	return reflectSchema(ObservabilityObservedTraceSignalSchemaID, "Eshu observability.observed_trace_signal Payload (schema version 1)", &observabilityv1.ObservedTraceSignal{})
}

// ObservabilityCoverageWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.coverage_warning' payload.
const ObservabilityCoverageWarningSchemaID = schemaBaseID + "observability/v1/coverage_warning.schema.json"

// ObservabilityCoverageWarningSchema returns the JSON Schema bytes for observabilityv1.CoverageWarning.
func ObservabilityCoverageWarningSchema() ([]byte, error) {
	return reflectSchema(ObservabilityCoverageWarningSchemaID, "Eshu observability.coverage_warning Payload (schema version 1)", &observabilityv1.CoverageWarning{})
}

// ObservabilitySourceInstanceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 'observability.source_instance' payload.
const ObservabilitySourceInstanceSchemaID = schemaBaseID + "observability/v1/source_instance.schema.json"

// ObservabilitySourceInstanceSchema returns the JSON Schema bytes for observabilityv1.SourceInstance.
func ObservabilitySourceInstanceSchema() ([]byte, error) {
	return reflectSchema(ObservabilitySourceInstanceSchemaID, "Eshu observability.source_instance Payload (schema version 1)", &observabilityv1.SourceInstance{})
}
