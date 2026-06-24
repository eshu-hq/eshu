// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHelmComponentExtensionCollectorDeployment(t *testing.T) {
	t.Parallel()

	defaultManifests := renderHelmChart(t)
	if helmManifestExists(defaultManifests, "Deployment", "eshu-component-extension-collector") {
		t.Fatal("default chart render included eshu-component-extension-collector deployment")
	}

	valuesPath := filepath.Join(t.TempDir(), "component-extension-values.yaml")
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
  extraVolumes:
    - name: component-registry
      persistentVolumeClaim:
        claimName: eshu-component-registry
  extraVolumeMounts:
    - name: component-registry
      mountPath: /data/.eshu/components
componentExtensionCollector:
  enabled: true
  instanceId: pagerduty-reference
  componentHome: /data/.eshu/components
  trustMode: allowlist
  allowIds: dev.eshu.examples.pagerduty
  allowPublishers: eshu-hq
  extensionEgressPolicyJSON: '{"mode":"restricted","extensions":[{"component_id":"dev.eshu.examples.pagerduty","decision":"allow"}]}'
  scopeKind: component
  pollInterval: 13s
  claimLeaseTTL: 90s
  heartbeatInterval: 35s
  extraVolumes:
    - name: component-registry
      persistentVolumeClaim:
        claimName: eshu-component-registry
  extraVolumeMounts:
    - name: component-registry
      mountPath: /data/.eshu/components
  extraEnv:
    - name: PAGERDUTY_API_TOKEN
      valueFrom:
        secretKeyRef:
          name: pagerduty-reference-credentials
          key: api-token
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write component extension values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	coordinatorContainer := requireHelmContainer(t, requireHelmManifest(t, manifests, "Deployment", "eshu-workflow-coordinator"), "workflow-coordinator")
	coordinatorEnv := helmEnvByName(coordinatorContainer)
	assertHelmLiteralEnv(t, coordinatorEnv, "ESHU_COMPONENT_HOME", "/data/.eshu/components")
	assertHelmLiteralEnv(t, coordinatorEnv, "ESHU_COMPONENT_TRUST_MODE", "allowlist")
	assertHelmLiteralEnv(t, coordinatorEnv, "ESHU_COMPONENT_ALLOW_IDS", "dev.eshu.examples.pagerduty")
	assertHelmLiteralEnv(t, coordinatorEnv, "ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON", `{"mode":"restricted","extensions":[{"component_id":"dev.eshu.examples.pagerduty","decision":"allow"}]}`)
	assertComponentExtensionCollector(t, manifests, map[string]string{
		"ESHU_COMPONENT_HOME":                         "/data/.eshu/components",
		"ESHU_COMPONENT_TRUST_MODE":                   "allowlist",
		"ESHU_COMPONENT_ALLOW_IDS":                    "dev.eshu.examples.pagerduty",
		"ESHU_COMPONENT_ALLOW_PUBLISHERS":             "eshu-hq",
		"ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON":    `{"mode":"restricted","extensions":[{"component_id":"dev.eshu.examples.pagerduty","decision":"allow"}]}`,
		"ESHU_COMPONENT_COLLECTOR_INSTANCE_ID":        "pagerduty-reference",
		"ESHU_COMPONENT_COLLECTOR_SCOPE_KIND":         "component",
		"ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL":      "13s",
		"ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL":    "90s",
		"ESHU_COMPONENT_COLLECTOR_HEARTBEAT_INTERVAL": "35s",
	})
	assertHelmValueFromEnv(t, manifests, "component-extension-collector", "PAGERDUTY_API_TOKEN")
}

func TestHelmComponentExtensionCollectorValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		values     string
		wantOutput string
	}{
		{
			name:       "strict trust mode",
			values:     componentExtensionCollectorValues("strict", true, true),
			wantOutput: "value must be one of 'disabled', 'allowlist'",
		},
		{
			name:       "coordinator registry mount",
			values:     componentExtensionCollectorValues("allowlist", false, true),
			wantOutput: "workflowCoordinator.extraVolumeMounts must mount componentExtensionCollector.componentHome",
		},
		{
			name:       "collector registry mount",
			values:     componentExtensionCollectorValues("allowlist", true, false),
			wantOutput: "componentExtensionCollector.extraVolumeMounts must mount componentExtensionCollector.componentHome",
		},
		{
			name:       "empty allow ids",
			values:     componentExtensionCollectorValuesWithAllowlist("allowlist", true, true, "", "eshu-hq"),
			wantOutput: "componentExtensionCollector.allowIds is required when componentExtensionCollector.enabled=true",
		},
		{
			name:       "empty allow publishers",
			values:     componentExtensionCollectorValuesWithAllowlist("allowlist", true, true, "dev.eshu.examples.pagerduty", ""),
			wantOutput: "componentExtensionCollector.allowPublishers is required when componentExtensionCollector.enabled=true",
		},
		{
			name:       "separator only allow ids",
			values:     componentExtensionCollectorValuesWithAllowlist("allowlist", true, true, " , , ", "eshu-hq"),
			wantOutput: "componentExtensionCollector.allowIds must contain at least one non-empty CSV token when componentExtensionCollector.enabled=true",
		},
		{
			name:       "separator only allow publishers",
			values:     componentExtensionCollectorValuesWithAllowlist("allowlist", true, true, "dev.eshu.examples.pagerduty", ","),
			wantOutput: "componentExtensionCollector.allowPublishers must contain at least one non-empty CSV token when componentExtensionCollector.enabled=true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			valuesPath := filepath.Join(t.TempDir(), "component-extension-values.yaml")
			if err := os.WriteFile(valuesPath, []byte(tt.values), 0o600); err != nil {
				t.Fatalf("write component extension values: %v", err)
			}

			output := renderHelmChartFailure(t, "-f", valuesPath)
			if !strings.Contains(output, tt.wantOutput) {
				t.Fatalf("helm template error = %q, want %q", output, tt.wantOutput)
			}
		})
	}
}

func TestHelmComponentExtensionCollectorHonorsNetworkPolicyToggle(t *testing.T) {
	t.Parallel()

	values := componentExtensionCollectorValues("allowlist", true, true) + `
networkPolicy:
  enabled: false
`
	valuesPath := filepath.Join(t.TempDir(), "component-extension-values.yaml")
	if err := os.WriteFile(valuesPath, []byte(values), 0o600); err != nil {
		t.Fatalf("write component extension values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	requireHelmManifest(t, manifests, "Deployment", "eshu-component-extension-collector")
	if helmManifestExists(manifests, "NetworkPolicy", "eshu-component-extension-collector") {
		t.Fatal("component extension collector rendered NetworkPolicy with networkPolicy.enabled=false")
	}
}

func assertComponentExtensionCollector(t *testing.T, manifests []helmManifest, wantEnv map[string]string) {
	t.Helper()

	component := "component-extension-collector"
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-"+component)
	container := requireHelmContainer(t, deployment, component)
	if command := helmStringSlice(container["command"]); !stringSlicesEqual(command, []string{"/usr/local/bin/eshu-collector-component-extension"}) {
		t.Fatalf("%s command = %#v, want component extension binary", component, command)
	}
	env := helmEnvByName(container)
	for name, want := range wantEnv {
		assertHelmLiteralEnv(t, env, name, want)
	}
	if _, ok := env["ESHU_COLLECTOR_INSTANCES_JSON"]; ok {
		t.Fatalf("%s should read component registry state, not ESHU_COLLECTOR_INSTANCES_JSON", component)
	}
	requireHelmManifest(t, manifests, "Service", "eshu-"+component+"-metrics")
	requireHelmManifest(t, manifests, "ServiceMonitor", "eshu-"+component+"-metrics")
	requireHelmManifest(t, manifests, "NetworkPolicy", "eshu-"+component)
	requireHelmManifest(t, manifests, "PodDisruptionBudget", "eshu-"+component)
}

func componentExtensionCollectorValues(trustMode string, mountCoordinator bool, mountCollector bool) string {
	return componentExtensionCollectorValuesWithAllowlist(
		trustMode,
		mountCoordinator,
		mountCollector,
		"dev.eshu.examples.pagerduty",
		"eshu-hq",
	)
}

func componentExtensionCollectorValuesWithAllowlist(trustMode string, mountCoordinator bool, mountCollector bool, allowIDs string, allowPublishers string) string {
	coordinatorMounts := "  extraVolumeMounts: []\n"
	if mountCoordinator {
		coordinatorMounts = `  extraVolumeMounts:
    - name: component-registry
      mountPath: /data/.eshu/components
`
	}
	collectorMounts := "  extraVolumeMounts: []\n"
	if mountCollector {
		collectorMounts = `  extraVolumeMounts:
    - name: component-registry
      mountPath: /data/.eshu/components
`
	}
	return `contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
neo4j:
  auth:
    secretName: ""
workflowCoordinator:
  enabled: true
  deploymentMode: active
  claimsEnabled: true
  extraVolumes:
    - name: component-registry
      persistentVolumeClaim:
        claimName: eshu-component-registry
` + coordinatorMounts + `componentExtensionCollector:
  enabled: true
  instanceId: pagerduty-reference
  componentHome: /data/.eshu/components
  trustMode: ` + trustMode + `
  allowIds: ` + strconv.Quote(allowIDs) + `
  allowPublishers: ` + strconv.Quote(allowPublishers) + `
  extensionEgressPolicyJSON: '{"mode":"restricted","extensions":[{"component_id":"dev.eshu.examples.pagerduty","decision":"allow"}]}'
  extraVolumes:
    - name: component-registry
      persistentVolumeClaim:
        claimName: eshu-component-registry
` + collectorMounts
}
