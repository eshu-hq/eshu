// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGrafanaDashboardResourceEmitsDeclaredMetadataOnlyEvidence(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "dashboards/checkout.yaml", `
apiVersion: grafana.integreatly.org/v1beta1
kind: GrafanaDashboard
metadata:
  name: checkout-latency
  namespace: observability
  labels:
    app.kubernetes.io/name: checkout
spec:
  folder: checkout
  json: |
    {
      "uid": "checkout-latency",
      "title": "Checkout Latency",
      "tags": ["service:checkout", "team-private"],
      "panels": [
        {
          "title": "p95",
          "datasource": {"type": "prometheus", "uid": "prom-prod"},
          "targets": [{"expr": "histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))"}]
        }
      ]
    }
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	dashboards := yamlBucketForTest(t, got, "observability_declared_dashboards")
	if len(dashboards) != 1 {
		t.Fatalf("len(observability_declared_dashboards) = %d, want 1: %#v", len(dashboards), dashboards)
	}
	row := dashboards[0]
	assertYAMLField(t, row, "source_class", "declared")
	assertYAMLField(t, row, "source_kind", "kubernetes")
	assertYAMLField(t, row, "declaration_kind", "grafana_dashboard_resource")
	assertYAMLField(t, row, "resource_kind", "GrafanaDashboard")
	assertYAMLField(t, row, "namespace", "observability")
	assertYAMLField(t, row, "dashboard_uid", "checkout-latency")
	assertYAMLField(t, row, "folder", "checkout")
	assertYAMLField(t, row, "outcome", "exact")
	assertYAMLFieldContains(t, row, "datasource_refs", "uid:prom-prod")
	assertYAMLFieldContains(t, row, "datasource_refs", "type:prometheus")
	assertYAMLFieldContains(t, row, "service_hints", "checkout")
	if fingerprint, _ := row["dashboard_title_fingerprint"].(string); strings.TrimSpace(fingerprint) == "" {
		t.Fatalf("dashboard_title_fingerprint = %#v, want non-empty", row["dashboard_title_fingerprint"])
	}
	assertYAMLForbiddenKeysAbsent(t, row, "title", "dashboard_json", "json", "panels", "targets", "expr", "query")
	assertYAMLForbiddenValuesAbsent(t, row, "Checkout Latency", "histogram_quantile", "team-private")
}

func TestParseGrafanaFolderResourceAndProvisioning(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, filepath.Join("environments", "prod", "grafana-folders.yaml"), `
apiVersion: grafana.integreatly.org/v1beta1
kind: GrafanaFolder
metadata:
  name: checkout-folder
  namespace: observability
spec:
  uid: checkout-folder
  title: Checkout Private Folder
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-dashboard-provisioning
  namespace: observability
data:
  dashboards.yaml: |
    apiVersion: 1
    providers:
      - name: checkout-provider
        folder: Checkout Provisioned Folder
        folderUid: checkout-provisioned
        options:
          path: /var/lib/grafana/dashboards/checkout
      - name: checkout-title-only-provider
        folder: Checkout Title Only Folder
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	folders := yamlBucketForTest(t, got, "observability_declared_folders")
	if len(folders) != 3 {
		t.Fatalf("len(observability_declared_folders) = %d, want 3: %#v", len(folders), folders)
	}
	resource := yamlNamedRowForTest(t, folders, "folder.checkout-folder")
	assertYAMLField(t, resource, "source_kind", "kubernetes")
	assertYAMLField(t, resource, "resource_kind", "GrafanaFolder")
	assertYAMLField(t, resource, "folder_uid", "checkout-folder")
	assertYAMLField(t, resource, "environment", "prod")
	if fingerprint, _ := resource["folder_title_fingerprint"].(string); strings.TrimSpace(fingerprint) == "" {
		t.Fatalf("folder_title_fingerprint = %#v, want non-empty", resource["folder_title_fingerprint"])
	}
	assertYAMLForbiddenValuesAbsent(t, resource, "Checkout Private Folder")

	provisioned := yamlNamedRowForTest(t, folders, "folder.checkout-provisioned")
	assertYAMLField(t, provisioned, "declaration_kind", "grafana_folder_provisioning")
	assertYAMLField(t, provisioned, "folder_uid", "checkout-provisioned")
	assertYAMLForbiddenValuesAbsent(t, provisioned, "Checkout Provisioned Folder", "/var/lib/grafana")

	titleOnly := yamlFolderRowWithoutUIDForTest(t, folders)
	assertYAMLField(t, titleOnly, "declaration_kind", "grafana_folder_provisioning")
	if titleOnly["name"] == "folder.grafana-dashboard-provisioning" {
		t.Fatalf("title-only folder name = %q, want title-derived identity in %#v", titleOnly["name"], titleOnly)
	}
	assertYAMLForbiddenValuesAbsent(t, titleOnly, "Checkout Title Only Folder")
}

func TestParseGrafanaConfigMapProvisioningRedactsSecretsAndQueries(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "manifests/grafana-config.yaml", `
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-provisioning
  namespace: observability
data:
  datasources.yaml: |
    apiVersion: 1
    datasources:
      - name: Prometheus Prod
        uid: prom-prod
        type: prometheus
        url: https://prometheus.internal.example
        secureJsonData:
          basicAuthPassword: super-secret
      - name: Private Plugin
        uid: private-plugin
        type: custom-private-plugin
  alerts.yaml: |
    apiVersion: 1
    groups:
      - orgId: 1
        name: checkout.rules
        folder: checkout
        rules:
          - uid: checkout-high-latency
            title: Checkout High Latency
            condition: A
            data:
              - refId: A
                datasourceUid: prom-prod
                model:
                  expr: sum(rate(http_request_duration_seconds_count[5m]))
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	datasources := yamlBucketForTest(t, got, "observability_declared_datasources")
	if len(datasources) != 2 {
		t.Fatalf("len(observability_declared_datasources) = %d, want 2: %#v", len(datasources), datasources)
	}
	prometheus := yamlNamedRowForTest(t, datasources, "datasource.prom-prod")
	assertYAMLField(t, prometheus, "datasource_uid", "prom-prod")
	assertYAMLField(t, prometheus, "datasource_type", "prometheus")
	assertYAMLField(t, prometheus, "redaction_state", "redacted")
	assertYAMLFieldContains(t, prometheus, "redacted_fields", "secureJsonData")
	assertYAMLFieldContains(t, prometheus, "redacted_fields", "url")
	assertYAMLForbiddenKeysAbsent(t, prometheus, "url", "secureJsonData", "basicAuthPassword", "password")
	assertYAMLForbiddenValuesAbsent(t, prometheus, "Prometheus Prod", "prometheus.internal.example", "super-secret")

	unsupported := yamlNamedRowForTest(t, datasources, "datasource.private-plugin")
	assertYAMLField(t, unsupported, "outcome", "unsupported")

	alerts := yamlBucketForTest(t, got, "observability_declared_alert_rules")
	if len(alerts) != 1 {
		t.Fatalf("len(observability_declared_alert_rules) = %d, want 1: %#v", len(alerts), alerts)
	}
	alert := alerts[0]
	assertYAMLField(t, alert, "alert_rule_uid", "checkout-high-latency")
	assertYAMLField(t, alert, "rule_group", "checkout.rules")
	assertYAMLFieldContains(t, alert, "datasource_refs", "uid:prom-prod")
	if fingerprint, _ := alert["alert_rule_title_fingerprint"].(string); strings.TrimSpace(fingerprint) == "" {
		t.Fatalf("alert_rule_title_fingerprint = %#v, want non-empty", alert["alert_rule_title_fingerprint"])
	}
	assertYAMLForbiddenKeysAbsent(t, alert, "title", "model", "expr", "query")
	assertYAMLForbiddenValuesAbsent(t, alert, "Checkout High Latency", "sum(rate")

	warnings := yamlBucketForTest(t, got, "observability_coverage_warnings")
	if len(warnings) != 1 {
		t.Fatalf("len(observability_coverage_warnings) = %d, want 1: %#v", len(warnings), warnings)
	}
	assertYAMLField(t, warnings[0], "outcome", "unsupported")
	assertYAMLField(t, warnings[0], "warning_kind", "unsupported_datasource_type")
	assertYAMLForbiddenValuesAbsent(t, warnings[0], "prometheus.internal.example", "super-secret")
}

func TestParseMalformedGrafanaDashboardEmitsRejectedWarning(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "dashboards/malformed.yaml", `
apiVersion: grafana.integreatly.org/v1beta1
kind: GrafanaDashboard
metadata:
  name: broken-dashboard
  namespace: observability
spec:
  json: |
    {"title":"Broken Dashboard","panels":[
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	if dashboards := yamlBucketForTest(t, got, "observability_declared_dashboards"); len(dashboards) != 0 {
		t.Fatalf("observability_declared_dashboards = %#v, want empty", dashboards)
	}
	warnings := yamlBucketForTest(t, got, "observability_coverage_warnings")
	if len(warnings) != 1 {
		t.Fatalf("len(observability_coverage_warnings) = %d, want 1: %#v", len(warnings), warnings)
	}
	assertYAMLField(t, warnings[0], "warning_kind", "malformed_dashboard_json")
	assertYAMLField(t, warnings[0], "outcome", "rejected")
	assertYAMLForbiddenValuesAbsent(t, warnings[0], "Broken Dashboard")
}

func TestParseHelmValuesGrafanaEvidence(t *testing.T) {
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
          url: https://tempo.internal.example
  dashboards:
    checkout:
      latency:
        json: |
          {"uid":"checkout-helm","title":"Synthetic Checkout Helm Dashboard","panels":[{"datasource":{"type":"tempo","uid":"tempo-prod"},"targets":[{"query":"{service.name=\"checkout\"}"}]}]}
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	dashboards := yamlBucketForTest(t, got, "observability_declared_dashboards")
	if len(dashboards) != 1 {
		t.Fatalf("len(observability_declared_dashboards) = %d, want 1: %#v", len(dashboards), dashboards)
	}
	assertYAMLField(t, dashboards[0], "source_kind", "helm")
	assertYAMLField(t, dashboards[0], "dashboard_uid", "checkout-helm")
	assertYAMLField(t, dashboards[0], "environment", "prod")
	assertYAMLFieldContains(t, dashboards[0], "datasource_refs", "uid:tempo-prod")
	assertYAMLForbiddenValuesAbsent(t, dashboards[0], "Synthetic Checkout Helm Dashboard", "{service.name")

	datasources := yamlBucketForTest(t, got, "observability_declared_datasources")
	if len(datasources) != 1 {
		t.Fatalf("len(observability_declared_datasources) = %d, want 1: %#v", len(datasources), datasources)
	}
	assertYAMLField(t, datasources[0], "source_kind", "helm")
	assertYAMLField(t, datasources[0], "datasource_uid", "tempo-prod")
	assertYAMLField(t, datasources[0], "datasource_type", "tempo")
	assertYAMLForbiddenValuesAbsent(t, datasources[0], "tempo.internal.example")
}

func TestParseKustomizeOverlayDoesNotInventGrafanaEvidence(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "overlays/prod/kustomization.yaml", `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - deployment.yaml
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	for _, bucket := range []string{
		"observability_declared_folders",
		"observability_declared_dashboards",
		"observability_declared_datasources",
		"observability_declared_alert_rules",
		"observability_declared_scrape_configs",
		"observability_declared_metric_rules",
		"observability_declared_metric_routes",
		"observability_declared_log_routes",
		"observability_declared_trace_routes",
		"observability_coverage_warnings",
	} {
		if rows := yamlBucketForTest(t, got, bucket); len(rows) != 0 {
			t.Fatalf("%s = %#v, want empty", bucket, rows)
		}
	}
}

func TestParseDuplicateGrafanaDashboardsMarksAmbiguous(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "dashboards.yaml", `
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-dashboards
  namespace: observability
data:
  first.json: |
    {"uid":"checkout-dup","title":"Checkout One","panels":[]}
  second.json: |
    {"uid":"checkout-dup","title":"Checkout Two","panels":[]}
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	dashboards := yamlBucketForTest(t, got, "observability_declared_dashboards")
	if len(dashboards) != 2 {
		t.Fatalf("len(observability_declared_dashboards) = %d, want 2: %#v", len(dashboards), dashboards)
	}
	for _, row := range dashboards {
		assertYAMLField(t, row, "outcome", "ambiguous")
		assertYAMLField(t, row, "duplicate_dashboard_identity", true)
	}
}

func writeYAMLTestFile(t *testing.T, name string, body string) string {
	t.Helper()
	filePath := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(filePath), err)
	}
	if err := os.WriteFile(filePath, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", filePath, err)
	}
	return filePath
}

func yamlBucketForTest(t *testing.T, payload map[string]any, key string) []map[string]any {
	t.Helper()
	rows, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	return rows
}

func yamlNamedRowForTest(t *testing.T, rows []map[string]any, name string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["name"] == name {
			return row
		}
	}
	t.Fatalf("missing row named %q in %#v", name, rows)
	return nil
}

func yamlFolderRowWithoutUIDForTest(t *testing.T, rows []map[string]any) map[string]any {
	t.Helper()
	for _, row := range rows {
		if _, hasUID := row["folder_uid"]; hasUID {
			continue
		}
		if fingerprint, _ := row["folder_title_fingerprint"].(string); strings.TrimSpace(fingerprint) != "" {
			return row
		}
	}
	t.Fatalf("missing title-fingerprinted folder without UID in %#v", rows)
	return nil
}

func assertYAMLField(t *testing.T, row map[string]any, key string, want any) {
	t.Helper()
	if got := row[key]; got != want {
		t.Fatalf("%s = %#v, want %#v in %#v", key, got, want, row)
	}
}

func assertYAMLFieldContains(t *testing.T, row map[string]any, key string, want string) {
	t.Helper()
	got, _ := row[key].(string)
	if !strings.Contains(got, want) {
		t.Fatalf("%s = %q, want substring %q in %#v", key, got, want, row)
	}
}

func assertYAMLForbiddenKeysAbsent(t *testing.T, row map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, exists := row[key]; exists {
			t.Fatalf("forbidden key %q present in %#v", key, row)
		}
	}
}

func assertYAMLForbiddenValuesAbsent(t *testing.T, row map[string]any, values ...string) {
	t.Helper()
	rendered := strings.ToLower(strings.Join(mapValuesForTest(row), " "))
	for _, value := range values {
		if strings.Contains(rendered, strings.ToLower(value)) {
			t.Fatalf("forbidden value %q present in %#v", value, row)
		}
	}
}

func mapValuesForTest(row map[string]any) []string {
	values := make([]string, 0, len(row))
	for _, value := range row {
		values = append(values, fmt.Sprint(value))
	}
	return values
}
