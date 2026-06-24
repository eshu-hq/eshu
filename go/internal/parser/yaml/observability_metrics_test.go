// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePrometheusOperatorResourcesEmitsMetricEvidence(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "manifests/prometheus-resources.yaml", `
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: checkout-service
  namespace: monitoring
  labels:
    release: monitoring
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: checkout-api
  namespaceSelector:
    matchNames:
      - checkout
  endpoints:
    - port: http-metrics
      path: /metrics
---
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: checkout-pods
  namespace: monitoring
spec:
  selector:
    matchLabels:
      app: checkout-worker
  podMetricsEndpoints:
    - port: metrics
---
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: checkout-rules
  namespace: monitoring
spec:
  groups:
    - name: checkout.rules
      rules:
        - alert: CheckoutHighLatency
          expr: histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))
        - record: job:checkout_requests:rate5m
          expr: sum(rate(http_requests_total[5m])) by (job)
---
apiVersion: monitoring.coreos.com/v1alpha1
kind: ScrapeConfig
metadata:
  name: external-api
  namespace: monitoring
spec:
  jobName: external-api-private
  staticConfigs:
    - targets:
        - private-api.internal.example:443
        - backup-api.internal.example:443
      labels:
        team: checkout
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	scrapes := yamlBucketForTest(t, got, "observability_declared_scrape_configs")
	if len(scrapes) != 3 {
		t.Fatalf("len(observability_declared_scrape_configs) = %d, want 3: %#v", len(scrapes), scrapes)
	}
	serviceMonitor := yamlNamedRowForTest(t, scrapes, "scrape_config.ServiceMonitor.checkout-service")
	assertYAMLField(t, serviceMonitor, "source_kind", "kubernetes")
	assertYAMLField(t, serviceMonitor, "declaration_kind", "prometheus_service_monitor")
	assertYAMLField(t, serviceMonitor, "resource_kind", "ServiceMonitor")
	assertYAMLField(t, serviceMonitor, "namespace", "monitoring")
	assertYAMLField(t, serviceMonitor, "selector_label_keys", "app.kubernetes.io/name")
	assertYAMLField(t, serviceMonitor, "release_label_present", true)
	assertYAMLField(t, serviceMonitor, "endpoint_count", 1)
	assertYAMLField(t, serviceMonitor, "port_names", "http-metrics")
	assertYAMLField(t, serviceMonitor, "namespace_selector_state", "named")
	assertYAMLField(t, serviceMonitor, "outcome", "exact")
	if fingerprint, _ := serviceMonitor["selector_identity_fingerprint"].(string); strings.TrimSpace(fingerprint) == "" {
		t.Fatalf("selector_identity_fingerprint = %#v, want non-empty", serviceMonitor["selector_identity_fingerprint"])
	}
	assertYAMLForbiddenValuesAbsent(t, serviceMonitor, "checkout-api")

	podMonitor := yamlNamedRowForTest(t, scrapes, "scrape_config.PodMonitor.checkout-pods")
	assertYAMLField(t, podMonitor, "declaration_kind", "prometheus_pod_monitor")
	assertYAMLField(t, podMonitor, "selector_label_keys", "app")
	assertYAMLField(t, podMonitor, "endpoint_count", 1)
	assertYAMLField(t, podMonitor, "port_names", "metrics")
	assertYAMLForbiddenValuesAbsent(t, podMonitor, "checkout-worker")

	scrapeConfig := yamlNamedRowForTest(t, scrapes, "scrape_config.ScrapeConfig.external-api")
	assertYAMLField(t, scrapeConfig, "declaration_kind", "prometheus_scrape_config")
	assertYAMLField(t, scrapeConfig, "target_count", 2)
	assertYAMLField(t, scrapeConfig, "redaction_state", "redacted")
	assertYAMLFieldContains(t, scrapeConfig, "redacted_fields", "staticConfigs.targets")
	if fingerprint, _ := scrapeConfig["job_name_fingerprint"].(string); strings.TrimSpace(fingerprint) == "" {
		t.Fatalf("job_name_fingerprint = %#v, want non-empty", scrapeConfig["job_name_fingerprint"])
	}
	assertYAMLForbiddenKeysAbsent(t, scrapeConfig, "jobName", "staticConfigs", "targets", "labels")
	assertYAMLForbiddenValuesAbsent(t, scrapeConfig, "external-api-private", "private-api.internal.example", "checkout")

	rules := yamlBucketForTest(t, got, "observability_declared_metric_rules")
	if len(rules) != 2 {
		t.Fatalf("len(observability_declared_metric_rules) = %d, want 2: %#v", len(rules), rules)
	}
	alert := yamlMetricRuleRowForTest(t, rules, "alert")
	assertYAMLField(t, alert, "declaration_kind", "prometheus_rule")
	assertYAMLField(t, alert, "rule_group", "checkout.rules")
	assertYAMLField(t, alert, "outcome", "exact")
	if fingerprint, _ := alert["alert_rule_name_fingerprint"].(string); strings.TrimSpace(fingerprint) == "" {
		t.Fatalf("alert_rule_name_fingerprint = %#v, want non-empty", alert["alert_rule_name_fingerprint"])
	}
	assertYAMLForbiddenKeysAbsent(t, alert, "expr", "query")
	assertYAMLForbiddenValuesAbsent(t, alert, "CheckoutHighLatency", "histogram_quantile", "http_request_duration")

	record := yamlMetricRuleRowForTest(t, rules, "record")
	assertYAMLField(t, record, "rule_group", "checkout.rules")
	if fingerprint, _ := record["record_rule_name_fingerprint"].(string); strings.TrimSpace(fingerprint) == "" {
		t.Fatalf("record_rule_name_fingerprint = %#v, want non-empty", record["record_rule_name_fingerprint"])
	}
	assertYAMLForbiddenValuesAbsent(t, record, "job:checkout_requests:rate5m", "http_requests_total")
}

func TestParsePrometheusMetricEvidenceEdgeOutcomes(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "manifests/prometheus-edge.yaml", `
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: missing-selector
  namespace: monitoring
spec:
  endpoints:
    - port: http
---
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: duplicate-rules-one
  namespace: monitoring
spec:
  groups:
    - name: duplicate.rules
      rules:
        - alert: DuplicateAlert
          expr: vector(1)
---
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: duplicate-rules-two
  namespace: monitoring
spec:
  groups:
    - name: duplicate.rules
      rules:
        - alert: DuplicateAlert
          expr: vector(2)
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	scrapes := yamlBucketForTest(t, got, "observability_declared_scrape_configs")
	if len(scrapes) != 1 {
		t.Fatalf("len(observability_declared_scrape_configs) = %d, want 1: %#v", len(scrapes), scrapes)
	}
	assertYAMLField(t, scrapes[0], "outcome", "unresolved")

	warnings := yamlBucketForTest(t, got, "observability_coverage_warnings")
	missingSelector := yamlWarningRowForTest(t, warnings, "missing_selector")
	assertYAMLField(t, missingSelector, "outcome", "unresolved")

	rules := yamlBucketForTest(t, got, "observability_declared_metric_rules")
	if len(rules) != 2 {
		t.Fatalf("len(observability_declared_metric_rules) = %d, want 2: %#v", len(rules), rules)
	}
	for _, row := range rules {
		assertYAMLField(t, row, "outcome", "ambiguous")
		assertYAMLField(t, row, "duplicate_metric_rule_identity", true)
		assertYAMLForbiddenValuesAbsent(t, row, "DuplicateAlert", "vector(")
	}
}

func TestParseHelmValuesPrometheusMimirOtelAndYACEMetricRoutes(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, filepath.Join("environments", "platform-prod", "values.yaml"), `
prometheus:
  prometheusSpec:
    serviceMonitorSelector:
      matchLabels:
        release: monitoring
    remoteWrite:
      - url: http://mimir.prod.example/api/v1/push
        headers:
          X-Scope-OrgID: prod-tenant
serviceMonitor:
  enabled: true
  labels:
    release: monitoring
mimir:
  structuredConfig:
    ruler_storage:
      backend: s3
    limits:
      accept_ha_samples: true
      ha_cluster_label: cluster
  gateway:
    nginx:
      config:
        serverSnippet: |
          location /prometheus/api/v1/push {
            proxy_pass http://mimir-distributor:8080/api/v1/push;
          }
config:
  receivers:
    prometheus:
      config:
        scrape_configs:
          - job_name: 'yace'
            static_configs:
              - targets: ['yace.monitoring.svc.cluster.local:80']
  exporters:
    otlphttp/mimir:
      endpoint: http://mimir.prod.example/otlp
      headers:
        X-Scope-OrgID: prod-tenant
  service:
    pipelines:
      metrics:
        receivers: [prometheus]
        processors: [batch]
        exporters: [otlphttp/mimir]
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	scrapes := yamlBucketForTest(t, got, "observability_declared_scrape_configs")
	if len(scrapes) != 3 {
		t.Fatalf("len(observability_declared_scrape_configs) = %d, want 3: %#v", len(scrapes), scrapes)
	}
	selector := yamlNamedRowForTest(t, scrapes, "scrape_config.helm.service_monitor_selector")
	assertYAMLField(t, selector, "source_kind", "helm")
	assertYAMLField(t, selector, "declaration_kind", "helm_prometheus_service_monitor_selector")
	assertYAMLField(t, selector, "selector_label_keys", "release")
	assertYAMLField(t, selector, "release_label_present", true)
	assertYAMLForbiddenValuesAbsent(t, selector, "monitoring")

	yace := yamlNamedRowForTest(t, scrapes, "scrape_config.helm.service_monitor")
	assertYAMLField(t, yace, "declaration_kind", "helm_service_monitor_values")
	assertYAMLField(t, yace, "release_label_present", true)
	assertYAMLForbiddenValuesAbsent(t, yace, "monitoring")

	otelScrape := yamlNamedRowForTest(t, scrapes, "scrape_config.otel.prometheus.0")
	assertYAMLField(t, otelScrape, "declaration_kind", "otel_prometheus_receiver_scrape_config")
	assertYAMLField(t, otelScrape, "target_count", 1)
	assertYAMLField(t, otelScrape, "redaction_state", "redacted")
	assertYAMLFieldContains(t, otelScrape, "redacted_fields", "scrape_configs.static_configs.targets")
	if fingerprint, _ := otelScrape["job_name_fingerprint"].(string); strings.TrimSpace(fingerprint) == "" {
		t.Fatalf("job_name_fingerprint = %#v, want non-empty", otelScrape["job_name_fingerprint"])
	}
	assertYAMLForbiddenValuesAbsent(t, otelScrape, "yace.monitoring.svc.cluster.local")

	routes := yamlBucketForTest(t, got, "observability_declared_metric_routes")
	if len(routes) != 3 {
		t.Fatalf("len(observability_declared_metric_routes) = %d, want 3: %#v", len(routes), routes)
	}
	remoteWrite := yamlNamedRowForTest(t, routes, "metric_route.prometheus.remote_write.0")
	assertYAMLField(t, remoteWrite, "declaration_kind", "helm_prometheus_remote_write")
	assertYAMLField(t, remoteWrite, "backend_kind", "mimir")
	assertYAMLField(t, remoteWrite, "redaction_state", "redacted")
	assertYAMLFieldContains(t, remoteWrite, "redacted_fields", "url")
	assertYAMLFieldContains(t, remoteWrite, "redacted_fields", "headers")
	assertYAMLForbiddenValuesAbsent(t, remoteWrite, "mimir.prod.example", "prod-tenant")

	otel := yamlNamedRowForTest(t, routes, "metric_route.otel.metrics")
	assertYAMLField(t, otel, "declaration_kind", "otel_metric_pipeline")
	assertYAMLField(t, otel, "backend_kind", "mimir")
	assertYAMLField(t, otel, "exporter_refs", "otlphttp/mimir")
	assertYAMLField(t, otel, "receiver_refs", "prometheus")
	assertYAMLForbiddenValuesAbsent(t, otel, "mimir.prod.example", "prod-tenant")

	mimir := yamlNamedRowForTest(t, routes, "metric_route.mimir.gateway")
	assertYAMLField(t, mimir, "declaration_kind", "mimir_gateway_route")
	assertYAMLField(t, mimir, "backend_kind", "mimir")
	assertYAMLField(t, mimir, "tenant_scope_state", "configured")
	assertYAMLForbiddenValuesAbsent(t, mimir, "mimir-distributor")
}

func TestParseMalformedMetricResourcesEmitWarnings(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "manifests/malformed-prometheus.yaml", `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: malformed-rule
  namespace: monitoring
spec:
  groups:
    - name: malformed.rules
      rules: not-a-list
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	if rules := yamlBucketForTest(t, got, "observability_declared_metric_rules"); len(rules) != 0 {
		t.Fatalf("observability_declared_metric_rules = %#v, want empty", rules)
	}
	warnings := yamlBucketForTest(t, got, "observability_coverage_warnings")
	warning := yamlWarningRowForTest(t, warnings, "malformed_metric_rule")
	assertYAMLField(t, warning, "outcome", "rejected")
	assertYAMLForbiddenValuesAbsent(t, warning, "malformed.rules")
}

func TestParseHelmServiceMonitorMissingDiscoveryLabelsWarns(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "values.yaml", `
serviceMonitor:
  enabled: true
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	scrapes := yamlBucketForTest(t, got, "observability_declared_scrape_configs")
	if len(scrapes) != 1 {
		t.Fatalf("len(observability_declared_scrape_configs) = %d, want 1: %#v", len(scrapes), scrapes)
	}
	assertYAMLField(t, scrapes[0], "outcome", "unresolved")

	warnings := yamlBucketForTest(t, got, "observability_coverage_warnings")
	warning := yamlWarningRowForTest(t, warnings, "missing_discovery_label")
	assertYAMLField(t, warning, "outcome", "unresolved")
}

func yamlMetricRuleRowForTest(t *testing.T, rows []map[string]any, ruleKind string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["rule_kind"] == ruleKind {
			return row
		}
	}
	t.Fatalf("missing metric rule kind %q in %#v", ruleKind, rows)
	return nil
}

func yamlWarningRowForTest(t *testing.T, rows []map[string]any, warningKind string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["warning_kind"] == warningKind {
			return row
		}
	}
	t.Fatalf("missing warning kind %q in %#v", warningKind, rows)
	return nil
}
