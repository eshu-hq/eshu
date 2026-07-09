// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestStreamFactsEmitsDeclaredTempoTraceRouteObservabilityFacts(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "observability", "traces.yaml"), "apiVersion: v1\n")
	observedAt := time.Date(2026, time.June, 1, 15, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		FileData: []map[string]any{{
			"path": repoPath + "/observability/traces.yaml",
			"lang": "yaml",
			"observability_declared_trace_routes": []map[string]any{{
				"name":                             "trace_route.otel.traces",
				"line_number":                      25,
				"source_class":                     "declared",
				"source_kind":                      "helm",
				"declaration_kind":                 "otel_trace_pipeline",
				"pipeline_name":                    "traces",
				"receiver_refs":                    "otlp",
				"processor_refs":                   "batch",
				"exporter_refs":                    "otlp/tempo",
				"backend_kind":                     "tempo",
				"route_destination_fingerprint":    "dest:abc123",
				"tenant_scope_state":               "configured",
				"tenant_id_fingerprint":            "tenant:def456",
				"trace_tag_keys":                   "service.name,pod.uid",
				"trace_tag_identity_fingerprint":   "tags:ghi789",
				"traces_to_logs_datasource_uid":    "loki-prod",
				"traces_to_metrics_datasource_uid": "mimir-prod",
				"redaction_state":                  "redacted",
				"redacted_fields":                  "endpoint,headers,url,query,actions",
				"outcome":                          "exact",
				"endpoint":                         "https://tempo.internal.example:4317",
				"headers": map[string]any{
					"X-Scope-OrgID": "prod-tenant",
				},
				"query":    `{trace_id="$${__trace.traceId}"}`,
				"trace_id": "abcd1234",
			}},
		}},
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: "observability/traces.yaml",
			Digest:       "digest-traces",
			Language:     "yaml",
			CommitSHA:    "1234567890abcdef1234567890abcdef12345678",
		}},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false)
	allFacts := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount(), len(allFacts); got != want {
		t.Fatalf("FactCount = %d, want len(allFacts) %d", got, want)
	}

	route := factByKindForTest(t, allFacts, facts.ObservabilityDeclaredTraceRouteFactKind)
	assertObservabilityFactEnvelope(t, route)
	assertFactPayload(t, route, "source_class", "declared")
	assertFactPayload(t, route, "source_kind", "helm")
	assertFactPayload(t, route, "pipeline_name", "traces")
	assertFactPayload(t, route, "backend_kind", "tempo")
	assertFactPayload(t, route, "tenant_scope_state", "configured")
	assertFactPayload(t, route, "source_revision", "1234567890abcdef1234567890abcdef12345678")
	assertFactForbiddenKeysAbsent(t, route, "endpoint", "headers", "url", "query", "trace_id", "actions")
	assertFactForbiddenValuesAbsent(t, route, "tempo.internal.example", "prod-tenant", "$${__trace.traceId}", "abcd1234")
}
