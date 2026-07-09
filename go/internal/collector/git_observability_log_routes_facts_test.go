// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestStreamFactsEmitsDeclaredLokiLogRouteObservabilityFacts(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "observability", "logs.yaml"), "apiVersion: v1\n")
	observedAt := time.Date(2026, time.June, 1, 14, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		FileData: []map[string]any{{
			"path": repoPath + "/observability/logs.yaml",
			"lang": "yaml",
			"observability_declared_log_routes": []map[string]any{{
				"name":                          "log_route.otel.logs",
				"line_number":                   25,
				"source_class":                  "declared",
				"source_kind":                   "helm",
				"declaration_kind":              "otel_log_pipeline",
				"pipeline_name":                 "logs",
				"receiver_refs":                 "filelog,otlp",
				"processor_refs":                "batch",
				"exporter_refs":                 "otlphttp/loki",
				"backend_kind":                  "loki",
				"route_destination_fingerprint": "dest:abc123",
				"tenant_scope_state":            "configured",
				"tenant_id_fingerprint":         "tenant:def456",
				"label_keys":                    "cluster,service",
				"label_identity_fingerprint":    "labels:ghi789",
				"redaction_state":               "redacted",
				"redacted_fields":               "endpoint,headers,clients.url,tenant_id,scrape_configs.static_configs.labels",
				"outcome":                       "exact",
				"endpoint":                      "https://loki.internal.example/otlp",
				"headers": map[string]any{
					"X-Scope-OrgID": "prod-tenant",
				},
				"tenant_id": "prod-tenant",
				"labels": map[string]any{
					"service": "checkout-api",
				},
				"scrape_configs": []any{map[string]any{
					"static_configs": []any{map[string]any{
						"targets": []any{"localhost"},
					}},
				}},
			}},
		}},
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: "observability/logs.yaml",
			Digest:       "digest-logs",
			Language:     "yaml",
			CommitSHA:    "abcdef0123456789abcdef0123456789abcdef01",
		}},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false)
	allFacts := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount(), len(allFacts); got != want {
		t.Fatalf("FactCount = %d, want len(allFacts) %d", got, want)
	}

	route := factByKindForTest(t, allFacts, facts.ObservabilityDeclaredLogRouteFactKind)
	assertObservabilityFactEnvelope(t, route)
	assertFactPayload(t, route, "source_class", "declared")
	assertFactPayload(t, route, "source_kind", "helm")
	assertFactPayload(t, route, "pipeline_name", "logs")
	assertFactPayload(t, route, "backend_kind", "loki")
	assertFactPayload(t, route, "tenant_scope_state", "configured")
	assertFactPayload(t, route, "source_revision", "abcdef0123456789abcdef0123456789abcdef01")
	assertFactPayload(t, route, "redaction_version", "observability-redaction-v1")
	assertFactForbiddenKeysAbsent(t, route, "endpoint", "headers", "tenant_id", "labels", "scrape_configs", "static_configs", "targets", "url")
	assertFactForbiddenValuesAbsent(t, route, "loki.internal.example", "prod-tenant", "checkout-api", "localhost")
}
