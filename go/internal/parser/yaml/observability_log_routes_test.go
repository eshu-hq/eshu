package yaml

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePromtailHelmValuesEmitDeclaredLokiLogRoutes(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, filepath.Join("environments", "prod", "values-promtail.yaml"), `
config:
  clients:
    - url: https://logs-prod.internal.example/loki/api/v1/push
      tenant_id: checkout-prod
      external_labels:
        cluster: prod-a
        service: checkout-api
  scrape_configs:
    - job_name: kubernetes-pods
      static_configs:
        - targets: [localhost]
          labels:
            app: checkout-api
            namespace: checkout
            pod_uid: high-cardinality-value
            __path__: /var/log/pods/checkout/*.log
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	routes := yamlBucketForTest(t, got, "observability_declared_log_routes")
	if len(routes) != 1 {
		t.Fatalf("len(observability_declared_log_routes) = %d, want 1: %#v", len(routes), routes)
	}
	route := yamlNamedRowForTest(t, routes, "log_route.promtail.client.0")
	assertYAMLField(t, route, "source_class", "declared")
	assertYAMLField(t, route, "source_kind", "helm")
	assertYAMLField(t, route, "declaration_kind", "promtail_client_route")
	assertYAMLField(t, route, "backend_kind", "loki")
	assertYAMLField(t, route, "environment", "prod")
	assertYAMLField(t, route, "tenant_scope_state", "configured")
	assertYAMLField(t, route, "scrape_config_count", 1)
	assertYAMLField(t, route, "target_count", 1)
	assertYAMLField(t, route, "discovery_modes", "static")
	assertYAMLField(t, route, "label_keys", "__path__,app,cluster,namespace,pod_uid,service")
	assertYAMLField(t, route, "redaction_state", "redacted")
	assertYAMLFieldContains(t, route, "redacted_fields", "clients.url")
	assertYAMLFieldContains(t, route, "redacted_fields", "clients.tenant_id")
	assertYAMLFieldContains(t, route, "redacted_fields", "scrape_configs.static_configs.labels")
	assertYAMLField(t, route, "outcome", "exact")
	for _, key := range []string{"route_destination_fingerprint", "tenant_id_fingerprint", "label_identity_fingerprint"} {
		if fingerprint, _ := route[key].(string); strings.TrimSpace(fingerprint) == "" {
			t.Fatalf("%s = %#v, want non-empty in %#v", key, route[key], route)
		}
	}
	assertYAMLForbiddenKeysAbsent(t, route, "clients", "url", "tenant_id", "external_labels", "scrape_configs", "labels", "__path__")
	assertYAMLForbiddenValuesAbsent(t, route, "logs-prod.internal.example", "checkout-prod", "checkout-api", "high-cardinality-value", "/var/log/pods")

	warnings := yamlBucketForTest(t, got, "observability_coverage_warnings")
	warning := yamlWarningRowForTest(t, warnings, "high_cardinality_log_label_values_redacted")
	assertYAMLField(t, warning, "outcome", "rejected")
	assertYAMLForbiddenValuesAbsent(t, warning, "checkout-api", "high-cardinality-value")
}

func TestParseOtelLogsLokiGatewayAndDatasourceLogRoutes(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, filepath.Join("environments", "platform-prod", "values.yaml"), `
grafana:
  datasources:
    datasources.yaml:
      apiVersion: 1
      datasources:
        - name: Loki Prod
          uid: loki-prod
          type: loki
          url: https://loki-gateway.internal.example
loki:
  auth_enabled: true
  limits_config:
    max_streams_per_user: 100000
gateway:
  enabled: true
  nginx:
    config:
      serverSnippet: |
        location /loki/api/v1/push {
          proxy_pass http://loki-distributor:3100;
        }
config:
  receivers:
    filelog:
      include:
        - /var/log/pods/*/*/*.log
    otlp:
      protocols:
        grpc: {}
  exporters:
    otlphttp/loki:
      endpoint: https://loki-gateway.internal.example/otlp
      headers:
        X-Scope-OrgID: prod-tenant
  service:
    pipelines:
      logs:
        receivers: [filelog, otlp]
        processors: [batch]
        exporters: [otlphttp/loki]
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	routes := yamlBucketForTest(t, got, "observability_declared_log_routes")
	if len(routes) != 3 {
		t.Fatalf("len(observability_declared_log_routes) = %d, want 3: %#v", len(routes), routes)
	}

	datasource := yamlNamedRowForTest(t, routes, "log_route.grafana_datasource.loki-prod")
	assertYAMLField(t, datasource, "declaration_kind", "grafana_loki_datasource")
	assertYAMLField(t, datasource, "datasource_uid", "loki-prod")
	assertYAMLField(t, datasource, "backend_kind", "loki")
	assertYAMLField(t, datasource, "redaction_state", "redacted")
	assertYAMLFieldContains(t, datasource, "redacted_fields", "url")

	otel := yamlNamedRowForTest(t, routes, "log_route.otel.logs")
	assertYAMLField(t, otel, "declaration_kind", "otel_log_pipeline")
	assertYAMLField(t, otel, "backend_kind", "loki")
	assertYAMLField(t, otel, "receiver_refs", "filelog,otlp")
	assertYAMLField(t, otel, "processor_refs", "batch")
	assertYAMLField(t, otel, "exporter_refs", "otlphttp/loki")
	assertYAMLField(t, otel, "tenant_scope_state", "configured")
	assertYAMLFieldContains(t, otel, "receiver_kinds", "filelog")
	assertYAMLFieldContains(t, otel, "redacted_fields", "exporters.endpoint")
	assertYAMLFieldContains(t, otel, "redacted_fields", "receivers.filelog.include")

	gateway := yamlNamedRowForTest(t, routes, "log_route.loki.gateway")
	assertYAMLField(t, gateway, "declaration_kind", "loki_gateway_route")
	assertYAMLField(t, gateway, "backend_kind", "loki")
	assertYAMLField(t, gateway, "tenant_scope_state", "configured")
	assertYAMLField(t, gateway, "redaction_state", "redacted")
	assertYAMLFieldContains(t, gateway, "redacted_fields", "serverSnippet")

	for _, route := range routes {
		assertYAMLForbiddenKeysAbsent(t, route, "endpoint", "headers", "url", "serverSnippet", "include")
		assertYAMLForbiddenValuesAbsent(t, route, "loki-gateway.internal.example", "prod-tenant", "loki-distributor", "/var/log/pods")
	}
}

func TestParseLokiLogRouteEdgeOutcomes(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "values-promtail.yaml", `
config:
  clients:
    - tenant_id: missing-endpoint-tenant
    - url: https://logs-prod.internal.example/loki/api/v1/push
      tenant_id: duplicate-tenant
    - url: https://logs-prod.internal.example/loki/api/v1/push
      tenant_id: duplicate-tenant
  scrape_configs:
    - static_configs:
        - targets: [localhost]
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	routes := yamlBucketForTest(t, got, "observability_declared_log_routes")
	if len(routes) != 3 {
		t.Fatalf("len(observability_declared_log_routes) = %d, want 3: %#v", len(routes), routes)
	}
	missing := yamlNamedRowForTest(t, routes, "log_route.promtail.client.0")
	assertYAMLField(t, missing, "outcome", "unresolved")

	duplicateOne := yamlNamedRowForTest(t, routes, "log_route.promtail.client.1")
	duplicateTwo := yamlNamedRowForTest(t, routes, "log_route.promtail.client.2")
	for _, row := range []map[string]any{duplicateOne, duplicateTwo} {
		assertYAMLField(t, row, "outcome", "ambiguous")
		assertYAMLField(t, row, "duplicate_log_route_identity", true)
		assertYAMLForbiddenValuesAbsent(t, row, "logs-prod.internal.example", "duplicate-tenant")
	}

	warnings := yamlBucketForTest(t, got, "observability_coverage_warnings")
	missingEndpoint := yamlWarningRowForTest(t, warnings, "missing_log_route_endpoint")
	assertYAMLField(t, missingEndpoint, "outcome", "unresolved")
}

func TestParseMalformedLogRouteConfigEmitsWarning(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "values-logs.yaml", `
config:
  clients: not-a-list
  scrape_configs:
    - static_configs:
        - targets: [localhost]
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	if routes := yamlBucketForTest(t, got, "observability_declared_log_routes"); len(routes) != 0 {
		t.Fatalf("observability_declared_log_routes = %#v, want empty", routes)
	}
	warnings := yamlBucketForTest(t, got, "observability_coverage_warnings")
	warning := yamlWarningRowForTest(t, warnings, "malformed_log_route")
	assertYAMLField(t, warning, "outcome", "rejected")
	assertYAMLForbiddenValuesAbsent(t, warning, "not-a-list")
}
