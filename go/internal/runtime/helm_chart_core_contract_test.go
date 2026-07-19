// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelmNornicDBNoSecretPathKeepsBoltCredentials(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(t, "--set-string", "neo4j.auth.secretName=")
	for _, manifest := range manifests {
		kind, _ := manifest["kind"].(string)
		if kind != "Deployment" && kind != "StatefulSet" {
			continue
		}

		podSpec := helmPodSpec(t, manifest)
		for _, containerGroup := range []string{"initContainers", "containers"} {
			for _, container := range helmMapSlice(podSpec[containerGroup]) {
				env := helmEnvByName(container)
				if _, ok := env["NEO4J_URI"]; !ok {
					continue
				}
				assertHelmLiteralEnv(t, env, "NEO4J_USERNAME", "neo4j")
				assertHelmLiteralEnv(t, env, "NEO4J_PASSWORD", defaultHelmNeo4jPassword)
			}
		}
	}
}

func TestHelmMCPDeploymentStartsHTTPTransport(t *testing.T) {
	t.Parallel()

	deployment := requireHelmManifest(t, renderHelmChart(t), "Deployment", "eshu-mcp-server")
	container := requireHelmContainer(t, deployment, "mcp-server")

	command := helmStringSlice(container["command"])
	want := []string{"eshu", "mcp", "start", "--transport", "http"}
	if !stringSlicesEqual(command, want) {
		t.Fatalf("mcp command = %#v, want %#v", command, want)
	}
}

// TestHelmMCPDeploymentDisablesUnauthEscapeHatch is the issue #5168 review P1
// regression: the mcp-server container runs `eshu mcp start --transport http`,
// the same CLI subcommand that defaults ESHU_MCP_ALLOW_UNAUTHENTICATED on for a
// loopback bind. The chart must pin that env to "false" so a Helm pod can never
// silently inherit the dev/loopback escape hatch and defeat the no-silent-open
// startup gate on a publicly reachable Service.
func TestHelmMCPDeploymentDisablesUnauthEscapeHatch(t *testing.T) {
	t.Parallel()

	deployment := requireHelmManifest(t, renderHelmChart(t), "Deployment", "eshu-mcp-server")
	container := requireHelmContainer(t, deployment, "mcp-server")
	env := helmEnvByName(container)
	assertHelmLiteralEnv(t, env, "ESHU_MCP_ALLOW_UNAUTHENTICATED", "false")
}

func TestHelmHTTPRuntimesScrapeMetricsOnHTTPPort(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(t, "--set", "observability.prometheus.enabled=true")
	for _, serviceName := range []string{"eshu-api-metrics", "eshu-mcp-server-metrics"} {
		service := requireHelmManifest(t, manifests, "Service", serviceName)
		ports := helmMapSlice(helmMap(service["spec"])["ports"])
		if len(ports) != 1 {
			t.Fatalf("%s ports = %d, want 1", serviceName, len(ports))
		}
		if got, want := ports[0]["targetPort"], "http"; got != want {
			t.Fatalf("%s targetPort = %#v, want %q", serviceName, got, want)
		}
	}
}

func TestHelmOCIRegistryCollectorDeployment(t *testing.T) {
	t.Parallel()

	defaultManifests := renderHelmChart(t)
	if helmManifestExists(defaultManifests, "Deployment", "eshu-oci-registry-collector") {
		t.Fatal("default chart render included OCI registry collector deployment")
	}

	valuesPath := filepath.Join(t.TempDir(), "oci-values.yaml")
	values := []byte(`
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-oci-registry-collector
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
ociRegistryCollector:
  enabled: true
  instanceId: oci-registry-primary
  pollInterval: 10m
  aws:
    region: us-east-1
  targets:
    - provider: ecr
      registry_id: "123456789012"
      region: us-east-1
      repository: team/api
      references: ["latest"]
    - provider: dockerhub
      repository: library/busybox
      references: ["latest"]
  extraEnv:
    - name: JFROG_USERNAME
      valueFrom:
        secretKeyRef:
          name: jfrog-oci-credentials
          key: username
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write OCI registry collector values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-oci-registry-collector")
	container := requireHelmContainer(t, deployment, "oci-registry-collector")
	command := helmStringSlice(container["command"])
	if !stringSlicesEqual(command, []string{"/usr/local/bin/eshu-collector-oci-registry"}) {
		t.Fatalf("oci registry collector command = %#v", command)
	}

	env := helmEnvByName(container)
	assertHelmLiteralEnv(t, env, "ESHU_OCI_REGISTRY_COLLECTOR_INSTANCE_ID", "oci-registry-primary")
	assertHelmLiteralEnv(t, env, "ESHU_OCI_REGISTRY_POLL_INTERVAL", "10m")
	assertHelmLiteralEnv(t, env, "AWS_REGION", "us-east-1")
	assertHelmLiteralEnv(t, env, "AWS_DEFAULT_REGION", "us-east-1")
	targets := helmString(env["ESHU_OCI_REGISTRY_TARGETS_JSON"]["value"])
	for _, want := range []string{`"provider":"ecr"`, `"provider":"dockerhub"`} {
		if !strings.Contains(targets, want) {
			t.Fatalf("ESHU_OCI_REGISTRY_TARGETS_JSON = %q, missing %s", targets, want)
		}
	}
	if _, ok := env["JFROG_USERNAME"]["valueFrom"]; !ok {
		t.Fatalf("JFROG_USERNAME env = %#v, want valueFrom secret ref", env["JFROG_USERNAME"])
	}

	serviceAccount := requireHelmManifest(t, manifests, "ServiceAccount", "eshu")
	annotations := helmMap(helmMap(serviceAccount["metadata"])["annotations"])
	if got, want := helmString(annotations["eks.amazonaws.com/role-arn"]), "arn:aws:iam::123456789012:role/eshu-oci-registry-collector"; got != want {
		t.Fatalf("IRSA annotation = %q, want %q", got, want)
	}
	requireHelmManifest(t, manifests, "Service", "eshu-oci-registry-collector-metrics")
	requireHelmManifest(t, manifests, "ServiceMonitor", "eshu-oci-registry-collector-metrics")
}
