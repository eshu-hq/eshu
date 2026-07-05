// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// AppliedResource is the schema-version-1 typed payload for the 'observability.applied_resource' fact
// kind: one applied observability resource from Argo CD, Kubernetes, or equivalent state. Passthrough lane: no per-kind UID is emitter-guaranteed.
//
// SourceInstanceID is the only required field; every other field is an
// optional pointer the coverage-metadata classifier reads through the
// shared candidate-key fallback chain.
type AppliedResource struct {
	// SourceInstanceID is the collector-derived source instance identity
	// (repo_id:relative_path for the git-declared lane, the live collector's
	// source_instance_id for the observed/applied lane). Required — the one
	// identity field EVERY observability collector injects on EVERY kind in
	// BOTH lanes, so a fact missing it is a malformed emission that
	// dead-letters input_invalid rather than yielding a coverage decision with
	// no source anchor.
	SourceInstanceID string `json:"source_instance_id"`

	// ProviderObjectUID is the live provider object uid; first object-ref candidate. Optional.
	ProviderObjectUID *string `json:"provider_object_uid,omitempty"`

	// DashboardUID is the declared Grafana dashboard uid. Optional.
	DashboardUID *string `json:"dashboard_uid,omitempty"`

	// DatasourceUID is the declared Grafana datasource uid. Optional.
	DatasourceUID *string `json:"datasource_uid,omitempty"`

	// AlertRuleUID is the Grafana alert rule uid (observed_rule identity on the Grafana path). Optional.
	AlertRuleUID *string `json:"alert_rule_uid,omitempty"`

	// FolderUID is the Grafana folder uid. Optional.
	FolderUID *string `json:"folder_uid,omitempty"`

	// ResourceIdentity is an applied-resource identity string. Optional.
	ResourceIdentity *string `json:"resource_identity,omitempty"`

	// ResourceIdentityFingerprint is a redacted applied-resource identity fingerprint. Optional.
	ResourceIdentityFingerprint *string `json:"resource_identity_fingerprint,omitempty"`

	// ResourceName is an applied-resource name. Optional.
	ResourceName *string `json:"resource_name,omitempty"`

	// PipelineName is a declared metric/log/trace pipeline route name. Optional.
	PipelineName *string `json:"pipeline_name,omitempty"`

	// SelectorIdentityFingerprint is a redacted scrape-selector identity fingerprint. Optional.
	SelectorIdentityFingerprint *string `json:"selector_identity_fingerprint,omitempty"`

	// RuleGroup is the metric/log rule group name. Optional.
	RuleGroup *string `json:"rule_group,omitempty"`

	// RuleName is the metric/log rule name. Optional.
	RuleName *string `json:"rule_name,omitempty"`

	// AlertRuleNameFingerprint is a redacted alert-rule name fingerprint. Optional.
	AlertRuleNameFingerprint *string `json:"alert_rule_name_fingerprint,omitempty"`

	// RecordRuleNameFingerprint is a redacted recording-rule name fingerprint. Optional.
	RecordRuleNameFingerprint *string `json:"record_rule_name_fingerprint,omitempty"`

	// RouteDestinationFingerprint is a redacted pipeline route destination fingerprint. Optional.
	RouteDestinationFingerprint *string `json:"route_destination_fingerprint,omitempty"`

	// LabelIdentityFingerprint is a redacted log-signal label identity fingerprint. Optional.
	LabelIdentityFingerprint *string `json:"label_identity_fingerprint,omitempty"`

	// TraceTagIdentityFingerprint is a redacted trace-tag identity fingerprint. Optional.
	TraceTagIdentityFingerprint *string `json:"trace_tag_identity_fingerprint,omitempty"`

	// TagName is the observed Tempo trace tag name. Optional.
	TagName *string `json:"tag_name,omitempty"`

	// SeriesFingerprint is a redacted Loki log series fingerprint. Optional.
	SeriesFingerprint *string `json:"series_fingerprint,omitempty"`

	// AppName is a declared/applied application name. Optional.
	AppName *string `json:"app_name,omitempty"`

	// Provider is the observability provider; when absent the reducer derives it from the fact kind. Optional.
	Provider *string `json:"provider,omitempty"`

	// BackendKind is the declared metric backend kind (prometheus/mimir), read before source_kind for metric routes/rules. Optional.
	BackendKind *string `json:"backend_kind,omitempty"`

	// SourceKind is the source system kind (git/grafana/loki/tempo/prometheus/argocd/...). Optional.
	SourceKind *string `json:"source_kind,omitempty"`

	// SourceClass is the evidence class (declared/applied/observed); derived from the fact kind when absent. Optional.
	SourceClass *string `json:"source_class,omitempty"`

	// ResourceClass is the observability resource class used to derive the coverage signal. Optional.
	ResourceClass *string `json:"resource_class,omitempty"`

	// ObservabilityResourceClass is the applied/warning resource class, read before resource_class. Optional.
	ObservabilityResourceClass *string `json:"observability_resource_class,omitempty"`

	// ResourceKind is the applied Kubernetes/Argo resource kind, a third resource-class fallback. Optional.
	ResourceKind *string `json:"resource_kind,omitempty"`

	// Outcome is the source-local outcome; an absent value reads as "derived". Optional.
	Outcome *string `json:"outcome,omitempty"`

	// FreshnessState is the source freshness; an absent value reads as "unknown". Optional.
	FreshnessState *string `json:"freshness_state,omitempty"`

	// WarningKind is the coverage-warning reason token; also a reason-code and unsupported-signal input. Optional.
	WarningKind *string `json:"warning_kind,omitempty"`

	// DriftCandidateReason is the provider drift reason; a drift reason-code input. Optional.
	DriftCandidateReason *string `json:"drift_candidate_reason,omitempty"`

	// DeclaredMatchState is the declared-vs-observed reconciliation state; a reason-code input. Optional.
	DeclaredMatchState *string `json:"declared_match_state,omitempty"`

	// ServiceHints is a declared service hint; first target-service-ref candidate. Optional.
	ServiceHints *string `json:"service_hints,omitempty"`

	// ServiceRef is a declared service reference; second target-service-ref candidate. Optional.
	ServiceRef *string `json:"service_ref,omitempty"`
}

// AppliedSyncState is the schema-version-1 typed payload for the 'observability.applied_sync_state' fact
// kind: one applied sync/health/permission state for an observability resource. Passthrough lane: no per-kind UID is emitter-guaranteed.
//
// SourceInstanceID is the only required field; every other field is an
// optional pointer the coverage-metadata classifier reads through the
// shared candidate-key fallback chain.
type AppliedSyncState struct {
	// SourceInstanceID is the collector-derived source instance identity
	// (repo_id:relative_path for the git-declared lane, the live collector's
	// source_instance_id for the observed/applied lane). Required — the one
	// identity field EVERY observability collector injects on EVERY kind in
	// BOTH lanes, so a fact missing it is a malformed emission that
	// dead-letters input_invalid rather than yielding a coverage decision with
	// no source anchor.
	SourceInstanceID string `json:"source_instance_id"`

	// ProviderObjectUID is the live provider object uid; first object-ref candidate. Optional.
	ProviderObjectUID *string `json:"provider_object_uid,omitempty"`

	// DashboardUID is the declared Grafana dashboard uid. Optional.
	DashboardUID *string `json:"dashboard_uid,omitempty"`

	// DatasourceUID is the declared Grafana datasource uid. Optional.
	DatasourceUID *string `json:"datasource_uid,omitempty"`

	// AlertRuleUID is the Grafana alert rule uid (observed_rule identity on the Grafana path). Optional.
	AlertRuleUID *string `json:"alert_rule_uid,omitempty"`

	// FolderUID is the Grafana folder uid. Optional.
	FolderUID *string `json:"folder_uid,omitempty"`

	// ResourceIdentity is an applied-resource identity string. Optional.
	ResourceIdentity *string `json:"resource_identity,omitempty"`

	// ResourceIdentityFingerprint is a redacted applied-resource identity fingerprint. Optional.
	ResourceIdentityFingerprint *string `json:"resource_identity_fingerprint,omitempty"`

	// ResourceName is an applied-resource name. Optional.
	ResourceName *string `json:"resource_name,omitempty"`

	// PipelineName is a declared metric/log/trace pipeline route name. Optional.
	PipelineName *string `json:"pipeline_name,omitempty"`

	// SelectorIdentityFingerprint is a redacted scrape-selector identity fingerprint. Optional.
	SelectorIdentityFingerprint *string `json:"selector_identity_fingerprint,omitempty"`

	// RuleGroup is the metric/log rule group name. Optional.
	RuleGroup *string `json:"rule_group,omitempty"`

	// RuleName is the metric/log rule name. Optional.
	RuleName *string `json:"rule_name,omitempty"`

	// AlertRuleNameFingerprint is a redacted alert-rule name fingerprint. Optional.
	AlertRuleNameFingerprint *string `json:"alert_rule_name_fingerprint,omitempty"`

	// RecordRuleNameFingerprint is a redacted recording-rule name fingerprint. Optional.
	RecordRuleNameFingerprint *string `json:"record_rule_name_fingerprint,omitempty"`

	// RouteDestinationFingerprint is a redacted pipeline route destination fingerprint. Optional.
	RouteDestinationFingerprint *string `json:"route_destination_fingerprint,omitempty"`

	// LabelIdentityFingerprint is a redacted log-signal label identity fingerprint. Optional.
	LabelIdentityFingerprint *string `json:"label_identity_fingerprint,omitempty"`

	// TraceTagIdentityFingerprint is a redacted trace-tag identity fingerprint. Optional.
	TraceTagIdentityFingerprint *string `json:"trace_tag_identity_fingerprint,omitempty"`

	// TagName is the observed Tempo trace tag name. Optional.
	TagName *string `json:"tag_name,omitempty"`

	// SeriesFingerprint is a redacted Loki log series fingerprint. Optional.
	SeriesFingerprint *string `json:"series_fingerprint,omitempty"`

	// AppName is a declared/applied application name. Optional.
	AppName *string `json:"app_name,omitempty"`

	// Provider is the observability provider; when absent the reducer derives it from the fact kind. Optional.
	Provider *string `json:"provider,omitempty"`

	// BackendKind is the declared metric backend kind (prometheus/mimir), read before source_kind for metric routes/rules. Optional.
	BackendKind *string `json:"backend_kind,omitempty"`

	// SourceKind is the source system kind (git/grafana/loki/tempo/prometheus/argocd/...). Optional.
	SourceKind *string `json:"source_kind,omitempty"`

	// SourceClass is the evidence class (declared/applied/observed); derived from the fact kind when absent. Optional.
	SourceClass *string `json:"source_class,omitempty"`

	// ResourceClass is the observability resource class used to derive the coverage signal. Optional.
	ResourceClass *string `json:"resource_class,omitempty"`

	// ObservabilityResourceClass is the applied/warning resource class, read before resource_class. Optional.
	ObservabilityResourceClass *string `json:"observability_resource_class,omitempty"`

	// ResourceKind is the applied Kubernetes/Argo resource kind, a third resource-class fallback. Optional.
	ResourceKind *string `json:"resource_kind,omitempty"`

	// Outcome is the source-local outcome; an absent value reads as "derived". Optional.
	Outcome *string `json:"outcome,omitempty"`

	// FreshnessState is the source freshness; an absent value reads as "unknown". Optional.
	FreshnessState *string `json:"freshness_state,omitempty"`

	// WarningKind is the coverage-warning reason token; also a reason-code and unsupported-signal input. Optional.
	WarningKind *string `json:"warning_kind,omitempty"`

	// DriftCandidateReason is the provider drift reason; a drift reason-code input. Optional.
	DriftCandidateReason *string `json:"drift_candidate_reason,omitempty"`

	// DeclaredMatchState is the declared-vs-observed reconciliation state; a reason-code input. Optional.
	DeclaredMatchState *string `json:"declared_match_state,omitempty"`

	// ServiceHints is a declared service hint; first target-service-ref candidate. Optional.
	ServiceHints *string `json:"service_hints,omitempty"`

	// ServiceRef is a declared service reference; second target-service-ref candidate. Optional.
	ServiceRef *string `json:"service_ref,omitempty"`
}
