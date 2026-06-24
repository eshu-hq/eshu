// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestTerraformParseGrafanaDeclaredResources(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "grafana.tf", `
resource "grafana_data_source" "prometheus" {
  type = "prometheus"
  uid  = "prom-prod"
  name = "Prometheus Prod"
  url  = "https://prometheus.internal.example"
  secure_json_data_encoded = jsonencode({ basicAuthPassword = "super-secret" })
}

resource "grafana_folder" "checkout" {
  uid   = "checkout-folder"
  title = "Checkout Private Folder"
}

resource "grafana_dashboard" "checkout" {
  folder      = grafana_folder.checkout.uid
  config_json = jsonencode({
    uid   = "checkout-dashboard"
    title = "Checkout Dashboard"
    panels = [{
      datasource = { uid = "prom-prod", type = "prometheus" }
      targets = [{ expr = "sum(rate(http_requests_total[5m]))" }]
    }]
  })
}

resource "grafana_rule_group" "checkout" {
  name       = "checkout.rules"
  folder_uid = grafana_folder.checkout.uid
  rule {
    name      = "Checkout Errors"
    condition = "A"
    data {
      ref_id         = "A"
      datasource_uid = "prom-prod"
      model          = jsonencode({ expr = "sum(rate(errors_total[5m]))" })
    }
  }
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	datasources := bucketForTest(t, got, "observability_declared_datasources")
	if len(datasources) != 1 {
		t.Fatalf("len(observability_declared_datasources) = %d, want 1: %#v", len(datasources), datasources)
	}
	datasource := datasources[0]
	assertHCLField(t, datasource, "source_kind", "terraform")
	assertHCLField(t, datasource, "declaration_kind", "terraform_resource")
	assertHCLField(t, datasource, "datasource_uid", "prom-prod")
	assertHCLField(t, datasource, "datasource_type", "prometheus")
	assertHCLFieldContains(t, datasource, "redacted_fields", "secure_json_data_encoded")
	assertHCLFieldContains(t, datasource, "redacted_fields", "url")
	assertHCLForbiddenKeysAbsent(t, datasource, "url", "secure_json_data_encoded")
	assertHCLForbiddenValuesAbsent(t, datasource, "Prometheus Prod", "prometheus.internal.example", "super-secret")

	folders := bucketForTest(t, got, "observability_declared_folders")
	if len(folders) != 1 {
		t.Fatalf("len(observability_declared_folders) = %d, want 1: %#v", len(folders), folders)
	}
	folder := folders[0]
	assertHCLField(t, folder, "resource_type", "grafana_folder")
	assertHCLField(t, folder, "folder_uid", "checkout-folder")
	if fingerprint, _ := folder["folder_title_fingerprint"].(string); strings.TrimSpace(fingerprint) == "" {
		t.Fatalf("folder_title_fingerprint = %#v, want non-empty", folder["folder_title_fingerprint"])
	}
	assertHCLForbiddenValuesAbsent(t, folder, "Checkout Private Folder")

	dashboards := bucketForTest(t, got, "observability_declared_dashboards")
	if len(dashboards) != 1 {
		t.Fatalf("len(observability_declared_dashboards) = %d, want 1: %#v", len(dashboards), dashboards)
	}
	dashboard := dashboards[0]
	assertHCLField(t, dashboard, "resource_type", "grafana_dashboard")
	assertHCLField(t, dashboard, "dashboard_uid", "checkout-dashboard")
	assertHCLFieldContains(t, dashboard, "datasource_refs", "uid:prom-prod")
	assertHCLFieldContains(t, dashboard, "datasource_refs", "type:prometheus")
	assertHCLField(t, dashboard, "outcome", "exact")
	if fingerprint, _ := dashboard["dashboard_title_fingerprint"].(string); strings.TrimSpace(fingerprint) == "" {
		t.Fatalf("dashboard_title_fingerprint = %#v, want non-empty", dashboard["dashboard_title_fingerprint"])
	}
	assertHCLForbiddenKeysAbsent(t, dashboard, "config_json", "json", "panels", "targets", "expr")
	assertHCLForbiddenValuesAbsent(t, dashboard, "Checkout Dashboard", "sum(rate")

	alerts := bucketForTest(t, got, "observability_declared_alert_rules")
	if len(alerts) != 1 {
		t.Fatalf("len(observability_declared_alert_rules) = %d, want 1: %#v", len(alerts), alerts)
	}
	alert := alerts[0]
	assertHCLField(t, alert, "resource_type", "grafana_rule_group")
	assertHCLField(t, alert, "rule_group", "checkout.rules")
	assertHCLFieldContains(t, alert, "datasource_refs", "uid:prom-prod")
	assertHCLForbiddenKeysAbsent(t, alert, "model", "expr", "query")
	assertHCLForbiddenValuesAbsent(t, alert, "Checkout Errors", "errors_total")
}

func TestTerraformParseGrafanaDatasourceExpressionTypeIsUnresolvedNotUnsupported(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "dynamic-grafana.tf", `
variable "grafana_datasource_type" {
  type = string
}

resource "grafana_data_source" "dynamic" {
  type = var.grafana_datasource_type
  uid  = var.grafana_datasource_uid
  name = "Dynamic Datasource"
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	datasources := bucketForTest(t, got, "observability_declared_datasources")
	if len(datasources) != 1 {
		t.Fatalf("len(observability_declared_datasources) = %d, want 1: %#v", len(datasources), datasources)
	}
	assertHCLField(t, datasources[0], "name", "datasource.dynamic")
	assertHCLField(t, datasources[0], "datasource_type_resolution", "unresolved")
	assertHCLField(t, datasources[0], "outcome", "derived")
	assertHCLForbiddenValuesAbsent(t, datasources[0], "Dynamic Datasource")
	if warnings := bucketForTest(t, got, "observability_coverage_warnings"); len(warnings) != 0 {
		t.Fatalf("observability_coverage_warnings = %#v, want empty", warnings)
	}
}

func assertHCLField(t *testing.T, row map[string]any, key string, want any) {
	t.Helper()
	if got := row[key]; got != want {
		t.Fatalf("%s = %#v, want %#v in %#v", key, got, want, row)
	}
}

func assertHCLFieldContains(t *testing.T, row map[string]any, key string, want string) {
	t.Helper()
	got, _ := row[key].(string)
	if !strings.Contains(got, want) {
		t.Fatalf("%s = %q, want substring %q in %#v", key, got, want, row)
	}
}

func assertHCLForbiddenKeysAbsent(t *testing.T, row map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, exists := row[key]; exists {
			t.Fatalf("forbidden key %q present in %#v", key, row)
		}
	}
}

func assertHCLForbiddenValuesAbsent(t *testing.T, row map[string]any, values ...string) {
	t.Helper()
	for _, value := range values {
		for _, rendered := range mapValuesForHCLTest(row) {
			if strings.Contains(strings.ToLower(rendered), strings.ToLower(value)) {
				t.Fatalf("forbidden value %q present in %#v", value, row)
			}
		}
	}
}

func mapValuesForHCLTest(row map[string]any) []string {
	values := make([]string, 0, len(row))
	for _, value := range row {
		values = append(values, fmt.Sprint(value))
	}
	return values
}
