// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

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

// ObservabilityFamilyDeclaration is the typed fact-family contract declaration for the
// observability family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var ObservabilityFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "observability",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "observability_coverage_correlation",
	ProjectionHook:         "observability_coverage_correlation",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/observability/coverage/correlations",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "observability.applied_resource", PayloadSchema: "sdk/go/factschema/schema/observability.applied_resource.v1.schema.json"},
		{Kind: "observability.applied_sync_state", PayloadSchema: "sdk/go/factschema/schema/observability.applied_sync_state.v1.schema.json"},
		{Kind: "observability.coverage_warning", PayloadSchema: "sdk/go/factschema/schema/observability.coverage_warning.v1.schema.json"},
		{Kind: "observability.declared_alert_rule", PayloadSchema: "sdk/go/factschema/schema/observability.declared_alert_rule.v1.schema.json"},
		{Kind: "observability.declared_dashboard", PayloadSchema: "sdk/go/factschema/schema/observability.declared_dashboard.v1.schema.json"},
		{Kind: "observability.declared_datasource", PayloadSchema: "sdk/go/factschema/schema/observability.declared_datasource.v1.schema.json"},
		{Kind: "observability.declared_folder", PayloadSchema: "sdk/go/factschema/schema/observability.declared_folder.v1.schema.json"},
		{Kind: "observability.declared_log_route", PayloadSchema: "sdk/go/factschema/schema/observability.declared_log_route.v1.schema.json"},
		{Kind: "observability.declared_metric_route", PayloadSchema: "sdk/go/factschema/schema/observability.declared_metric_route.v1.schema.json"},
		{Kind: "observability.declared_metric_rule", PayloadSchema: "sdk/go/factschema/schema/observability.declared_metric_rule.v1.schema.json"},
		{Kind: "observability.declared_scrape_config", PayloadSchema: "sdk/go/factschema/schema/observability.declared_scrape_config.v1.schema.json"},
		{Kind: "observability.declared_trace_route", PayloadSchema: "sdk/go/factschema/schema/observability.declared_trace_route.v1.schema.json"},
		{Kind: "observability.observed_dashboard", PayloadSchema: "sdk/go/factschema/schema/observability.observed_dashboard.v1.schema.json"},
		{Kind: "observability.observed_log_signal", PayloadSchema: "sdk/go/factschema/schema/observability.observed_log_signal.v1.schema.json"},
		{Kind: "observability.observed_rule", PayloadSchema: "sdk/go/factschema/schema/observability.observed_rule.v1.schema.json"},
		{Kind: "observability.observed_target", PayloadSchema: "sdk/go/factschema/schema/observability.observed_target.v1.schema.json"},
		{Kind: "observability.observed_trace_signal", PayloadSchema: "sdk/go/factschema/schema/observability.observed_trace_signal.v1.schema.json"},
		{Kind: "observability.source_instance", PayloadSchema: "sdk/go/factschema/schema/observability.source_instance.v1.schema.json"},
	},
	Producers: []contract.Producer{
		{ID: "collector-grafana", Repository: "eshu-hq/eshu", Path: "go/internal/collector/observability/grafana", PlannedRepository: "eshu-hq/eshu-collector-grafana", AuthorityMode: "core_issued_grant", Kinds: []string{"observability.observed_dashboard"}},
		{ID: "collector-loki", Repository: "eshu-hq/eshu", Path: "go/internal/collector/observability/loki", PlannedRepository: "eshu-hq/eshu-collector-loki", AuthorityMode: "core_issued_grant", Kinds: []string{"observability.coverage_warning", "observability.observed_log_signal", "observability.observed_rule", "observability.source_instance"}},
		{ID: "collector-prometheus-mimir", Repository: "eshu-hq/eshu", Path: "go/internal/collector/observability/prometheus", PlannedRepository: "eshu-hq/eshu-collector-prometheus-mimir", AuthorityMode: "core_issued_grant", Kinds: []string{"observability.coverage_warning", "observability.observed_rule", "observability.observed_target", "observability.source_instance"}},
		{ID: "collector-tempo", Repository: "eshu-hq/eshu-collector-tempo", Path: ".", PlannedRepository: "", AuthorityMode: "core_issued_grant", Kinds: []string{"observability.coverage_warning", "observability.observed_trace_signal", "observability.source_instance"}},
		{ID: "git-core", Repository: "eshu-hq/eshu", Path: "go/internal/collector/repo", AuthorityMode: "core_owned", Kinds: []string{"observability.applied_resource", "observability.applied_sync_state", "observability.declared_alert_rule", "observability.declared_dashboard", "observability.declared_datasource", "observability.declared_folder", "observability.declared_log_route", "observability.declared_metric_route", "observability.declared_metric_rule", "observability.declared_scrape_config", "observability.declared_trace_route"}},
	},
})
