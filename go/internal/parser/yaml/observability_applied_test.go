// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import "testing"

func TestParseArgoApplicationStatusEmitsAppliedObservabilityState(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "clusters/prod/argocd/observability-app.yaml", `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: observability
  namespace: argocd
spec:
  project: platform
  destination:
    name: prod-us-east-1
    server: https://kubernetes.default.svc
    namespace: observability
  source:
    repoURL: https://github.com/example/iac-observability
    path: environments/prod
    targetRevision: main
status:
  sync:
    status: Synced
    revision: 0123456789abcdef0123456789abcdef01234567
  health:
    status: Healthy
  operationState:
    phase: Succeeded
  reconciledAt: "2026-06-01T12:00:00Z"
  resources:
    - group: grafana.integreatly.org
      kind: GrafanaDashboard
      namespace: observability
      name: checkout-overview
      status: Synced
      health:
        status: Healthy
    - group: monitoring.coreos.com
      kind: ServiceMonitor
      namespace: payments
      name: checkout-api
      status: OutOfSync
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	syncStates := yamlBucketForTest(t, got, "observability_applied_sync_states")
	if got, want := len(syncStates), 1; got != want {
		t.Fatalf("len(observability_applied_sync_states) = %d, want %d: %#v", got, want, syncStates)
	}
	assertYAMLField(t, syncStates[0], "source_class", "applied")
	assertYAMLField(t, syncStates[0], "source_kind", "argocd")
	assertYAMLField(t, syncStates[0], "app_name", "observability")
	assertYAMLField(t, syncStates[0], "cluster_name", "prod-us-east-1")
	assertYAMLField(t, syncStates[0], "sync_status", "Synced")
	assertYAMLField(t, syncStates[0], "health_status", "Healthy")
	assertYAMLField(t, syncStates[0], "outcome", "exact")
	assertYAMLForbiddenValuesAbsent(t, syncStates[0], "https://kubernetes.default.svc")

	resources := yamlBucketForTest(t, got, "observability_applied_resources")
	if got, want := len(resources), 2; got != want {
		t.Fatalf("len(observability_applied_resources) = %d, want %d: %#v", got, want, resources)
	}
	assertYAMLField(t, resources[0], "source_class", "applied")
	assertYAMLField(t, resources[0], "source_kind", "argocd")
	assertYAMLField(t, resources[0], "resource_kind", "GrafanaDashboard")
	assertYAMLField(t, resources[0], "resource_namespace", "observability")
	assertYAMLField(t, resources[0], "observability_resource_class", "dashboard")
	assertYAMLField(t, resources[0], "outcome", "exact")
	assertYAMLField(t, resources[1], "observability_resource_class", "scrape_config")
	assertYAMLField(t, resources[1], "outcome", "drifted")
}

func TestParseAppliedKubernetesResourceRequiresAppliedState(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "exports/prod/grafana-dashboard.yaml", `
apiVersion: grafana.integreatly.org/v1beta1
kind: GrafanaDashboard
metadata:
  name: checkout-overview
  namespace: observability
  resourceVersion: "987654"
  generation: 7
  uid: 2dbcc422-547f-44fb-a90f-9c6d75abbb5f
status:
  conditions:
    - type: Ready
      status: "True"
      reason: Reconciled
spec:
  instanceSelector:
    matchLabels:
      dashboards: grafana
  json: '{"title":"Sensitive Dashboard","panels":[]}'
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	resources := yamlBucketForTest(t, got, "observability_applied_resources")
	if got, want := len(resources), 1; got != want {
		t.Fatalf("len(observability_applied_resources) = %d, want %d: %#v", got, want, resources)
	}
	assertYAMLField(t, resources[0], "source_kind", "kubernetes")
	assertYAMLField(t, resources[0], "source_class", "applied")
	assertYAMLField(t, resources[0], "resource_kind", "GrafanaDashboard")
	assertYAMLField(t, resources[0], "resource_generation", "7")
	assertYAMLField(t, resources[0], "observability_resource_class", "dashboard")
	assertYAMLField(t, resources[0], "outcome", "exact")
	assertYAMLForbiddenValuesAbsent(t, resources[0], "Sensitive Dashboard", "panels")
	if declared := yamlBucketForTest(t, got, "observability_declared_dashboards"); len(declared) != 0 {
		t.Fatalf("observability_declared_dashboards = %#v, want empty for applied-state exports", declared)
	}

	declaredOnly := writeYAMLTestFile(t, "declared/grafana-dashboard.yaml", `
apiVersion: grafana.integreatly.org/v1beta1
kind: GrafanaDashboard
metadata:
  name: checkout-overview
  namespace: observability
spec:
  json: '{"title":"Checkout","panels":[]}'
`)
	got, err = Parse(declaredOnly, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if resources := yamlBucketForTest(t, got, "observability_applied_resources"); len(resources) != 0 {
		t.Fatalf("observability_applied_resources = %#v, want empty for declared-only manifests", resources)
	}
}

func TestParseAppliedStateOutcomesStayDistinct(t *testing.T) {
	t.Parallel()

	filePath := writeYAMLTestFile(t, "clusters/prod/argocd/observability-failures.yaml", `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: observability
  namespace: argocd
spec:
  destination:
    name: prod
  source:
    repoURL: https://github.com/example/iac-observability
    path: environments/prod
status:
  sync:
    status: OutOfSync
  health:
    status: Degraded
  operationState:
    phase: Failed
  conditions:
    - type: ComparisonError
      message: "permission denied listing servicemonitors"
  resources:
    - group: monitoring.coreos.com
      kind: PrometheusRule
      namespace: observability
      name: checkout-rules
      status: Pruned
    - group: apps
      kind: Deployment
      namespace: observability
      name: tempo
      status: Missing
    - group: apps
      kind: Deployment
      namespace: observability
      name: loki
      status: Synced
      health:
        status: Stale
`)

	got, err := Parse(filePath, false, Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	syncStates := yamlBucketForTest(t, got, "observability_applied_sync_states")
	if got, want := len(syncStates), 1; got != want {
		t.Fatalf("len(observability_applied_sync_states) = %d, want %d: %#v", got, want, syncStates)
	}
	assertYAMLField(t, syncStates[0], "outcome", "permission_hidden")

	resources := yamlBucketForTest(t, got, "observability_applied_resources")
	if got, want := len(resources), 3; got != want {
		t.Fatalf("len(observability_applied_resources) = %d, want %d: %#v", got, want, resources)
	}
	assertYAMLField(t, yamlResourceRowForTest(t, resources, "loki"), "outcome", "stale")
	assertYAMLField(t, yamlResourceRowForTest(t, resources, "checkout-rules"), "outcome", "pruned")
	assertYAMLField(t, yamlResourceRowForTest(t, resources, "tempo"), "outcome", "missing")
}

func yamlResourceRowForTest(t *testing.T, rows []map[string]any, name string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["resource_name"] == name {
			return row
		}
	}
	t.Fatalf("missing resource_name %q in %#v", name, rows)
	return nil
}
