// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestStreamFactsEmitsDeclaredPrometheusMimirObservabilityFacts(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "observability", "metrics.yaml"), "apiVersion: v1\n")
	observedAt := time.Date(2026, time.June, 1, 13, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		FileData: []map[string]any{{
			"path": repoPath + "/observability/metrics.yaml",
			"lang": "yaml",
			"observability_declared_scrape_configs": []map[string]any{{
				"name":                          "scrape_config.ServiceMonitor.checkout-service",
				"line_number":                   5,
				"source_class":                  "declared",
				"source_kind":                   "kubernetes",
				"declaration_kind":              "prometheus_service_monitor",
				"resource_kind":                 "ServiceMonitor",
				"resource_name":                 "checkout-service",
				"selector_identity_fingerprint": "selector:abc123",
				"selector_label_keys":           "app.kubernetes.io/name",
				"release_label_present":         true,
				"endpoint_count":                1,
				"outcome":                       "exact",
			}},
			"observability_declared_metric_rules": []map[string]any{{
				"name":                        "metric_rule.checkout.rules.abc123",
				"line_number":                 15,
				"source_class":                "declared",
				"source_kind":                 "kubernetes",
				"declaration_kind":            "prometheus_rule",
				"resource_kind":               "PrometheusRule",
				"resource_name":               "checkout-rules",
				"rule_group":                  "checkout.rules",
				"rule_kind":                   "alert",
				"alert_rule_name_fingerprint": "alert:def456",
				"outcome":                     "exact",
			}},
			"observability_declared_metric_routes": []map[string]any{{
				"name":             "metric_route.otel.metrics",
				"line_number":      25,
				"source_class":     "declared",
				"source_kind":      "helm",
				"declaration_kind": "otel_metric_pipeline",
				"pipeline_name":    "metrics",
				"receiver_refs":    "prometheus",
				"exporter_refs":    "otlphttp/mimir",
				"backend_kind":     "mimir",
				"redaction_state":  "redacted",
				"redacted_fields":  "endpoint,headers",
				"outcome":          "exact",
			}},
		}},
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: "observability/metrics.yaml",
			Digest:       "digest-metrics",
			Language:     "yaml",
			CommitSHA:    "fedcba9876543210fedcba9876543210fedcba98",
		}},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false)
	allFacts := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount, len(allFacts); got != want {
		t.Fatalf("FactCount = %d, want len(allFacts) %d", got, want)
	}

	scrape := factByKindForTest(t, allFacts, facts.ObservabilityDeclaredScrapeConfigFactKind)
	assertObservabilityFactEnvelope(t, scrape)
	assertFactPayload(t, scrape, "selector_identity_fingerprint", "selector:abc123")
	assertFactPayload(t, scrape, "selector_label_keys", "app.kubernetes.io/name")
	assertFactPayload(t, scrape, "release_label_present", true)
	assertFactForbiddenKeysAbsent(t, scrape, "matchLabels", "targets", "url", "headers")
	assertFactForbiddenValuesAbsent(t, scrape, "checkout-api", "private-api.internal.example")

	rule := factByKindForTest(t, allFacts, facts.ObservabilityDeclaredMetricRuleFactKind)
	assertObservabilityFactEnvelope(t, rule)
	assertFactPayload(t, rule, "rule_group", "checkout.rules")
	assertFactPayload(t, rule, "rule_kind", "alert")
	assertFactForbiddenKeysAbsent(t, rule, "expr", "query")
	assertFactForbiddenValuesAbsent(t, rule, "CheckoutHighLatency", "histogram_quantile")

	route := factByKindForTest(t, allFacts, facts.ObservabilityDeclaredMetricRouteFactKind)
	assertObservabilityFactEnvelope(t, route)
	assertFactPayload(t, route, "pipeline_name", "metrics")
	assertFactPayload(t, route, "backend_kind", "mimir")
	assertFactPayload(t, route, "source_revision", "fedcba9876543210fedcba9876543210fedcba98")
	assertFactForbiddenKeysAbsent(t, route, "endpoint", "headers", "url")
	assertFactForbiddenValuesAbsent(t, route, "mimir.prod.example", "prod-tenant")
}
