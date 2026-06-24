// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelmSecurityAlertCollectorDeployment(t *testing.T) {
	t.Parallel()

	defaultManifests := renderHelmChart(t)
	if helmManifestExists(defaultManifests, "Deployment", "eshu-security-alert-collector") {
		t.Fatal("default chart render included security-alert collector deployment")
	}

	valuesPath := filepath.Join(t.TempDir(), "security-alert-values.yaml")
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
    - instance_id: security-alert-primary
      collector_kind: security_alert
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: github_dependabot
            scope_id: security-alert:github:example-org/example-repo
            repository: example-org/example-repo
            token_env: GITHUB_TOKEN
            allowed_repositories: [example-org/example-repo]
            repository_alert_limit: 25
            max_pages: 2
securityAlertCollector:
  enabled: true
  instanceId: security-alert-primary
  pollInterval: 5s
  claimLeaseTTL: 90s
  heartbeatInterval: 20s
  collectorInstances:
    - instance_id: security-alert-primary
      collector_kind: security_alert
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: github_dependabot
            scope_id: security-alert:github:example-org/example-repo
            repository: example-org/example-repo
            token_env: GITHUB_TOKEN
            allowed_repositories: [example-org/example-repo]
            repository_alert_limit: 25
            max_pages: 2
  extraEnv:
    - name: GITHUB_TOKEN
      valueFrom:
        secretKeyRef:
          name: security-alert-credentials
          key: github-token
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write security-alert collector values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-security-alert-collector")
	container := requireHelmContainer(t, deployment, "security-alert-collector")
	if command := helmStringSlice(container["command"]); !stringSlicesEqual(command, []string{"/usr/local/bin/eshu-collector-security-alerts"}) {
		t.Fatalf("security-alert collector command = %#v", command)
	}

	env := helmEnvByName(container)
	assertHelmLiteralEnv(t, env, "ESHU_SECURITY_ALERT_COLLECTOR_INSTANCE_ID", "security-alert-primary")
	assertHelmLiteralEnv(t, env, "ESHU_SECURITY_ALERT_POLL_INTERVAL", "5s")
	assertHelmLiteralEnv(t, env, "ESHU_SECURITY_ALERT_CLAIM_LEASE_TTL", "90s")
	assertHelmLiteralEnv(t, env, "ESHU_SECURITY_ALERT_HEARTBEAT_INTERVAL", "20s")
	assertHelmValueFromEnv(t, manifests, "security-alert-collector", "GITHUB_TOKEN")
	instances := helmString(env["ESHU_COLLECTOR_INSTANCES_JSON"]["value"])
	for _, want := range []string{
		`"collector_kind":"security_alert"`,
		`"provider":"github_dependabot"`,
		`"token_env":"GITHUB_TOKEN"`,
		`"allowed_repositories":["example-org/example-repo"]`,
	} {
		if !strings.Contains(instances, want) {
			t.Fatalf("ESHU_COLLECTOR_INSTANCES_JSON = %q, missing %s", instances, want)
		}
	}
	requireHelmManifest(t, manifests, "Service", "eshu-security-alert-collector-metrics")
	requireHelmManifest(t, manifests, "ServiceMonitor", "eshu-security-alert-collector-metrics")
	requireHelmManifest(t, manifests, "NetworkPolicy", "eshu-security-alert-collector")
	requireHelmManifest(t, manifests, "PodDisruptionBudget", "eshu-security-alert-collector")
}
