// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"testing"

	incidentv1 "github.com/eshu-hq/eshu-sdk/factschema/incident/v1"
	kuberneteslivev1 "github.com/eshu-hq/eshu-sdk/factschema/kuberneteslive/v1"
	observabilityv1 "github.com/eshu-hq/eshu-sdk/factschema/observability/v1"
	securityalertv1 "github.com/eshu-hq/eshu-sdk/factschema/securityalert/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// opsDecodeCases covers the operations seams (observability declared/
// applied/observed, kubernetes-live, incident routing, security alerts).
func opsDecodeCases() []decodeCase {
	instance := map[string]any{"source_instance_id": "grafana-prod"}
	observed := map[string]any{"source_instance_id": "prom-prod", "provider_object_uid": "uid-99"}
	return []decodeCase{
		{
			name: "ObservabilityDeclaredFolder", kind: "observability.declared_folder", target: &observabilityv1.DeclaredFolder{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityDeclaredFolder(env) },
			requiredField: "source_instance_id", spotFields: instance,
		},
		{
			name: "ObservabilityDeclaredDashboard", kind: "observability.declared_dashboard", target: &observabilityv1.DeclaredDashboard{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityDeclaredDashboard(env) },
			requiredField: "source_instance_id", spotFields: instance,
		},
		{
			name: "ObservabilityDeclaredDatasource", kind: "observability.declared_datasource", target: &observabilityv1.DeclaredDatasource{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityDeclaredDatasource(env) },
			requiredField: "source_instance_id", spotFields: instance,
		},
		{
			name: "ObservabilityDeclaredAlertRule", kind: "observability.declared_alert_rule", target: &observabilityv1.DeclaredAlertRule{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityDeclaredAlertRule(env) },
			requiredField: "source_instance_id", spotFields: instance,
		},
		{
			name: "ObservabilityDeclaredScrapeConfig", kind: "observability.declared_scrape_config", target: &observabilityv1.DeclaredScrapeConfig{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityDeclaredScrapeConfig(env) },
			requiredField: "source_instance_id", spotFields: instance,
		},
		{
			name: "ObservabilityDeclaredMetricRule", kind: "observability.declared_metric_rule", target: &observabilityv1.DeclaredMetricRule{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityDeclaredMetricRule(env) },
			requiredField: "source_instance_id", spotFields: instance,
		},
		{
			name: "ObservabilityDeclaredMetricRoute", kind: "observability.declared_metric_route", target: &observabilityv1.DeclaredMetricRoute{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityDeclaredMetricRoute(env) },
			requiredField: "source_instance_id", spotFields: instance,
		},
		{
			name: "ObservabilityDeclaredLogRoute", kind: "observability.declared_log_route", target: &observabilityv1.DeclaredLogRoute{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityDeclaredLogRoute(env) },
			requiredField: "source_instance_id", spotFields: instance,
		},
		{
			name: "ObservabilityDeclaredTraceRoute", kind: "observability.declared_trace_route", target: &observabilityv1.DeclaredTraceRoute{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityDeclaredTraceRoute(env) },
			requiredField: "source_instance_id", spotFields: instance,
		},
		{
			name: "ObservabilityAppliedResource", kind: "observability.applied_resource", target: &observabilityv1.AppliedResource{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityAppliedResource(env) },
			requiredField: "source_instance_id", spotFields: instance,
		},
		{
			name: "ObservabilityAppliedSyncState", kind: "observability.applied_sync_state", target: &observabilityv1.AppliedSyncState{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityAppliedSyncState(env) },
			requiredField: "source_instance_id", spotFields: instance,
		},
		{
			name: "ObservabilityObservedDashboard", kind: "observability.observed_dashboard", target: &observabilityv1.ObservedDashboard{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityObservedDashboard(env) },
			requiredField: "provider_object_uid", spotFields: observed,
		},
		{
			name: "ObservabilityObservedTarget", kind: "observability.observed_target", target: &observabilityv1.ObservedTarget{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityObservedTarget(env) },
			requiredField: "source_instance_id", spotFields: observed,
		},
		{
			name: "ObservabilityObservedRule", kind: "observability.observed_rule", target: &observabilityv1.ObservedRule{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityObservedRule(env) },
			requiredField: "source_instance_id", spotFields: instance,
		},
		{
			name: "ObservabilityObservedLogSignal", kind: "observability.observed_log_signal", target: &observabilityv1.ObservedLogSignal{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityObservedLogSignal(env) },
			requiredField: "provider_object_uid", spotFields: observed,
		},
		{
			name: "ObservabilityObservedTraceSignal", kind: "observability.observed_trace_signal", target: &observabilityv1.ObservedTraceSignal{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityObservedTraceSignal(env) },
			requiredField: "source_instance_id", spotFields: observed,
		},
		{
			name: "ObservabilityCoverageWarning", kind: "observability.coverage_warning", target: &observabilityv1.CoverageWarning{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeObservabilityCoverageWarning(env) },
			requiredField: "source_instance_id", spotFields: instance,
		},
		{
			name: "KubernetesLivePodTemplate", kind: "kubernetes_live.pod_template", target: &kuberneteslivev1.PodTemplate{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeKubernetesLivePodTemplate(env) },
			requiredField: "object_id",
			spotFields:    map[string]any{"object_id": "apps/v1:Deployment:prod:api"},
		},
		{
			name: "KubernetesLiveRelationship", kind: "kubernetes_live.relationship", target: &kuberneteslivev1.Relationship{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeKubernetesLiveRelationship(env) },
			requiredField: "to_object_id",
			spotFields:    map[string]any{"relationship_type": "OWNS", "from_object_id": "apps/v1:Deployment:prod:api"},
		},
		{
			name: "KubernetesLiveWarning", kind: "kubernetes_live.warning", target: &kuberneteslivev1.Warning{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeKubernetesLiveWarning(env) },
			requiredField: "reason",
			spotFields:    map[string]any{"reason": "stale_snapshot", "cluster_id": "prod-eu"},
		},
		{
			name: "KubernetesLiveNamespace", kind: "kubernetes_live.namespace", target: &kuberneteslivev1.Namespace{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeKubernetesLiveNamespace(env) },
			requiredField: "object_id",
			spotFields:    map[string]any{"object_id": "v1:Namespace::prod"},
		},
		{
			name: "IncidentRecord", kind: "incident.record", target: &incidentv1.IncidentRecord{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeIncidentRecord(env) },
			requiredField: "provider_incident_id",
			spotFields:    map[string]any{"provider": "pagerduty", "provider_incident_id": "INC-4242"},
		},
		{
			name: "IncidentRoutingAppliedPagerDutyResource", kind: "incident_routing.applied_pagerduty_resource", target: &incidentv1.AppliedPagerDutyResource{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeIncidentRoutingAppliedPagerDutyResource(env) },
			requiredField: "resource_name",
			spotFields:    map[string]any{"source_class": "terraform", "outcome": "applied"},
		},
		{
			name: "IncidentRoutingObservedPagerDutyService", kind: "incident_routing.observed_pagerduty_service", target: &incidentv1.ObservedPagerDutyService{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeIncidentRoutingObservedPagerDutyService(env) },
			requiredField: "service_id",
			spotFields:    map[string]any{"provider": "pagerduty", "service_id": "PSVC-1"},
		},
		{
			name: "IncidentRoutingCoverageWarning", kind: "incident_routing.coverage_warning", target: &incidentv1.CoverageWarning{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeIncidentRoutingCoverageWarning(env) },
			requiredField: "reason",
			spotFields:    map[string]any{"source_class": "terraform", "reason": "missing_integration"},
		},
		{
			name: "SecurityAlertRepositoryAlert", kind: "security_alert.repository_alert", target: &securityalertv1.RepositoryAlert{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeSecurityAlertRepositoryAlert(env) },
			requiredField: "repository_id",
			spotFields:    map[string]any{"repository_id": "github.com/example/repo"},
		},
	}
}

func TestOpsDecodeSeams(t *testing.T) {
	t.Parallel()
	runDecodeCases(t, opsDecodeCases())
}
