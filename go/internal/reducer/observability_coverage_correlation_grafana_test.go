// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildObservabilityCoverageDecisionsClassifiesGrafanaStackEvidence(t *testing.T) {
	t.Parallel()

	decisions, _, err := BuildObservabilityCoverageDecisions([]facts.Envelope{
		observabilityFact("declared-dashboard", facts.ObservabilityDeclaredDashboardFactKind, map[string]any{
			"provider":        "grafana",
			"source_class":    "declared",
			"source_kind":     "kubernetes",
			"dashboard_uid":   "checkout-latency",
			"service_hints":   "checkout",
			"freshness_state": "current",
			"outcome":         "exact",
		}),
		observabilityFact("observed-dashboard", facts.ObservabilityObservedDashboardFactKind, map[string]any{
			"provider":             "grafana",
			"source_class":         "observed",
			"source_kind":          "grafana",
			"resource_class":       "dashboard",
			"provider_object_uid":  "checkout-latency",
			"declared_match_state": "matched_declared",
			"freshness_state":      "current",
			"outcome":              "observed",
		}),
		observabilityFact("applied-scrape", facts.ObservabilityAppliedResourceFactKind, map[string]any{
			"provider":                     "prometheus",
			"source_class":                 "applied",
			"source_kind":                  "argocd",
			"observability_resource_class": "scrape_config",
			"resource_name":                "checkout-service",
			"sync_status":                  "Synced",
			"freshness_state":              "current",
			"outcome":                      "exact",
		}),
		observabilityFact("observed-target", facts.ObservabilityObservedTargetFactKind, map[string]any{
			"provider":            "prometheus",
			"source_class":        "observed",
			"source_kind":         "prometheus",
			"resource_class":      "target",
			"provider_object_uid": "checkout-service",
			"freshness_state":     "current",
			"outcome":             "observed",
		}),
		observabilityFact("mimir-stale-rule", facts.ObservabilityObservedRuleFactKind, map[string]any{
			"provider":            "mimir",
			"source_class":        "observed",
			"source_kind":         "mimir",
			"resource_class":      "rule",
			"provider_object_uid": "checkout.rules:HighLatency",
			"freshness_state":     "stale",
			"outcome":             "stale",
		}),
		observabilityFact("grafana-observed-alert", facts.ObservabilityObservedRuleFactKind, map[string]any{
			"provider":            "grafana",
			"source_class":        "observed",
			"source_kind":         "grafana",
			"resource_class":      "alert_rule",
			"provider_object_uid": "checkout-alerts:HighErrorRate",
			"freshness_state":     "current",
			"outcome":             "observed",
		}),
		observabilityFact("loki-manual-signal", facts.ObservabilityObservedLogSignalFactKind, map[string]any{
			"provider":               "loki",
			"source_class":           "observed",
			"source_kind":            "loki",
			"resource_class":         "log_signal",
			"provider_object_uid":    "series:checkout",
			"freshness_state":        "current",
			"outcome":                "observed",
			"drift_candidate_reason": "manual_provider_resource",
		}),
		observabilityFact("tempo-permission", facts.ObservabilityCoverageWarningFactKind, map[string]any{
			"provider":            "tempo",
			"source_class":        "observed",
			"source_kind":         "tempo",
			"resource_class":      "trace_signal",
			"provider_object_uid": "tag:resource.service.name",
			"freshness_state":     "permission_hidden",
			"warning_kind":        "permission_hidden",
			"outcome":             "permission_hidden",
		}),
		observabilityFact("tempo-rejected", facts.ObservabilityCoverageWarningFactKind, map[string]any{
			"provider":            "tempo",
			"source_class":        "observed",
			"source_kind":         "tempo",
			"resource_class":      "trace_signal",
			"provider_object_uid": "tag:high-cardinality",
			"freshness_state":     "unknown",
			"warning_kind":        "high_cardinality_rejected",
			"outcome":             "rejected",
		}),
		observabilityFact("unsupported-warning", facts.ObservabilityCoverageWarningFactKind, map[string]any{
			"provider":            "grafana",
			"source_class":        "declared",
			"source_kind":         "terraform",
			"resource_class":      "GrafanaMuteTiming",
			"provider_object_uid": "unsupported:mute-timing",
			"freshness_state":     "unknown",
			"warning_kind":        "unsupported_resource_kind",
			"outcome":             "unsupported",
		}),
	})
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}

	index := observabilityDecisionsByProviderAndRef(decisions)
	dashboard := index["grafana|dashboard|checkout-latency"]
	assertCoverageOutcome(t, dashboard, ObservabilityCoverageExact, "covered")
	if dashboard.SourceClass != "mixed" {
		t.Fatalf("dashboard SourceClass = %q, want mixed", dashboard.SourceClass)
	}
	if got, want := dashboard.SourceClasses, []string{"declared", "observed"}; !slices.Equal(got, want) {
		t.Fatalf("dashboard SourceClasses = %v, want %v", got, want)
	}
	if dashboard.TargetServiceRef != "checkout" {
		t.Fatalf("dashboard TargetServiceRef = %q, want checkout", dashboard.TargetServiceRef)
	}

	assertCoverageOutcome(t, index["prometheus|scrape_target|checkout-service"], ObservabilityCoverageExact, "covered")
	assertCoverageOutcome(t, index["mimir|rule|checkout.rules:HighLatency"], ObservabilityCoverageStale, "stale")
	assertCoverageOutcome(t, index["grafana|alert_rule|checkout-alerts:HighErrorRate"], ObservabilityCoverageExact, "covered")
	assertCoverageOutcome(t, index["loki|log_signal|series:checkout"], ObservabilityCoverageDrifted, "drifted")
	assertCoverageOutcome(t, index["tempo|trace_signal|tag:resource.service.name"], ObservabilityCoveragePermissionHidden, "permission_hidden")
	assertCoverageOutcome(t, index["tempo|trace_signal|tag:high-cardinality"], ObservabilityCoverageRejected, "rejected")
	assertCoverageOutcome(t, index["grafana|unsupported|unsupported:mute-timing"], ObservabilityCoverageRejected, "rejected")
}

func TestObservabilityCoverageCorrelationFactKindsIncludesGrafanaStackSources(t *testing.T) {
	t.Parallel()

	got := observabilityCoverageCorrelationFactKinds()
	for _, kind := range facts.ObservabilityFactKinds() {
		if !slices.Contains(got, kind) {
			t.Fatalf("observabilityCoverageCorrelationFactKinds() missing %q from %v", kind, got)
		}
	}
}

func TestExtractObservabilityCoverageEdgeRowsDoesNotPromoteGrafanaStackOutcomes(t *testing.T) {
	t.Parallel()

	rows, tally, _, err := ExtractObservabilityCoverageEdgeRows([]facts.Envelope{
		observabilityFact("observed-dashboard", facts.ObservabilityObservedDashboardFactKind, map[string]any{
			"provider":            "grafana",
			"source_class":        "observed",
			"source_kind":         "grafana",
			"resource_class":      "dashboard",
			"provider_object_uid": "manual-dashboard",
			"freshness_state":     "current",
			"outcome":             "observed",
		}),
		observabilityFact("loki-manual-signal", facts.ObservabilityObservedLogSignalFactKind, map[string]any{
			"provider":               "loki",
			"source_class":           "observed",
			"source_kind":            "loki",
			"resource_class":         "log_signal",
			"provider_object_uid":    "series:checkout",
			"freshness_state":        "current",
			"outcome":                "observed",
			"drift_candidate_reason": "manual_provider_resource",
		}),
		observabilityFact("tempo-permission", facts.ObservabilityCoverageWarningFactKind, map[string]any{
			"provider":            "tempo",
			"source_class":        "observed",
			"source_kind":         "tempo",
			"resource_class":      "trace_signal",
			"provider_object_uid": "tag:hidden",
			"freshness_state":     "permission_hidden",
			"outcome":             "permission_hidden",
		}),
	})
	if err != nil {
		t.Fatalf("ExtractObservabilityCoverageEdgeRows() error = %v, want nil", err)
	}

	if len(rows) != 0 {
		t.Fatalf("Grafana-stack provenance decisions produced %d COVERS edge row(s), want 0: %v", len(rows), rows)
	}
	if len(tally.materialized) != 0 {
		t.Fatalf("materialized tally = %v, want empty", tally.materialized)
	}
}

func observabilityFact(factID string, kind string, payload map[string]any) facts.Envelope {
	version, _ := facts.ObservabilitySchemaVersion(kind)
	if payload == nil {
		payload = map[string]any{}
	}
	if payload["scope_id"] == nil {
		payload["scope_id"] = "scope-observability"
	}
	if payload["generation_id"] == nil {
		payload["generation_id"] = "generation-observability"
	}
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       "scope-observability",
		GenerationID:  "generation-observability",
		FactKind:      kind,
		SchemaVersion: version,
		CollectorKind: "observability-test",
		ObservedAt:    time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC),
		Payload:       payload,
	}
}

func observabilityDecisionsByProviderAndRef(
	decisions []ObservabilityCoverageCorrelationDecision,
) map[string]ObservabilityCoverageCorrelationDecision {
	out := make(map[string]ObservabilityCoverageCorrelationDecision, len(decisions))
	for _, decision := range decisions {
		out[decision.Provider+"|"+decision.CoverageSignal+"|"+decision.ObservabilityObjectRef] = decision
	}
	return out
}
