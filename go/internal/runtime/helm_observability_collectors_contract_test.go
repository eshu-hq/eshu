// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelmObservabilityCollectorDeployments(t *testing.T) {
	t.Parallel()

	defaultManifests := renderHelmChart(t)
	for _, name := range []string{
		"eshu-grafana-collector",
		"eshu-prometheus-mimir-collector",
		"eshu-loki-collector",
		"eshu-tempo-collector",
	} {
		if helmManifestExists(defaultManifests, "Deployment", name) {
			t.Fatalf("default chart render included %s deployment", name)
		}
	}

	valuesPath := filepath.Join(t.TempDir(), "observability-collector-values.yaml")
	values := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
neo4j:
  auth:
    secretName: ""
observability:
  prometheus:
    enabled: true
    serviceMonitor:
      enabled: true
workflowCoordinator:
  enabled: true
  deploymentMode: active
  claimsEnabled: true
  collectorInstances:
    - instance_id: grafana-primary
      collector_kind: grafana
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: grafana
            scope_id: grafana:prod
            instance_id: prod
            base_url: https://grafana.example.test
            token_env: GRAFANA_TOKEN
            resource_limit: 25
            enabled: true
    - instance_id: metrics-primary
      collector_kind: prometheus_mimir
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: mimir
            scope_id: mimir:prod
            instance_id: prod
            base_url: https://mimir.example.test
            token_env: MIMIR_TOKEN
            tenant_id_env: MIMIR_TENANT
            resource_limit: 50
            enabled: true
    - instance_id: loki-primary
      collector_kind: loki
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - scope_id: loki:prod
            instance_id: prod
            base_url: https://loki.example.test
            token_env: LOKI_TOKEN
            tenant_id_env: LOKI_TENANT
            label_value_names: [app]
            enabled: true
    - instance_id: tempo-primary
      collector_kind: tempo
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - scope_id: tempo:prod
            instance_id: prod
            base_url: https://tempo.example.test
            token_env: TEMPO_TOKEN
            tenant_id_env: TEMPO_TENANT
            tag_value_names: [resource.service.name]
            freshness_probe_enabled: true
            enabled: true
grafanaCollector:
  enabled: true
  instanceId: grafana-primary
  pollInterval: 5s
  claimLeaseTTL: 1m
  heartbeatInterval: 20s
  collectorInstances:
    - instance_id: grafana-primary
      collector_kind: grafana
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: grafana
            scope_id: grafana:prod
            instance_id: prod
            base_url: https://grafana.example.test
            token_env: GRAFANA_TOKEN
            resource_limit: 25
            enabled: true
  extraEnv:
    - name: GRAFANA_TOKEN
      valueFrom:
        secretKeyRef:
          name: observability-credentials
          key: grafana-token
prometheusMimirCollector:
  enabled: true
  instanceId: metrics-primary
  pollInterval: 6s
  claimLeaseTTL: 90s
  heartbeatInterval: 30s
  collectorInstances:
    - instance_id: metrics-primary
      collector_kind: prometheus_mimir
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: mimir
            scope_id: mimir:prod
            instance_id: prod
            base_url: https://mimir.example.test
            token_env: MIMIR_TOKEN
            tenant_id_env: MIMIR_TENANT
            resource_limit: 50
            enabled: true
  extraEnv:
    - name: MIMIR_TOKEN
      valueFrom:
        secretKeyRef:
          name: observability-credentials
          key: mimir-token
    - name: MIMIR_TENANT
      valueFrom:
        secretKeyRef:
          name: observability-credentials
          key: mimir-tenant
componentExtensionCollector:
  allowIds: dev.eshu.collector.loki
lokiCollector:
  enabled: true
  instanceId: loki-primary
  pollInterval: 7s
  claimLeaseTTL: 2m
  heartbeatInterval: 40s
  componentId: dev.eshu.collector.loki
  componentVersion: "0.1.2"
  seed:
    image: ghcr.io/eshu-hq/eshu-collector-loki@sha256:12eac89e7abd2988529c6f2854ba63293b5507502630764c3f6b8ffc7e3bf84d
  host:
    sourceSystem: loki
    scope:
      id: "loki:prod"
      kind: log_source
  collectorInstances:
    - instance_id: loki-primary
      collector_kind: loki
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - scope_id: loki:prod
            instance_id: prod
            base_url: https://loki.example.test
            token_env: LOKI_TOKEN
            tenant_id_env: LOKI_TENANT
            label_value_names: [app]
            enabled: true
  extraEnv:
    - name: LOKI_TOKEN
      valueFrom:
        secretKeyRef:
          name: observability-credentials
          key: loki-token
    - name: LOKI_TENANT
      valueFrom:
        secretKeyRef:
          name: observability-credentials
          key: loki-tenant
componentExtensionCollector:
  allowIds: dev.eshu.collector.tempo
tempoCollector:
  enabled: true
  instanceId: tempo-primary
  pollInterval: 8s
  claimLeaseTTL: 3m
  heartbeatInterval: 50s
  componentId: dev.eshu.collector.tempo
  componentVersion: "0.1.3"
  seed:
    image: ghcr.io/eshu-hq/eshu-collector-tempo@sha256:0230e174284bb7a9831babac9b9927be05df02e8620c20e448e91dbb1ac2ecac
  host:
    sourceSystem: tempo
    scope:
      id: "tempo:prod"
      kind: trace_source
  collectorInstances:
    - instance_id: tempo-primary
      collector_kind: tempo
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - scope_id: tempo:prod
            instance_id: prod
            base_url: https://tempo.example.test
            token_env: TEMPO_TOKEN
            tenant_id_env: TEMPO_TENANT
            tag_value_names: [resource.service.name]
            freshness_probe_enabled: true
            enabled: true
  extraEnv:
    - name: TEMPO_TOKEN
      valueFrom:
        secretKeyRef:
          name: observability-credentials
          key: tempo-token
    - name: TEMPO_TENANT
      valueFrom:
        secretKeyRef:
          name: observability-credentials
          key: tempo-tenant
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write observability collector values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	assertClaimCollector(t, manifests, "grafana-collector", "eshu-collector-grafana", map[string]string{
		"ESHU_GRAFANA_COLLECTOR_INSTANCE_ID":        "grafana-primary",
		"ESHU_GRAFANA_COLLECTOR_POLL_INTERVAL":      "5s",
		"ESHU_GRAFANA_COLLECTOR_CLAIM_LEASE_TTL":    "1m",
		"ESHU_GRAFANA_COLLECTOR_HEARTBEAT_INTERVAL": "20s",
	})
	assertClaimCollector(t, manifests, "prometheus-mimir-collector", "eshu-collector-prometheus-mimir", map[string]string{
		"ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID":        "metrics-primary",
		"ESHU_PROMETHEUS_MIMIR_COLLECTOR_POLL_INTERVAL":      "6s",
		"ESHU_PROMETHEUS_MIMIR_COLLECTOR_CLAIM_LEASE_TTL":    "90s",
		"ESHU_PROMETHEUS_MIMIR_COLLECTOR_HEARTBEAT_INTERVAL": "30s",
	})
	assertClaimCollector(t, manifests, "loki-collector", "eshu-collector-component-extension", map[string]string{
		"ESHU_LOKI_COLLECTOR_INSTANCE_ID":        "loki-primary",
		"ESHU_LOKI_COLLECTOR_POLL_INTERVAL":      "7s",
		"ESHU_LOKI_COLLECTOR_CLAIM_LEASE_TTL":    "2m",
		"ESHU_LOKI_COLLECTOR_HEARTBEAT_INTERVAL": "40s",
	})

	// The reworked loki lane (#9202) runs the component-extension worker on
	// the Eshu host image: the seeded satellite binary arrives via the init
	// seed container, so the Deployment carries the extension worker identity
	// env alongside the claim timing above.
	lokiEnv := helmEnvByName(requireHelmContainer(t, requireHelmManifest(t, manifests, "Deployment", "eshu-loki-collector"), "loki-collector"))
	assertHelmLiteralEnv(t, lokiEnv, "ESHU_COMPONENT_COLLECTOR_INSTANCE_ID", "loki-primary")
	assertHelmLiteralEnv(t, lokiEnv, "ESHU_COMPONENT_COLLECTOR_SCOPE_KIND", "log_source")
	assertHelmLiteralEnv(t, lokiEnv, "ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL", "7s")
	assertClaimCollector(t, manifests, "tempo-collector", "eshu-collector-component-extension", map[string]string{
		"ESHU_TEMPO_COLLECTOR_INSTANCE_ID":        "tempo-primary",
		"ESHU_TEMPO_COLLECTOR_POLL_INTERVAL":      "8s",
		"ESHU_TEMPO_COLLECTOR_CLAIM_LEASE_TTL":    "3m",
		"ESHU_TEMPO_COLLECTOR_HEARTBEAT_INTERVAL": "50s",
	})

	// The reworked tempo lane (#9172) runs the component-extension worker on
	// the Eshu host image: the seeded satellite binary arrives via the init
	// seed container, so the Deployment carries the extension worker identity
	// env alongside the claim timing above.
	tempoEnv := helmEnvByName(requireHelmContainer(t, requireHelmManifest(t, manifests, "Deployment", "eshu-tempo-collector"), "tempo-collector"))
	assertHelmLiteralEnv(t, tempoEnv, "ESHU_COMPONENT_COLLECTOR_INSTANCE_ID", "tempo-primary")
	assertHelmLiteralEnv(t, tempoEnv, "ESHU_COMPONENT_COLLECTOR_SCOPE_KIND", "trace_source")
	assertHelmLiteralEnv(t, tempoEnv, "ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL", "8s")

	assertHelmValueFromEnv(t, manifests, "grafana-collector", "GRAFANA_TOKEN")
	assertHelmValueFromEnv(t, manifests, "prometheus-mimir-collector", "MIMIR_TOKEN")
	assertHelmValueFromEnv(t, manifests, "prometheus-mimir-collector", "MIMIR_TENANT")
	assertHelmValueFromEnv(t, manifests, "loki-collector", "LOKI_TOKEN")
	assertHelmValueFromEnv(t, manifests, "loki-collector", "LOKI_TENANT")
	assertHelmValueFromEnv(t, manifests, "tempo-collector", "TEMPO_TOKEN")
	assertHelmValueFromEnv(t, manifests, "tempo-collector", "TEMPO_TENANT")

	apiContainer := requireHelmContainer(t, requireHelmManifest(t, manifests, "Deployment", "eshu-api"), "eshu")
	apiEnv := helmEnvByName(apiContainer)
	assertHelmLiteralEnv(t, apiEnv, "ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID", "metrics-primary")
	if value := helmString(apiEnv["ESHU_COLLECTOR_INSTANCES_JSON"]["value"]); !strings.Contains(value, `"collector_kind":"prometheus_mimir"`) {
		t.Fatalf("api ESHU_COLLECTOR_INSTANCES_JSON = %q, want prometheus_mimir collector config", value)
	}
	for _, name := range []string{"MIMIR_TOKEN", "MIMIR_TENANT"} {
		if _, ok := apiEnv[name]["valueFrom"]; !ok {
			t.Fatalf("api env %s = %#v, want valueFrom for metrics time-series source", name, apiEnv[name])
		}
	}
}

// TestHelmLokiCollectorRequiresDigestPinnedSeedImage proves the loki seed
// image must be pinned by digest alone (repository@sha256:<64 hex>): a tag
// would ride along in --image-ref and seed an unpinned collector, so the
// chart schema rejects it at render time. validate.yaml carries no loki pin
// rule (line-cap pressure from the tempo lane, #9172): this schema rejection
// is the single enforcement point.
func TestHelmLokiCollectorRequiresDigestPinnedSeedImage(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "loki-tag-seed-values.yaml")
	values := []byte(`
lokiCollector:
  enabled: true
  instanceId: loki-primary
  componentId: dev.eshu.collector.loki
  componentVersion: "0.1.2"
  seed:
    image: ghcr.io/eshu-hq/eshu-collector-loki:v0.1.2
  host:
    sourceSystem: loki
    scope:
      id: "loki:primary"
      kind: log_source
  collectorInstances:
    - instance_id: loki-primary
      collector_kind: loki
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - scope_id: loki:primary
            instance_id: primary
            base_url: https://loki.example.test
            enabled: true
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write loki tag-seed values: %v", err)
	}

	output := renderHelmChartFailure(t, "-f", valuesPath)
	if !strings.Contains(output, "/lokiCollector/seed/image") {
		t.Fatalf("helm template error = %q, want lokiCollector seed image digest rejection", output)
	}
}
