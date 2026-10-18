// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHelmEshuWorkloadsServingReadyzHaveReadinessProbe is the issue #8242
// regression: the ingester and the resolution engine serve the shared
// /readyz check (their admin mux always includes the status_schema ledger
// probe) but the chart gave them no readinessProbe, so Kubernetes reported
// them Ready while the migration ledger was behind the binary. Every Eshu
// workload container must gate pod readiness on /readyz, so a rollout with
// a pending migration holds writers not-Ready until the schema Job finishes.
//
// The test asserts the rule, not a list: any Deployment or StatefulSet
// container running an Eshu binary without a /readyz readinessProbe fails,
// including future workloads covered by these renders and both
// resolution-engine shapes (single Deployment and per-lane Deployments).
func TestHelmEshuWorkloadsServingReadyzHaveReadinessProbe(t *testing.T) {
	t.Parallel()

	t.Run("default", func(t *testing.T) {
		t.Parallel()

		manifests := renderHelmChart(t)
		// Pin the writers named in the issue so the rule scan cannot pass
		// vacuously if either workload stops rendering by default.
		requireHelmManifest(t, manifests, "StatefulSet", "eshu")
		requireHelmManifest(t, manifests, "Deployment", "eshu-resolution-engine")
		assertEshuReadyzProbes(t, manifests)
	})

	t.Run("lanes", func(t *testing.T) {
		t.Parallel()

		valuesPath := filepath.Join(t.TempDir(), "reducer-lanes-readyz.yaml")
		values := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
neo4j:
  auth:
    secretName: ""
resolutionEngine:
  enabled: true
  lanes:
    - name: code-graph
      domains:
        - sql_relationship_materialization
    - name: cloud-drift
      domains:
        - aws_cloud_runtime_drift
`)
		if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
			t.Fatalf("write reducer lane values: %v", err)
		}
		manifests := renderHelmChart(t, "-f", valuesPath)
		requireHelmManifest(t, manifests, "Deployment", "eshu-resolution-engine-code-graph")
		requireHelmManifest(t, manifests, "Deployment", "eshu-resolution-engine-cloud-drift")
		assertEshuReadyzProbes(t, manifests)
	})
}

// assertEshuReadyzProbes requires a /readyz readinessProbe on every
// Deployment and StatefulSet container that runs an Eshu binary. Containers
// that do not run one (bundled NornicDB, init containers, one-shot Jobs) are
// out of scope: NornicDB is not an Eshu runtime, init containers cannot carry
// probes, and Jobs never report pod readiness.
func assertEshuReadyzProbes(t *testing.T, manifests []helmManifest) {
	t.Helper()

	checked := 0
	for _, manifest := range manifests {
		kind, _ := manifest["kind"].(string)
		if kind != "Deployment" && kind != "StatefulSet" {
			continue
		}
		name := helmString(helmMap(manifest["metadata"])["name"])
		for _, container := range helmMapSlice(helmPodSpec(t, manifest)["containers"]) {
			command := helmStringSlice(container["command"])
			if len(command) == 0 || !isEshuBinaryCommand(command[0]) {
				continue
			}
			checked++
			containerName, _ := container["name"].(string)
			probe := helmMap(container["readinessProbe"])
			if probe == nil {
				t.Fatalf("%s/%s container %s runs %q with no readinessProbe, want /readyz",
					kind, name, containerName, command[0])
			}
			httpGet := helmMap(probe["httpGet"])
			if got, want := helmString(httpGet["path"]), "/readyz"; got != want {
				t.Fatalf("%s/%s container %s readinessProbe path = %q, want %q",
					kind, name, containerName, got, want)
			}
			if got, want := helmString(httpGet["port"]), "http"; got != want {
				t.Fatalf("%s/%s container %s readinessProbe port = %q, want %q",
					kind, name, containerName, got, want)
			}
			assertNamedContainerPort(t, kind, name, containerName, container, "http")
		}
	}
	if checked == 0 {
		t.Fatal("chart render included no Eshu workload containers")
	}
}

// isEshuBinaryCommand reports whether a container command runs an Eshu
// runtime binary: either a direct /usr/local/bin/eshu-* binary or the eshu
// CLI wrapper (api start, mcp start).
func isEshuBinaryCommand(command string) bool {
	return strings.HasPrefix(command, "/usr/local/bin/eshu") || command == "eshu"
}

// assertNamedContainerPort requires a declared container port with the given
// name: a probe that references an undeclared port name never becomes ready.
func assertNamedContainerPort(
	t *testing.T,
	kind, workload, containerName string,
	container map[string]any,
	want string,
) {
	t.Helper()

	for _, port := range helmMapSlice(container["ports"]) {
		if helmString(port["name"]) == want {
			return
		}
	}
	t.Fatalf("%s/%s container %s declares no %q port for its readinessProbe",
		kind, workload, containerName, want)
}
