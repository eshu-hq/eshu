// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelmCICDRunCollectorDeployment(t *testing.T) {
	t.Parallel()

	defaultManifests := renderHelmChart(t)
	if helmManifestExists(defaultManifests, "Deployment", "eshu-cicd-run-collector") {
		t.Fatal("default chart render included CI/CD run collector deployment")
	}

	valuesPath := filepath.Join(t.TempDir(), "cicd-run-collector-values.yaml")
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
    - instance_id: cicd-run-primary
      collector_kind: ci_cd_run
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: github_actions
            scope_id: ci-cd:github-actions:example-org/example-repo
            repository: example-org/example-repo
            token_env: GITHUB_TOKEN
            allowed_repositories: [example-org/example-repo]
            max_runs: 1
            max_jobs: 25
            max_artifacts: 25
cicdRunCollector:
  enabled: true
  instanceId: cicd-run-primary
  pollInterval: 7s
  claimLeaseTTL: 90s
  heartbeatInterval: 20s
  collectorInstances:
    - instance_id: cicd-run-primary
      collector_kind: ci_cd_run
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: github_actions
            scope_id: ci-cd:github-actions:example-org/example-repo
            repository: example-org/example-repo
            token_env: GITHUB_TOKEN
            allowed_repositories: [example-org/example-repo]
            max_runs: 1
            max_jobs: 25
            max_artifacts: 25
  extraEnv:
    - name: GITHUB_TOKEN
      valueFrom:
        secretKeyRef:
          name: cicd-run-credentials
          key: github-token
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write CI/CD run collector values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	assertClaimCollector(t, manifests, "cicd-run-collector", "eshu-collector-cicd-run", map[string]string{
		"ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID": "cicd-run-primary",
		"ESHU_CICD_RUN_POLL_INTERVAL":         "7s",
		"ESHU_CICD_RUN_CLAIM_LEASE_TTL":       "90s",
		"ESHU_CICD_RUN_HEARTBEAT_INTERVAL":    "20s",
	})
	assertHelmValueFromEnv(t, manifests, "cicd-run-collector", "GITHUB_TOKEN")
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-cicd-run-collector")
	env := helmEnvByName(requireHelmContainer(t, deployment, "cicd-run-collector"))
	instances := helmString(env["ESHU_COLLECTOR_INSTANCES_JSON"]["value"])
	for _, want := range []string{
		`"collector_kind":"ci_cd_run"`,
		`"provider":"github_actions"`,
		`"token_env":"GITHUB_TOKEN"`,
		`"max_artifacts":25`,
	} {
		if !strings.Contains(instances, want) {
			t.Fatalf("ESHU_COLLECTOR_INSTANCES_JSON = %q, missing %s", instances, want)
		}
	}
}
