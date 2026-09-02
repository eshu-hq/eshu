// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package obscoverage

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// benchObservabilityMetadataBatch builds a representative batch of observability
// metadata facts across both lanes and every kind the coverage-metadata
// classifier decodes, so the benchmark measures the typed-decode cost on a
// realistic mix rather than one kind.
func benchObservabilityMetadataBatch(perKind int) []facts.Envelope {
	kinds := []string{
		facts.ObservabilityDeclaredFolderFactKind,
		facts.ObservabilityDeclaredDashboardFactKind,
		facts.ObservabilityDeclaredDatasourceFactKind,
		facts.ObservabilityDeclaredAlertRuleFactKind,
		facts.ObservabilityDeclaredScrapeConfigFactKind,
		facts.ObservabilityDeclaredMetricRuleFactKind,
		facts.ObservabilityDeclaredMetricRouteFactKind,
		facts.ObservabilityDeclaredLogRouteFactKind,
		facts.ObservabilityDeclaredTraceRouteFactKind,
		facts.ObservabilityAppliedResourceFactKind,
		facts.ObservabilityAppliedSyncStateFactKind,
		facts.ObservabilityObservedDashboardFactKind,
		facts.ObservabilityObservedTargetFactKind,
		facts.ObservabilityObservedRuleFactKind,
		facts.ObservabilityObservedLogSignalFactKind,
		facts.ObservabilityObservedTraceSignalFactKind,
		facts.ObservabilityCoverageWarningFactKind,
	}
	var out []facts.Envelope
	for _, kind := range kinds {
		for i := 0; i < perKind; i++ {
			id := kind + ":" + strings.Repeat("x", i%3)
			payload := map[string]any{
				"source_instance_id":   "source-instance",
				"source_class":         "observed",
				"source_kind":          "prometheus",
				"provider":             "prometheus",
				"resource_class":       "target",
				"provider_object_uid":  id,
				"outcome":              "observed",
				"freshness_state":      "current",
				"declared_match_state": "not_compared",
			}
			out = append(out, facts.Envelope{
				FactID:        id,
				FactKind:      kind,
				SchemaVersion: "1.0.0",
				StableFactKey: id,
				Payload:       payload,
				ObservedAt:    time.Unix(0, 0),
			})
		}
	}
	return out
}

// BenchmarkObservabilityMetadataTypedDecode measures the production typed-decode
// classifier path (classifyObservabilityMetadataEvidence -> the contracts seam).
func BenchmarkObservabilityMetadataTypedDecode(b *testing.B) {
	batch := benchObservabilityMetadataBatch(64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := classifyObservabilityMetadataEvidence(batch)
		if err != nil {
			b.Fatalf("classifyObservabilityMetadataEvidence: %v", err)
		}
	}
}

// BenchmarkObservabilityMetadataRawMap measures a faithful replica of the
// pre-migration raw payloadString read path over the same batch, so the
// benchmark pair bounds the typed-decode overhead (the migration's only added
// cost). It is a benchmark-only baseline, not production code.
func BenchmarkObservabilityMetadataRawMap(b *testing.B) {
	batch := benchObservabilityMetadataBatch(64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		groups := make(map[string][]observabilityMetadataEvidence)
		for _, env := range batch {
			if _, ok := facts.ObservabilitySchemaVersion(env.FactKind); !ok ||
				env.FactKind == facts.ObservabilitySourceInstanceFactKind {
				continue
			}
			ev, ok := rawMetadataEvidenceForBench(env)
			if !ok {
				continue
			}
			key := strings.Join([]string{ev.provider, ev.coverageSignal, ev.objectRef}, "\x00")
			groups[key] = append(groups[key], ev)
		}
		decisions := make([]ObservabilityCoverageCorrelationDecision, 0, len(groups))
		for _, ev := range groups {
			decisions = append(decisions, classifyObservabilityMetadataGroup(ev))
		}
		sortObservabilityCoverageDecisions(decisions)
	}
}

// rawMetadataEvidenceForBench replicates the pre-migration raw payloadString read
// for the benchmark baseline only. It mirrors the old
// observabilityMetadataEvidenceFromEnvelope shape closely enough to bound the
// decode overhead; it is never used by production code.
func rawMetadataEvidenceForBench(env facts.Envelope) (observabilityMetadataEvidence, bool) {
	p := env.Payload
	view := observabilityMetadataView{
		providerObjectUID: payloadcore.PayloadString(p, "provider_object_uid"),
		dashboardUID:      payloadcore.PayloadString(p, "dashboard_uid"),
		datasourceUID:     payloadcore.PayloadString(p, "datasource_uid"),
		alertRuleUID:      payloadcore.PayloadString(p, "alert_rule_uid"),
		folderUID:         payloadcore.PayloadString(p, "folder_uid"),
		resourceIdentity:  payloadcore.PayloadString(p, "resource_identity"),
		resourceName:      payloadcore.PayloadString(p, "resource_name"),
		pipelineName:      payloadcore.PayloadString(p, "pipeline_name"),
		ruleGroup:         payloadcore.PayloadString(p, "rule_group"),
		ruleName:          payloadcore.PayloadString(p, "rule_name"),
		tagName:           payloadcore.PayloadString(p, "tag_name"),
		seriesFingerprint: payloadcore.PayloadString(p, "series_fingerprint"),
		appName:           payloadcore.PayloadString(p, "app_name"),
		provider:          payloadcore.PayloadString(p, "provider"),
		backendKind:       payloadcore.PayloadString(p, "backend_kind"),
		sourceKind:        payloadcore.PayloadString(p, "source_kind"),
		sourceClass:       payloadcore.PayloadString(p, "source_class"),
		resourceClass:     payloadcore.PayloadString(p, "resource_class"),
		outcome:           payloadcore.PayloadString(p, "outcome"),
		freshnessState:    payloadcore.PayloadString(p, "freshness_state"),
		warningKind:       payloadcore.PayloadString(p, "warning_kind"),
		serviceHints:      payloadcore.PayloadString(p, "service_hints"),
		serviceRef:        payloadcore.PayloadString(p, "service_ref"),
	}
	provider := observabilityMetadataProvider(env.FactKind, view)
	signal := observabilityMetadataCoverageSignal(env.FactKind, view)
	objectRef := observabilityMetadataObjectRef(env, view)
	if provider == "" || signal == "" || objectRef == "" {
		return observabilityMetadataEvidence{}, false
	}
	return observabilityMetadataEvidence{
		provider:       provider,
		coverageSignal: signal,
		objectRef:      objectRef,
		sourceClass:    normalizedObservabilitySourceClass(env.FactKind, view),
		sourceKind:     payloadcore.FirstNonBlank(view.sourceKind, provider),
		sourceOutcome:  normalizedObservabilitySourceOutcome(view),
		resourceClass:  payloadcore.FirstNonBlank(view.resourceClass, view.observabilityResourceClass, signal),
		freshnessState: normalizedObservabilityFreshness(view),
		factID:         env.FactID,
	}, true
}
