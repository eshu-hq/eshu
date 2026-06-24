// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestStreamFactsEmitsDeclaredGrafanaObservabilityFacts(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "observability", "grafana.yaml"), "apiVersion: v1\n")
	observedAt := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		FileData: []map[string]any{{
			"path": repoPath + "/observability/grafana.yaml",
			"lang": "yaml",
			"observability_declared_folders": []map[string]any{{
				"name":                     "folder.checkout",
				"line_number":              5,
				"source_class":             "declared",
				"source_kind":              "kubernetes",
				"declaration_kind":         "grafana_folder_resource",
				"folder_uid":               "checkout",
				"folder_title_fingerprint": "folder:abc123",
				"outcome":                  "exact",
			}},
			"observability_declared_dashboards": []map[string]any{{
				"name":                        "dashboard.checkout",
				"line_number":                 7,
				"source_class":                "declared",
				"source_kind":                 "kubernetes",
				"declaration_kind":            "grafana_dashboard_resource",
				"dashboard_uid":               "checkout-latency",
				"dashboard_title_fingerprint": "title:abc123",
				"datasource_refs":             "uid:prom-prod,type:prometheus",
				"service_hints":               "checkout",
				"outcome":                     "exact",
			}},
			"observability_declared_datasources": []map[string]any{{
				"name":                        "datasource.prom-prod",
				"line_number":                 15,
				"source_class":                "declared",
				"source_kind":                 "helm",
				"declaration_kind":            "grafana_datasource_provisioning",
				"datasource_uid":              "prom-prod",
				"datasource_type":             "prometheus",
				"datasource_name_fingerprint": "name:def456",
				"redaction_state":             "redacted",
				"redacted_fields":             "url,secureJsonData",
				"outcome":                     "exact",
			}},
			"observability_declared_alert_rules": []map[string]any{{
				"name":                         "alert.checkout-high-latency",
				"line_number":                  25,
				"source_class":                 "declared",
				"source_kind":                  "kubernetes",
				"declaration_kind":             "grafana_alert_provisioning",
				"alert_rule_uid":               "checkout-high-latency",
				"alert_rule_title_fingerprint": "alert:ghi789",
				"rule_group":                   "checkout.rules",
				"datasource_refs":              "uid:prom-prod",
				"outcome":                      "exact",
			}},
			"observability_coverage_warnings": []map[string]any{{
				"name":         "warning.unsupported.private-plugin",
				"line_number":  19,
				"source_class": "declared",
				"source_kind":  "kubernetes",
				"warning_kind": "unsupported_datasource_type",
				"outcome":      "unsupported",
			}},
		}},
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: "observability/grafana.yaml",
			Digest:       "digest-grafana",
			Language:     "yaml",
			CommitSHA:    "0123456789abcdef0123456789abcdef01234567",
		}},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false)
	allFacts := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount, len(allFacts); got != want {
		t.Fatalf("FactCount = %d, want len(allFacts) %d", got, want)
	}

	source := factByKindForTest(t, allFacts, facts.ObservabilitySourceInstanceFactKind)
	assertObservabilityFactEnvelope(t, source)
	assertFactPayload(t, source, "source_class", "declared")
	assertFactPayload(t, source, "source_kind", "git")
	assertFactPayload(t, source, "relative_path", "observability/grafana.yaml")
	assertFactPayload(t, source, "source_revision", "0123456789abcdef0123456789abcdef01234567")

	folder := factByKindForTest(t, allFacts, facts.ObservabilityDeclaredFolderFactKind)
	assertObservabilityFactEnvelope(t, folder)
	assertFactPayload(t, folder, "folder_uid", "checkout")
	assertFactForbiddenValuesAbsent(t, folder, "Checkout Private Folder")

	dashboard := factByKindForTest(t, allFacts, facts.ObservabilityDeclaredDashboardFactKind)
	assertObservabilityFactEnvelope(t, dashboard)
	assertFactPayload(t, dashboard, "dashboard_uid", "checkout-latency")
	assertFactPayload(t, dashboard, "scope_id", collected.Scope.ScopeID)
	assertFactPayload(t, dashboard, "generation_id", collected.Generation.GenerationID)
	assertFactPayload(t, dashboard, "freshness_state", "current")
	assertFactPayload(t, dashboard, "redaction_version", "observability-redaction-v1")
	assertFactPayload(t, dashboard, "source_revision", "0123456789abcdef0123456789abcdef01234567")
	assertFactForbiddenKeysAbsent(t, dashboard, "title", "query", "expr", "url", "secret")
	assertFactForbiddenValuesAbsent(t, dashboard, "Checkout Latency", "histogram_quantile")

	datasource := factByKindForTest(t, allFacts, facts.ObservabilityDeclaredDatasourceFactKind)
	assertFactPayload(t, datasource, "datasource_uid", "prom-prod")
	assertFactPayload(t, datasource, "datasource_type", "prometheus")
	assertFactForbiddenKeysAbsent(t, datasource, "url", "secureJsonData", "super-secret")
	assertFactForbiddenValuesAbsent(t, datasource, "prometheus.internal.example", "super-secret")

	alert := factByKindForTest(t, allFacts, facts.ObservabilityDeclaredAlertRuleFactKind)
	assertFactPayload(t, alert, "alert_rule_uid", "checkout-high-latency")
	assertFactForbiddenKeysAbsent(t, alert, "query", "expr", "model")
	assertFactForbiddenValuesAbsent(t, alert, "Checkout High Latency", "errors_total")

	warning := factByKindForTest(t, allFacts, facts.ObservabilityCoverageWarningFactKind)
	assertFactPayload(t, warning, "warning_kind", "unsupported_datasource_type")
	assertFactPayload(t, warning, "outcome", "unsupported")
}

func assertObservabilityFactEnvelope(t *testing.T, envelope facts.Envelope) {
	t.Helper()
	if got, want := envelope.SchemaVersion, facts.ObservabilitySchemaVersionV1; got != want {
		t.Fatalf("%s SchemaVersion = %q, want %q", envelope.FactKind, got, want)
	}
	if got, want := envelope.CollectorKind, "git"; got != want {
		t.Fatalf("%s CollectorKind = %q, want %q", envelope.FactKind, got, want)
	}
	if got, want := envelope.SourceConfidence, facts.SourceConfidenceObserved; got != want {
		t.Fatalf("%s SourceConfidence = %q, want %q", envelope.FactKind, got, want)
	}
	if !strings.Contains(envelope.StableFactKey, envelope.GenerationID) {
		t.Fatalf("%s StableFactKey = %q, want generation-scoped key", envelope.FactKind, envelope.StableFactKey)
	}
}

func factByKindForTest(t *testing.T, envelopes []facts.Envelope, kind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			return envelope
		}
	}
	t.Fatalf("missing fact kind %q in %#v", kind, envelopes)
	return facts.Envelope{}
}

func assertFactPayload(t *testing.T, envelope facts.Envelope, key string, want any) {
	t.Helper()
	if got := envelope.Payload[key]; got != want {
		t.Fatalf("%s payload[%s] = %#v, want %#v in %#v", envelope.FactKind, key, got, want, envelope.Payload)
	}
}

func assertFactForbiddenKeysAbsent(t *testing.T, envelope facts.Envelope, forbidden ...string) {
	t.Helper()
	for _, value := range forbidden {
		if _, exists := envelope.Payload[value]; exists {
			t.Fatalf("%s payload has forbidden key %q: %#v", envelope.FactKind, value, envelope.Payload)
		}
	}
}

func assertFactForbiddenValuesAbsent(t *testing.T, envelope facts.Envelope, forbidden ...string) {
	t.Helper()
	rendered := strings.ToLower(strings.Join(payloadValuesForTest(envelope.Payload), " "))
	for _, value := range forbidden {
		if strings.Contains(rendered, strings.ToLower(value)) {
			t.Fatalf("%s payload has forbidden value %q: %#v", envelope.FactKind, value, envelope.Payload)
		}
	}
}

func payloadValuesForTest(payload map[string]any) []string {
	values := make([]string, 0, len(payload))
	for _, value := range payload {
		values = append(values, fmt.Sprint(value))
	}
	return values
}
