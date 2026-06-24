// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOtelTempoAndDatasourceTraceRoutes(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, filepath.Join("environments", "prod", "values.yaml"), `
grafana:
  datasources:
    datasources.yaml:
      apiVersion: 1
      datasources:
        - name: Tempo Prod
          uid: tempo-prod
          type: tempo
          url: https://tempo-query.internal.example
          jsonData:
            tracesToLogsV2:
              datasourceUid: loki-prod
              tags:
                - key: service.name
                  value: service
                - key: pod.uid
                  value: high-cardinality-pod
              customQuery: true
              query: '{trace_id="$${__trace.traceId}"}'
            tracesToMetrics:
              datasourceUid: mimir-prod
              tags:
                - key: service.name
                  value: service
                - key: span.kind
              queries:
                - name: latency
                  query: 'sum(rate(traces_spanmetrics_latency_bucket{$$__tags}[5m]))'
            serviceMap:
              datasourceUid: mimir-prod
tempo:
  structuredConfig:
    multitenancy_enabled: true
    distributor:
      receivers:
        otlp:
          protocols:
            grpc:
              endpoint: 0.0.0.0:4317
gateway:
  enabled: true
  ingress:
    hosts:
      - tempo.internal.example
config:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: 0.0.0.0:4317
  processors:
    attributes:
      actions:
        - key: http.route
          value: /checkout/{id}
          action: insert
  exporters:
    otlp/tempo:
      endpoint: https://tempo-distributor.internal.example:4317
      headers:
        X-Scope-OrgID: prod-tenant
  connectors:
    spanmetrics:
      histogram: {}
  service:
    pipelines:
      traces:
        receivers: [otlp]
        processors: [batch, attributes]
        exporters: [otlp/tempo, spanmetrics]
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	routes := yamlBucketForTest(t, got, "observability_declared_trace_routes")
	if len(routes) != 3 {
		t.Fatalf("len(observability_declared_trace_routes) = %d, want 3: %#v", len(routes), routes)
	}

	datasource := yamlNamedRowForTest(t, routes, "trace_route.grafana_datasource.tempo-prod")
	assertYAMLField(t, datasource, "declaration_kind", "grafana_tempo_datasource")
	assertYAMLField(t, datasource, "datasource_uid", "tempo-prod")
	assertYAMLField(t, datasource, "backend_kind", "tempo")
	assertYAMLField(t, datasource, "traces_to_logs_datasource_uid", "loki-prod")
	assertYAMLField(t, datasource, "traces_to_metrics_datasource_uid", "mimir-prod")
	assertYAMLField(t, datasource, "service_map_datasource_uid", "mimir-prod")
	assertYAMLField(t, datasource, "trace_tag_keys", "pod.uid,service.name,span.kind")
	assertYAMLFieldContains(t, datasource, "redacted_fields", "jsonData.tracesToLogsV2.query")
	assertYAMLFieldContains(t, datasource, "redacted_fields", "jsonData.tracesToMetrics.queries")
	wantTagFingerprint := fingerprintObject(map[string]any{
		"pod.uid":      "present",
		"service.name": "present",
		"span.kind":    "present",
	})
	if got := datasource["trace_tag_identity_fingerprint"]; got != wantTagFingerprint {
		t.Fatalf("trace_tag_identity_fingerprint = %#v, want %q", got, wantTagFingerprint)
	}

	otel := yamlNamedRowForTest(t, routes, "trace_route.otel.traces")
	assertYAMLField(t, otel, "declaration_kind", "otel_trace_pipeline")
	assertYAMLField(t, otel, "backend_kind", "tempo")
	assertYAMLField(t, otel, "receiver_refs", "otlp")
	assertYAMLField(t, otel, "processor_refs", "attributes,batch")
	assertYAMLField(t, otel, "exporter_refs", "otlp/tempo,spanmetrics")
	assertYAMLField(t, otel, "connector_refs", "spanmetrics")
	assertYAMLField(t, otel, "tenant_scope_state", "configured")
	assertYAMLFieldContains(t, otel, "redacted_fields", "exporters.endpoint")
	assertYAMLFieldContains(t, otel, "redacted_fields", "exporters.headers")
	assertYAMLFieldContains(t, otel, "redacted_fields", "processors.attributes.actions")

	tempo := yamlNamedRowForTest(t, routes, "trace_route.tempo.gateway")
	assertYAMLField(t, tempo, "declaration_kind", "tempo_gateway_route")
	assertYAMLField(t, tempo, "backend_kind", "tempo")
	assertYAMLField(t, tempo, "tenant_scope_state", "configured")
	assertYAMLFieldContains(t, tempo, "redacted_fields", "ingress.hosts")

	for _, route := range routes {
		assertYAMLForbiddenKeysAbsent(t, route, "endpoint", "headers", "url", "query", "queries", "actions", "hosts")
		assertYAMLForbiddenValuesAbsent(t, route, "tempo-query.internal.example", "tempo-distributor.internal.example", "prod-tenant", "high-cardinality-pod", "$${__trace.traceId}", "/checkout")
	}

	warnings := yamlBucketForTest(t, got, "observability_coverage_warnings")
	warning := yamlWarningRowForTest(t, warnings, "high_cardinality_trace_tag_values_redacted")
	assertYAMLField(t, warning, "outcome", "rejected")
	assertYAMLForbiddenValuesAbsent(t, warning, "high-cardinality-pod")
}

func TestParseTempoTraceRouteEdgeOutcomes(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "values-tempo.yaml", `
grafana:
  datasources:
    datasources.yaml:
      apiVersion: 1
      datasources:
        - name: Tempo Missing
          uid: tempo-missing
          type: tempo
config:
  exporters:
    otlp/tempo:
      endpoint: https://tempo-distributor.internal.example:4317
    otlp/tempo-missing: {}
  service:
    pipelines:
      traces: not-a-map
      traces/dup-a:
        receivers: [otlp]
        exporters: [otlp/tempo]
      traces/dup-b:
        receivers: [otlp]
        exporters: [otlp/tempo]
      traces/missing:
        receivers: [otlp]
        exporters: [otlp/tempo-missing]
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	routes := yamlBucketForTest(t, got, "observability_declared_trace_routes")
	if len(routes) != 4 {
		t.Fatalf("len(observability_declared_trace_routes) = %d, want 4: %#v", len(routes), routes)
	}
	missing := yamlNamedRowForTest(t, routes, "trace_route.grafana_datasource.tempo-missing")
	assertYAMLField(t, missing, "outcome", "unresolved")
	missingExporter := yamlNamedRowForTest(t, routes, "trace_route.otel.traces_missing")
	assertYAMLField(t, missingExporter, "outcome", "unresolved")

	duplicateA := yamlNamedRowForTest(t, routes, "trace_route.otel.traces_dup-a")
	duplicateB := yamlNamedRowForTest(t, routes, "trace_route.otel.traces_dup-b")
	for _, row := range []map[string]any{duplicateA, duplicateB} {
		assertYAMLField(t, row, "outcome", "ambiguous")
		assertYAMLField(t, row, "duplicate_trace_route_identity", true)
		assertYAMLForbiddenValuesAbsent(t, row, "tempo-distributor.internal.example")
	}

	warnings := yamlBucketForTest(t, got, "observability_coverage_warnings")
	malformed := yamlWarningRowForTest(t, warnings, "malformed_trace_route")
	assertYAMLField(t, malformed, "outcome", "rejected")
	missingEndpoint := yamlWarningRowForTest(t, warnings, "missing_trace_route_endpoint")
	assertYAMLField(t, missingEndpoint, "outcome", "unresolved")
}

func TestParseTempoDatasourceSkipsMissingTraceTagRedaction(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "values-tempo-tags.yaml", `
grafana:
  datasources:
    datasources.yaml:
      apiVersion: 1
      datasources:
        - name: Tempo Prod
          uid: tempo-prod
          type: tempo
          url: https://tempo-query.internal.example
          jsonData:
            tracesToLogsV2:
              datasourceUid: loki-prod
              tags:
                - key: service.name
            tracesToMetrics:
              datasourceUid: mimir-prod
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	routes := yamlBucketForTest(t, got, "observability_declared_trace_routes")
	datasource := yamlNamedRowForTest(t, routes, "trace_route.grafana_datasource.tempo-prod")
	assertYAMLField(t, datasource, "trace_tag_keys", "service.name")
	redacted := cleanString(datasource["redacted_fields"])
	if strings.Contains(redacted, "jsonData.tracesToMetrics.tags") {
		t.Fatalf("redacted_fields = %q, want no missing tracesToMetrics tags redaction", redacted)
	}
	assertYAMLForbiddenValuesAbsent(t, datasource, "tempo-query.internal.example")
}
