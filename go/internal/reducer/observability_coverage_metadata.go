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

// classifyObservabilityMetadataEvidence groups the scope generation's
// observability declared/applied/observed facts by (provider, coverage_signal,
// object_ref) and classifies each group into one coverage decision. Each fact is
// decoded through the typed contracts seam (decodeObservabilityMetadataView): a
// fact missing its required source_instance_id (or provider_object_uid for the
// four observed kinds that require it) is quarantined per-fact as an
// input_invalid dead-letter via partitionDecodeFailures and skipped, while every
// valid fact in the batch still classifies — the same fault-isolation contract
// the AWS/GCP/incident families established. A non-input_invalid error is fatal
// to the intent.
func classifyObservabilityMetadataEvidence(
	envelopes []facts.Envelope,
) ([]ObservabilityCoverageCorrelationDecision, []quarantinedFact, error) {
	groups := make(map[string][]observabilityMetadataEvidence)
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		evidence, ok, err := observabilityMetadataEvidenceFromEnvelope(envelope)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(envelope, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
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
	return decisions, quarantined, nil
}

// observabilityMetadataEvidenceFromEnvelope decodes one observability fact
// through the typed seam and projects it into the classifier's evidence record.
// It returns (evidence, true, nil) for a usable metadata fact, (zero, false,
// nil) for a fact the classifier intentionally skips (the source_instance kind,
// or a fact whose provider/signal/object-ref could not be derived — today's
// silent-skip behavior for those, preserved byte-for-byte), and (zero, false,
// err) when the typed decode fails so the caller can quarantine or fail.
func observabilityMetadataEvidenceFromEnvelope(
	envelope facts.Envelope,
) (observabilityMetadataEvidence, bool, error) {
	if _, ok := facts.ObservabilitySchemaVersion(envelope.FactKind); !ok ||
		envelope.FactKind == facts.ObservabilitySourceInstanceFactKind {
		return observabilityMetadataEvidence{}, false, nil
	}
	view, err := decodeObservabilityMetadataView(envelope)
	if err != nil {
		return observabilityMetadataEvidence{}, false, err
	}
	provider := observabilityMetadataProvider(envelope.FactKind, view)
	signal := observabilityMetadataCoverageSignal(envelope.FactKind, view)
	objectRef := observabilityMetadataObjectRef(envelope, view)
	if provider == "" || signal == "" || objectRef == "" {
		return observabilityMetadataEvidence{}, false, nil
	}
	return observabilityMetadataEvidence{
		provider:       provider,
		coverageSignal: signal,
		objectRef:      objectRef,
		targetService: firstNonBlank(
			view.serviceHints,
			view.serviceRef,
		),
		sourceClass:   normalizedObservabilitySourceClass(envelope.FactKind, view),
		sourceKind:    firstNonBlank(view.sourceKind, provider),
		sourceOutcome: normalizedObservabilitySourceOutcome(view),
		resourceClass: firstNonBlank(
			view.resourceClass,
			view.observabilityResourceClass,
			signal,
		),
		freshnessState: normalizedObservabilityFreshness(view),
		reasonCode: firstNonBlank(
			view.warningKind,
			view.driftCandidateReason,
			view.declaredMatchState,
		),
		factID: envelope.FactID,
	}, true, nil
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

func observabilityMetadataProvider(factKind string, view observabilityMetadataView) string {
	if view.provider != "" {
		return view.provider
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
		return firstNonBlank(view.backendKind, "prometheus")
	case facts.ObservabilityDeclaredScrapeConfigFactKind,
		facts.ObservabilityDeclaredMetricRuleFactKind,
		facts.ObservabilityObservedTargetFactKind,
		facts.ObservabilityObservedRuleFactKind:
		return firstNonBlank(view.backendKind, view.sourceKind, "prometheus")
	default:
		return firstNonBlank(view.backendKind, view.sourceKind)
	}
}

func observabilityMetadataCoverageSignal(factKind string, view observabilityMetadataView) string {
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
		return observabilitySignalFromResourceClass(view.resourceClass)
	case facts.ObservabilityDeclaredScrapeConfigFactKind,
		facts.ObservabilityObservedTargetFactKind:
		return "scrape_target"
	case facts.ObservabilityDeclaredMetricRuleFactKind,
		facts.ObservabilityObservedRuleFactKind:
		if signal := observabilitySignalFromResourceClass(view.resourceClass); signal != "" {
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
			view.observabilityResourceClass,
			view.resourceClass,
			view.resourceKind,
		))
		if signal != "" {
			return signal
		}
		if factKind == facts.ObservabilityCoverageWarningFactKind &&
			observabilityMetadataUnsupported(view) {
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

func observabilityMetadataUnsupported(view observabilityMetadataView) bool {
	switch view.outcome {
	case "unsupported", "rejected":
		return true
	}
	switch view.warningKind {
	case "unsupported_resource_kind", "unsupported", "high_cardinality_rejected":
		return true
	default:
		return false
	}
}

// observabilityMetadataObjectRef returns the first non-empty object-ref
// candidate in the view's fixed priority order, falling back to the envelope's
// StableFactKey (an envelope-level identity that is always present) when every
// candidate is empty — byte-identical to the pre-typing raw-payload fallback
// chain.
func observabilityMetadataObjectRef(envelope facts.Envelope, view observabilityMetadataView) string {
	for _, value := range view.objectRefCandidates() {
		if value != "" {
			return value
		}
	}
	return envelope.StableFactKey
}

func normalizedObservabilitySourceClass(factKind string, view observabilityMetadataView) string {
	switch value := strings.TrimSpace(view.sourceClass); value {
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

func normalizedObservabilitySourceOutcome(view observabilityMetadataView) string {
	switch value := strings.TrimSpace(view.outcome); value {
	case "":
		return "derived"
	default:
		return value
	}
}

func normalizedObservabilityFreshness(view observabilityMetadataView) string {
	switch value := strings.TrimSpace(view.freshnessState); value {
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
