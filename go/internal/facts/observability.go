// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

const (
	// ObservabilitySourceInstanceFactKind identifies one configured declared,
	// applied, or observed observability evidence source.
	ObservabilitySourceInstanceFactKind = "observability.source_instance"
	// ObservabilityDeclaredFolderFactKind identifies declared Grafana folder
	// metadata from IaC or GitOps sources.
	ObservabilityDeclaredFolderFactKind = "observability.declared_folder"
	// ObservabilityDeclaredDashboardFactKind identifies declared Grafana
	// dashboard metadata from IaC or GitOps sources.
	ObservabilityDeclaredDashboardFactKind = "observability.declared_dashboard"
	// ObservabilityDeclaredDatasourceFactKind identifies declared Grafana
	// datasource metadata from IaC or GitOps sources.
	ObservabilityDeclaredDatasourceFactKind = "observability.declared_datasource"
	// ObservabilityDeclaredAlertRuleFactKind identifies declared Grafana alert
	// rule metadata from IaC or GitOps sources.
	ObservabilityDeclaredAlertRuleFactKind = "observability.declared_alert_rule"
	// ObservabilityDeclaredScrapeConfigFactKind identifies declared Prometheus
	// or Mimir scrape configuration metadata.
	ObservabilityDeclaredScrapeConfigFactKind = "observability.declared_scrape_config"
	// ObservabilityDeclaredMetricRuleFactKind identifies declared Prometheus or
	// Mimir rule metadata.
	ObservabilityDeclaredMetricRuleFactKind = "observability.declared_metric_rule"
	// ObservabilityDeclaredMetricRouteFactKind identifies declared metric
	// pipeline route metadata.
	ObservabilityDeclaredMetricRouteFactKind = "observability.declared_metric_route"
	// ObservabilityDeclaredLogRouteFactKind identifies declared log pipeline
	// route metadata.
	ObservabilityDeclaredLogRouteFactKind = "observability.declared_log_route"
	// ObservabilityDeclaredTraceRouteFactKind identifies declared trace pipeline
	// route metadata.
	ObservabilityDeclaredTraceRouteFactKind = "observability.declared_trace_route"
	// ObservabilityAppliedResourceFactKind identifies applied observability
	// resource metadata from Argo CD, Kubernetes, or equivalent state.
	ObservabilityAppliedResourceFactKind = "observability.applied_resource"
	// ObservabilityAppliedSyncStateFactKind identifies applied sync, health, or
	// permission state for observability resources.
	ObservabilityAppliedSyncStateFactKind = "observability.applied_sync_state"
	// ObservabilityObservedDashboardFactKind identifies live Grafana dashboard,
	// folder, datasource, or alert metadata.
	ObservabilityObservedDashboardFactKind = "observability.observed_dashboard"
	// ObservabilityObservedTargetFactKind identifies live Prometheus or Mimir
	// target metadata.
	ObservabilityObservedTargetFactKind = "observability.observed_target"
	// ObservabilityObservedRuleFactKind identifies live Prometheus, Mimir, or
	// Loki rule metadata.
	ObservabilityObservedRuleFactKind = "observability.observed_rule"
	// ObservabilityObservedLogSignalFactKind identifies bounded Loki log-signal
	// metadata without log lines.
	ObservabilityObservedLogSignalFactKind = "observability.observed_log_signal"
	// ObservabilityObservedTraceSignalFactKind identifies bounded Tempo
	// trace-signal metadata without spans.
	ObservabilityObservedTraceSignalFactKind = "observability.observed_trace_signal"
	// ObservabilityCoverageWarningFactKind identifies source-local coverage,
	// redaction, unsupported, stale, or permission-hidden warnings.
	ObservabilityCoverageWarningFactKind = "observability.coverage_warning"

	// ObservabilitySchemaVersionV1 is the first observability evidence schema.
	ObservabilitySchemaVersionV1 = "1.0.0"
)

var observabilityFactKinds = []string{
	ObservabilitySourceInstanceFactKind,
	ObservabilityDeclaredFolderFactKind,
	ObservabilityDeclaredDashboardFactKind,
	ObservabilityDeclaredDatasourceFactKind,
	ObservabilityDeclaredAlertRuleFactKind,
	ObservabilityDeclaredScrapeConfigFactKind,
	ObservabilityDeclaredMetricRuleFactKind,
	ObservabilityDeclaredMetricRouteFactKind,
	ObservabilityDeclaredLogRouteFactKind,
	ObservabilityDeclaredTraceRouteFactKind,
	ObservabilityAppliedResourceFactKind,
	ObservabilityAppliedSyncStateFactKind,
	ObservabilityObservedDashboardFactKind,
	ObservabilityObservedTargetFactKind,
	ObservabilityObservedRuleFactKind,
	ObservabilityObservedLogSignalFactKind,
	ObservabilityObservedTraceSignalFactKind,
	ObservabilityCoverageWarningFactKind,
}

var observabilitySchemaVersions = map[string]string{
	ObservabilitySourceInstanceFactKind:       ObservabilitySchemaVersionV1,
	ObservabilityDeclaredFolderFactKind:       ObservabilitySchemaVersionV1,
	ObservabilityDeclaredDashboardFactKind:    ObservabilitySchemaVersionV1,
	ObservabilityDeclaredDatasourceFactKind:   ObservabilitySchemaVersionV1,
	ObservabilityDeclaredAlertRuleFactKind:    ObservabilitySchemaVersionV1,
	ObservabilityDeclaredScrapeConfigFactKind: ObservabilitySchemaVersionV1,
	ObservabilityDeclaredMetricRuleFactKind:   ObservabilitySchemaVersionV1,
	ObservabilityDeclaredMetricRouteFactKind:  ObservabilitySchemaVersionV1,
	ObservabilityDeclaredLogRouteFactKind:     ObservabilitySchemaVersionV1,
	ObservabilityDeclaredTraceRouteFactKind:   ObservabilitySchemaVersionV1,
	ObservabilityAppliedResourceFactKind:      ObservabilitySchemaVersionV1,
	ObservabilityAppliedSyncStateFactKind:     ObservabilitySchemaVersionV1,
	ObservabilityObservedDashboardFactKind:    ObservabilitySchemaVersionV1,
	ObservabilityObservedTargetFactKind:       ObservabilitySchemaVersionV1,
	ObservabilityObservedRuleFactKind:         ObservabilitySchemaVersionV1,
	ObservabilityObservedLogSignalFactKind:    ObservabilitySchemaVersionV1,
	ObservabilityObservedTraceSignalFactKind:  ObservabilitySchemaVersionV1,
	ObservabilityCoverageWarningFactKind:      ObservabilitySchemaVersionV1,
}

// ObservabilityFactKinds returns the accepted observability source fact kinds in
// source-contract order.
func ObservabilityFactKinds() []string {
	return slices.Clone(observabilityFactKinds)
}

// ObservabilitySchemaVersion returns the schema version for an observability
// source fact kind.
func ObservabilitySchemaVersion(factKind string) (string, bool) {
	version, ok := observabilitySchemaVersions[factKind]
	return version, ok
}
