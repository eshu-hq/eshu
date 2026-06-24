// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type observabilityMetadataEvidence struct {
	provider       string
	coverageSignal string
	objectRef      string
	targetService  string
	sourceClass    string
	sourceKind     string
	sourceOutcome  string
	resourceClass  string
	freshnessState string
	reasonCode     string
	factID         string
}

func classifyObservabilityMetadataEvidence(
	envelopes []facts.Envelope,
) []ObservabilityCoverageCorrelationDecision {
	groups := make(map[string][]observabilityMetadataEvidence)
	for _, envelope := range envelopes {
		evidence, ok := observabilityMetadataEvidenceFromEnvelope(envelope)
		if !ok {
			continue
		}
		key := strings.Join([]string{evidence.provider, evidence.coverageSignal, evidence.objectRef}, "\x00")
		groups[key] = append(groups[key], evidence)
	}

	decisions := make([]ObservabilityCoverageCorrelationDecision, 0, len(groups))
	for _, evidence := range groups {
		decisions = append(decisions, classifyObservabilityMetadataGroup(evidence))
	}
	sortObservabilityCoverageDecisions(decisions)
	return decisions
}

func observabilityMetadataEvidenceFromEnvelope(
	envelope facts.Envelope,
) (observabilityMetadataEvidence, bool) {
	if _, ok := facts.ObservabilitySchemaVersion(envelope.FactKind); !ok ||
		envelope.FactKind == facts.ObservabilitySourceInstanceFactKind {
		return observabilityMetadataEvidence{}, false
	}
	provider := observabilityMetadataProvider(envelope.FactKind, envelope.Payload)
	signal := observabilityMetadataCoverageSignal(envelope.FactKind, envelope.Payload)
	objectRef := observabilityMetadataObjectRef(envelope)
	if provider == "" || signal == "" || objectRef == "" {
		return observabilityMetadataEvidence{}, false
	}
	return observabilityMetadataEvidence{
		provider:       provider,
		coverageSignal: signal,
		objectRef:      objectRef,
		targetService: firstNonBlank(
			payloadString(envelope.Payload, "service_hints"),
			payloadString(envelope.Payload, "service_ref"),
		),
		sourceClass:   normalizedObservabilitySourceClass(envelope.FactKind, envelope.Payload),
		sourceKind:    firstNonBlank(payloadString(envelope.Payload, "source_kind"), provider),
		sourceOutcome: normalizedObservabilitySourceOutcome(envelope.Payload),
		resourceClass: firstNonBlank(
			payloadString(envelope.Payload, "resource_class"),
			payloadString(envelope.Payload, "observability_resource_class"),
			signal,
		),
		freshnessState: normalizedObservabilityFreshness(envelope.Payload),
		reasonCode: firstNonBlank(
			payloadString(envelope.Payload, "warning_kind"),
			payloadString(envelope.Payload, "drift_candidate_reason"),
			payloadString(envelope.Payload, "declared_match_state"),
		),
		factID: envelope.FactID,
	}, true
}

func classifyObservabilityMetadataGroup(
	evidence []observabilityMetadataEvidence,
) ObservabilityCoverageCorrelationDecision {
	sourceClasses := make([]string, 0, len(evidence))
	sourceKinds := make([]string, 0, len(evidence))
	sourceOutcomes := make([]string, 0, len(evidence))
	evidenceFactIDs := make([]string, 0, len(evidence))
	freshness := "current"
	targetService := ""
	resourceClass := ""
	decision := ObservabilityCoverageCorrelationDecision{
		Provider:               evidence[0].provider,
		CoverageSignal:         evidence[0].coverageSignal,
		ObservabilityObjectRef: evidence[0].objectRef,
		ProvenanceOnly:         true,
		ResolutionMode:         "metadata_identity",
	}

	state := ObservabilityCoverageExact
	for _, item := range evidence {
		sourceClasses = append(sourceClasses, item.sourceClass)
		sourceKinds = append(sourceKinds, item.sourceKind)
		sourceOutcomes = append(sourceOutcomes, item.sourceOutcome)
		evidenceFactIDs = append(evidenceFactIDs, item.factID)
		targetService = firstNonBlank(targetService, item.targetService)
		resourceClass = firstNonBlank(resourceClass, item.resourceClass)
		freshness = worseObservabilityFreshness(freshness, item.freshnessState)
		state = worseObservabilityOutcome(state, metadataOutcome(item))
	}

	decision.SourceClasses = uniqueSortedStrings(sourceClasses)
	decision.SourceClass = collapsedObservabilityValue(decision.SourceClasses)
	decision.SourceKinds = uniqueSortedStrings(sourceKinds)
	decision.SourceKind = collapsedObservabilityValue(decision.SourceKinds)
	decision.SourceOutcomes = uniqueSortedStrings(sourceOutcomes)
	decision.SourceOutcome = collapsedObservabilityValue(decision.SourceOutcomes)
	decision.EvidenceFactIDs = uniqueSortedStrings(evidenceFactIDs)
	decision.TargetServiceRef = targetService
	decision.ResourceClass = resourceClass
	decision.FreshnessState = freshness
	decision.Outcome = state
	decision.CoverageStatus = observabilityMetadataCoverageStatus(state)
	decision.Reason = observabilityMetadataReason(state)
	return decision
}

func metadataOutcome(item observabilityMetadataEvidence) ObservabilityCoverageCorrelationOutcome {
	if item.freshnessState == "permission_hidden" || item.sourceOutcome == "permission_hidden" ||
		item.reasonCode == "permission_hidden" {
		return ObservabilityCoveragePermissionHidden
	}
	if item.sourceOutcome == "rejected" || item.sourceOutcome == "unsupported" ||
		item.reasonCode == "high_cardinality_rejected" {
		return ObservabilityCoverageRejected
	}
	if item.reasonCode == "manual_provider_resource" || item.sourceOutcome == "drifted" {
		return ObservabilityCoverageDrifted
	}
	if item.freshnessState == "stale" || item.sourceOutcome == "stale" {
		return ObservabilityCoverageStale
	}
	switch item.sourceOutcome {
	case "ambiguous":
		return ObservabilityCoverageAmbiguous
	case "unresolved", "partial":
		return ObservabilityCoverageUnresolved
	case "derived":
		return ObservabilityCoverageDerived
	default:
		return ObservabilityCoverageExact
	}
}

func worseObservabilityOutcome(
	current ObservabilityCoverageCorrelationOutcome,
	candidate ObservabilityCoverageCorrelationOutcome,
) ObservabilityCoverageCorrelationOutcome {
	if observabilityOutcomeRank(candidate) > observabilityOutcomeRank(current) {
		return candidate
	}
	return current
}

func observabilityOutcomeRank(outcome ObservabilityCoverageCorrelationOutcome) int {
	switch outcome {
	case ObservabilityCoveragePermissionHidden:
		return 8
	case ObservabilityCoverageRejected:
		return 7
	case ObservabilityCoverageDrifted:
		return 6
	case ObservabilityCoverageStale:
		return 5
	case ObservabilityCoverageAmbiguous:
		return 4
	case ObservabilityCoverageUnresolved:
		return 3
	case ObservabilityCoverageDerived:
		return 2
	case ObservabilityCoverageExact:
		return 1
	default:
		return 0
	}
}

func observabilityMetadataCoverageStatus(outcome ObservabilityCoverageCorrelationOutcome) string {
	switch outcome {
	case ObservabilityCoverageExact, ObservabilityCoverageDerived:
		return "covered"
	case ObservabilityCoverageDrifted:
		return "drifted"
	case ObservabilityCoveragePermissionHidden:
		return "permission_hidden"
	case ObservabilityCoverageStale:
		return "stale"
	case ObservabilityCoverageAmbiguous:
		return "ambiguous"
	case ObservabilityCoverageRejected:
		return "rejected"
	default:
		return "gap"
	}
}

func observabilityMetadataReason(outcome ObservabilityCoverageCorrelationOutcome) string {
	switch outcome {
	case ObservabilityCoverageExact:
		return "observability metadata identity is present and current"
	case ObservabilityCoverageDerived:
		return "observability metadata identity is derived from source configuration"
	case ObservabilityCoverageAmbiguous:
		return "observability metadata identity is ambiguous"
	case ObservabilityCoverageUnresolved:
		return "observability metadata source is partial or unresolved"
	case ObservabilityCoverageStale:
		return "observability metadata is stale"
	case ObservabilityCoverageRejected:
		return "observability metadata was rejected or unsupported"
	case ObservabilityCoverageDrifted:
		return "observability metadata indicates declared, applied, or observed drift"
	case ObservabilityCoveragePermissionHidden:
		return "observability metadata is hidden by source permissions"
	default:
		return "observability metadata outcome is unknown"
	}
}

func observabilityMetadataProvider(factKind string, payload map[string]any) string {
	if provider := payloadString(payload, "provider"); provider != "" {
		return provider
	}
	switch factKind {
	case facts.ObservabilityDeclaredFolderFactKind,
		facts.ObservabilityDeclaredDashboardFactKind,
		facts.ObservabilityDeclaredDatasourceFactKind,
		facts.ObservabilityDeclaredAlertRuleFactKind,
		facts.ObservabilityObservedDashboardFactKind:
		return "grafana"
	case facts.ObservabilityDeclaredLogRouteFactKind,
		facts.ObservabilityObservedLogSignalFactKind:
		return "loki"
	case facts.ObservabilityDeclaredTraceRouteFactKind,
		facts.ObservabilityObservedTraceSignalFactKind:
		return "tempo"
	case facts.ObservabilityDeclaredMetricRouteFactKind:
		return firstNonBlank(payloadString(payload, "backend_kind"), "prometheus")
	case facts.ObservabilityDeclaredScrapeConfigFactKind,
		facts.ObservabilityDeclaredMetricRuleFactKind,
		facts.ObservabilityObservedTargetFactKind,
		facts.ObservabilityObservedRuleFactKind:
		return firstNonBlank(payloadString(payload, "backend_kind"), payloadString(payload, "source_kind"), "prometheus")
	default:
		return firstNonBlank(payloadString(payload, "backend_kind"), payloadString(payload, "source_kind"))
	}
}

func observabilityMetadataCoverageSignal(factKind string, payload map[string]any) string {
	switch factKind {
	case facts.ObservabilityDeclaredFolderFactKind:
		return "folder"
	case facts.ObservabilityDeclaredDashboardFactKind:
		return "dashboard"
	case facts.ObservabilityDeclaredDatasourceFactKind:
		return "datasource"
	case facts.ObservabilityDeclaredAlertRuleFactKind:
		return "alert_rule"
	case facts.ObservabilityObservedDashboardFactKind:
		return observabilitySignalFromResourceClass(payloadString(payload, "resource_class"))
	case facts.ObservabilityDeclaredScrapeConfigFactKind,
		facts.ObservabilityObservedTargetFactKind:
		return "scrape_target"
	case facts.ObservabilityDeclaredMetricRuleFactKind,
		facts.ObservabilityObservedRuleFactKind:
		if signal := observabilitySignalFromResourceClass(payloadString(payload, "resource_class")); signal != "" {
			return signal
		}
		return "rule"
	case facts.ObservabilityDeclaredMetricRouteFactKind:
		return "metric_route"
	case facts.ObservabilityDeclaredLogRouteFactKind:
		return "log_route"
	case facts.ObservabilityDeclaredTraceRouteFactKind:
		return "trace_route"
	case facts.ObservabilityObservedLogSignalFactKind:
		return "log_signal"
	case facts.ObservabilityObservedTraceSignalFactKind:
		return "trace_signal"
	case facts.ObservabilityAppliedResourceFactKind,
		facts.ObservabilityAppliedSyncStateFactKind,
		facts.ObservabilityCoverageWarningFactKind:
		signal := observabilitySignalFromResourceClass(firstNonBlank(
			payloadString(payload, "observability_resource_class"),
			payloadString(payload, "resource_class"),
			payloadString(payload, "resource_kind"),
		))
		if signal != "" {
			return signal
		}
		if factKind == facts.ObservabilityCoverageWarningFactKind &&
			observabilityMetadataUnsupported(payload) {
			return "unsupported"
		}
		return ""
	default:
		return ""
	}
}

func observabilitySignalFromResourceClass(resourceClass string) string {
	switch strings.TrimSpace(resourceClass) {
	case "scrape_config", "ServiceMonitor", "PodMonitor", "ScrapeConfig", "target":
		return "scrape_target"
	case "metric_rule", "PrometheusRule", "rule":
		return "rule"
	case "alert_rule", "GrafanaAlertRule":
		return "alert_rule"
	case "log_route":
		return "log_route"
	case "trace_route":
		return "trace_route"
	case "log_signal":
		return "log_signal"
	case "trace_signal":
		return "trace_signal"
	case "dashboard", "GrafanaDashboard":
		return "dashboard"
	case "datasource", "GrafanaDatasource":
		return "datasource"
	case "folder", "GrafanaFolder":
		return "folder"
	default:
		return ""
	}
}

func observabilityMetadataUnsupported(payload map[string]any) bool {
	switch payloadString(payload, "outcome") {
	case "unsupported", "rejected":
		return true
	}
	switch payloadString(payload, "warning_kind") {
	case "unsupported_resource_kind", "unsupported", "high_cardinality_rejected":
		return true
	default:
		return false
	}
}

func observabilityMetadataObjectRef(envelope facts.Envelope) string {
	payload := envelope.Payload
	for _, key := range []string{
		"provider_object_uid",
		"dashboard_uid",
		"datasource_uid",
		"alert_rule_uid",
		"folder_uid",
		"resource_identity",
		"resource_identity_fingerprint",
		"resource_name",
		"pipeline_name",
		"selector_identity_fingerprint",
		"rule_group",
		"rule_name",
		"alert_rule_name_fingerprint",
		"record_rule_name_fingerprint",
		"route_destination_fingerprint",
		"label_identity_fingerprint",
		"trace_tag_identity_fingerprint",
		"tag_name",
		"series_fingerprint",
		"app_name",
	} {
		if value := payloadString(payload, key); value != "" {
			return value
		}
	}
	return envelope.StableFactKey
}

func normalizedObservabilitySourceClass(factKind string, payload map[string]any) string {
	switch value := strings.TrimSpace(payloadString(payload, "source_class")); value {
	case "declared", "applied", "observed":
		return value
	}
	switch factKind {
	case facts.ObservabilityAppliedResourceFactKind, facts.ObservabilityAppliedSyncStateFactKind:
		return "applied"
	case facts.ObservabilityObservedDashboardFactKind,
		facts.ObservabilityObservedTargetFactKind,
		facts.ObservabilityObservedRuleFactKind,
		facts.ObservabilityObservedLogSignalFactKind,
		facts.ObservabilityObservedTraceSignalFactKind:
		return "observed"
	default:
		return "declared"
	}
}

func normalizedObservabilitySourceOutcome(payload map[string]any) string {
	switch value := strings.TrimSpace(payloadString(payload, "outcome")); value {
	case "":
		return "derived"
	default:
		return value
	}
}

func normalizedObservabilityFreshness(payload map[string]any) string {
	switch value := strings.TrimSpace(payloadString(payload, "freshness_state")); value {
	case "":
		return "unknown"
	default:
		return value
	}
}

func worseObservabilityFreshness(current string, candidate string) string {
	if observabilityFreshnessRank(candidate) > observabilityFreshnessRank(current) {
		return candidate
	}
	return current
}

func observabilityFreshnessRank(value string) int {
	switch value {
	case "permission_hidden":
		return 4
	case "stale":
		return 3
	case "unknown":
		return 2
	case "current":
		return 1
	default:
		return 0
	}
}

func collapsedObservabilityValue(values []string) string {
	values = uniqueSortedStrings(values)
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	default:
		return "mixed"
	}
}
