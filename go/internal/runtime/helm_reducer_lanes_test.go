// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHelmResolutionEngineCanRenderDomainSpecificLanes(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "reducer-lanes.yaml")
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
resolutionEngine:
  enabled: true
  replicas: 1
  env:
    ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED: "true"
    ESHU_REDUCER_WORKERS: "3"
  lanes:
    - name: code-graph
      domains:
        - sql_relationship_materialization
        - inheritance_materialization
      replicas: 3
      env:
        ESHU_REDUCER_WORKERS: "5"
      resources:
        requests:
          cpu: 750m
          memory: 1Gi
    - name: cloud-drift
      domains:
        - aws_cloud_runtime_drift
      replicas: 2
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write reducer lane values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	if helmManifestExists(manifests, "Deployment", "eshu-resolution-engine") {
		t.Fatal("lane render included the undifferentiated resolution-engine deployment")
	}

	codeDeployment := requireHelmManifest(t, manifests, "Deployment", "eshu-resolution-engine-code-graph")
	codeSpec := helmMap(codeDeployment["spec"])
	if got, want := codeSpec["replicas"], 3; got != want {
		t.Fatalf("code lane replicas = %#v, want %d", got, want)
	}
	codeContainer := requireHelmContainer(t, codeDeployment, "resolution-engine")
	codeEnv := helmEnvByName(codeContainer)
	assertHelmLiteralEnv(
		t,
		codeEnv,
		"ESHU_REDUCER_CLAIM_DOMAINS",
		"sql_relationship_materialization,inheritance_materialization",
	)
	assertHelmLiteralEnv(t, codeEnv, "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED", "true")
	assertHelmLiteralEnv(t, codeEnv, "ESHU_REDUCER_WORKERS", "5")
	codeResources := helmMap(codeContainer["resources"])
	codeRequests := helmMap(codeResources["requests"])
	if got, want := helmString(codeRequests["cpu"]), "750m"; got != want {
		t.Fatalf("code lane cpu request = %q, want %q", got, want)
	}

	cloudDeployment := requireHelmManifest(t, manifests, "Deployment", "eshu-resolution-engine-cloud-drift")
	cloudSpec := helmMap(cloudDeployment["spec"])
	if got, want := cloudSpec["replicas"], 2; got != want {
		t.Fatalf("cloud lane replicas = %#v, want %d", got, want)
	}
	cloudContainer := requireHelmContainer(t, cloudDeployment, "resolution-engine")
	cloudEnv := helmEnvByName(cloudContainer)
	assertHelmLiteralEnv(t, cloudEnv, "ESHU_REDUCER_CLAIM_DOMAINS", "aws_cloud_runtime_drift")
	assertHelmLiteralEnv(t, cloudEnv, "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED", "true")
	assertHelmLiteralEnv(t, cloudEnv, "ESHU_REDUCER_WORKERS", "3")

	requireHelmManifest(t, manifests, "Service", "eshu-resolution-engine-code-graph-metrics")
	requireHelmManifest(t, manifests, "Service", "eshu-resolution-engine-cloud-drift-metrics")
	requireHelmManifest(t, manifests, "ServiceMonitor", "eshu-resolution-engine-metrics")
}

func TestHelmResolutionEngineRendersCodeCallProjectionConcurrency(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(t)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-resolution-engine")
	container := requireHelmContainer(t, deployment, "resolution-engine")
	env := helmEnvByName(container)

	assertHelmLiteralEnv(t, env, "ESHU_SHARED_PROJECTION_PARTITION_COUNT", "8")
	assertHelmLiteralEnv(t, env, "ESHU_SHARED_PROJECTION_WORKERS", "8")
	assertHelmLiteralEnv(t, env, "ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT", "8")
	assertHelmLiteralEnv(t, env, "ESHU_CODE_CALL_PROJECTION_WORKERS", "4")
	assertHelmLiteralEnv(t, env, "ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS", "1")
}
