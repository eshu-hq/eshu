// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"testing"
)

// TestParseHelmValuesEmitsBaseRowAndGrafanaObservabilityFromSameFile pins the
// current output shape for a values.yaml that both defines base Helm values
// (top-level keys, image repositories) and a Grafana provisioning section.
//
// Regression context: issue #4847. parseHelmValues and
// appendHelmGrafanaObservability each independently DecodeDocuments the same
// file source (language.go:44 and :56). A refactor to share one decoded
// documents slice between the two extraction paths MUST NOT change either
// output — this test pins both outputs from a single Parse() call so the
// refactor can be proven equivalent.
func TestParseHelmValuesEmitsBaseRowAndGrafanaObservabilityFromSameFile(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "values.yaml", `
replicaCount: 2
image:
  repository: ghcr.io/example/checkout-service
  tag: "1.2.3"
service:
  port: 8080
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

	// Base helm_values row (parseHelmValues output).
	values := yamlBucketForTest(t, got, "helm_values")
	if len(values) != 1 {
		t.Fatalf("len(helm_values) = %d, want 1: %#v", len(values), values)
	}
	assertYAMLField(t, values[0], "top_level_keys", "grafana,image,replicaCount,service")
	assertYAMLField(t, values[0], "image_repositories", "ghcr.io/example/checkout-service")
	assertYAMLField(t, values[0], "lang", "yaml")

	// Grafana observability rows (appendHelmGrafanaObservability output),
	// decoded from the SAME file source.
	dashboards := yamlBucketForTest(t, got, "observability_declared_dashboards")
	if len(dashboards) != 1 {
		t.Fatalf("len(observability_declared_dashboards) = %d, want 1: %#v", len(dashboards), dashboards)
	}
	assertYAMLField(t, dashboards[0], "source_kind", "helm")
	assertYAMLField(t, dashboards[0], "dashboard_uid", "checkout-helm")

	datasources := yamlBucketForTest(t, got, "observability_declared_datasources")
	if len(datasources) != 1 {
		t.Fatalf("len(observability_declared_datasources) = %d, want 1: %#v", len(datasources), datasources)
	}
	assertYAMLField(t, datasources[0], "source_kind", "helm")
	assertYAMLField(t, datasources[0], "datasource_uid", "tempo-prod")
}
