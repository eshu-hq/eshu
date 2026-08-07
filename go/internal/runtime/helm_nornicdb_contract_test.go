// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelmDefaultsRenderWithSafeGraphBackend(t *testing.T) {
	t.Parallel()

	manifests := renderDefaultHelmChart(t)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-api")
	container := requireHelmContainer(t, deployment, "eshu")
	env := helmEnvByName(container)
	for key, want := range map[string]string{
		"ESHU_GRAPH_BACKEND": "neo4j",
		"DEFAULT_DATABASE":   "neo4j",
		"NEO4J_DATABASE":     "neo4j",
	} {
		assertHelmLiteralEnv(t, env, key, want)
	}
}

func TestHelmBundledNornicDBUsesGraphOnlySearchControls(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(
		t,
		"--set", "nornicdb.enabled=true",
		"--set", "nornicdb.capabilities.relationshipMergePropertyIdentity=true",
		"--set", "schemaBootstrap.useHelmHooks=false",
	)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-nornicdb")
	container := requireHelmContainer(t, deployment, "nornicdb")
	env := helmEnvByName(container)

	for key, want := range map[string]string{
		"NORNICDB_SEARCH_BM25_ENABLED":    "false",
		"NORNICDB_SEARCH_VECTOR_ENABLED":  "false",
		"NORNICDB_SEARCH_BM25_WARMING":    "lazy",
		"NORNICDB_SEARCH_VECTOR_WARMING":  "lazy",
		"NORNICDB_PERSIST_SEARCH_INDEXES": "false",
		"NORNICDB_ASYNC_WRITES_ENABLED":   "false",
		"NORNICDB_HEIMDALL_ENABLED":       "false",
		"NORNICDB_QDRANT_GRPC_ENABLED":    "false",
		"NORNICDB_EMBEDDING_ENABLED":      "false",
	} {
		assertHelmLiteralEnv(t, env, key, want)
	}
}

func TestHelmBundledNornicDBBindsServiceReachableAddress(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(
		t,
		"--set", "nornicdb.enabled=true",
		"--set", "nornicdb.capabilities.relationshipMergePropertyIdentity=true",
		"--set", "schemaBootstrap.useHelmHooks=false",
	)
	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-nornicdb")
	container := requireHelmContainer(t, deployment, "nornicdb")

	if _, ok := container["command"]; ok {
		t.Fatalf("nornicdb command is set, want image entrypoint preserved")
	}
	if _, ok := container["args"]; ok {
		t.Fatalf("nornicdb args are set, want image entrypoint arguments preserved")
	}
	env := helmEnvByName(container)
	assertHelmLiteralEnv(t, env, "NORNICDB_ADDRESS", "0.0.0.0")

	service := requireHelmManifest(t, manifests, "Service", "eshu-nornicdb")
	ports := helmMapSlice(helmMap(service["spec"])["ports"])
	if len(ports) != 2 {
		t.Fatalf("nornicdb service ports = %d, want 2", len(ports))
	}
	for _, port := range ports {
		switch port["name"] {
		case "http":
			if got, want := port["targetPort"], "http"; got != want {
				t.Fatalf("nornicdb HTTP targetPort = %#v, want %q", got, want)
			}
		case "bolt":
			if got, want := port["targetPort"], "bolt"; got != want {
				t.Fatalf("nornicdb Bolt targetPort = %#v, want %q", got, want)
			}
		default:
			t.Fatalf("unexpected nornicdb service port %#v", port)
		}
	}
}

func TestHelmBundledNornicDBRequiresRelationshipMergePropertyIdentity(t *testing.T) {
	t.Parallel()

	output := renderHelmChartFailure(
		t,
		"--set", "nornicdb.enabled=true",
		"--set", "env.ESHU_GRAPH_BACKEND=nornicdb",
		"--set", "nornicdb.capabilities.relationshipMergePropertyIdentity=false",
		"--set", "schemaBootstrap.useHelmHooks=false",
	)
	if !strings.Contains(
		output,
		"nornicdb.capabilities.relationshipMergePropertyIdentity=true",
	) {
		t.Fatalf("helm compatibility error = %q, want relationship identity requirement", output)
	}
}

func TestHelmExternalNornicDBRequiresRelationshipMergePropertyIdentity(t *testing.T) {
	t.Parallel()

	output := renderHelmChartFailure(
		t,
		"--set", "nornicdb.enabled=false",
		"--set", "env.ESHU_GRAPH_BACKEND=nornicdb",
		"--set", "nornicdb.capabilities.relationshipMergePropertyIdentity=false",
		"--set", "schemaBootstrap.useHelmHooks=false",
	)
	if !strings.Contains(
		output,
		"nornicdb.capabilities.relationshipMergePropertyIdentity=true",
	) {
		t.Fatalf("helm compatibility error = %q, want external NornicDB relationship identity requirement", output)
	}
}

func TestHelmResolutionEngineNornicDBOverrideRequiresRelationshipMergePropertyIdentity(t *testing.T) {
	t.Parallel()

	output := renderHelmChartFailure(
		t,
		"--set", "env.ESHU_GRAPH_BACKEND=neo4j",
		"--set", "resolutionEngine.env.ESHU_GRAPH_BACKEND=nornicdb",
		"--set", "nornicdb.capabilities.relationshipMergePropertyIdentity=false",
		"--set", "schemaBootstrap.useHelmHooks=false",
	)
	if !strings.Contains(
		output,
		"nornicdb.capabilities.relationshipMergePropertyIdentity=true",
	) {
		t.Fatalf("helm compatibility error = %q, want workload NornicDB relationship identity requirement", output)
	}
}

func TestHelmDisabledWorkloadNornicDBOverrideDoesNotRequireCapability(t *testing.T) {
	t.Parallel()

	renderHelmChart(
		t,
		"--set", "env.ESHU_GRAPH_BACKEND=neo4j",
		"--set", "workflowCoordinator.enabled=false",
		"--set", "workflowCoordinator.env.ESHU_GRAPH_BACKEND=nornicdb",
		"--set", "nornicdb.capabilities.relationshipMergePropertyIdentity=false",
	)
}

func TestHelmEnabledWorkloadBackendOverridesUseRenderedPrecedence(t *testing.T) {
	t.Parallel()

	renderHelmChart(
		t,
		"--set", "env.ESHU_GRAPH_BACKEND=nornicdb",
		"--set", "api.env.ESHU_GRAPH_BACKEND=neo4j",
		"--set", "mcpServer.env.ESHU_GRAPH_BACKEND=neo4j",
		"--set", "repoSync.enabled=false",
		"--set", "resolutionEngine.enabled=false",
		"--set", "schemaBootstrap.enabled=false",
		"--set", "nornicdb.capabilities.relationshipMergePropertyIdentity=false",
	)
}

func TestHelmResolutionLaneBackendOverrideUsesRenderedPrecedence(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "resolution-lane-backend.yaml")
	values := []byte(`
env:
  ESHU_GRAPH_BACKEND: neo4j
resolutionEngine:
  enabled: true
  env:
    ESHU_GRAPH_BACKEND: nornicdb
  lanes:
    - name: provenance
      domains:
        - provenance_materialization
      env:
        ESHU_GRAPH_BACKEND: neo4j
nornicdb:
  capabilities:
    relationshipMergePropertyIdentity: false
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write resolution lane values: %v", err)
	}

	renderHelmChart(t, "-f", valuesPath)
}

func TestHelmResolutionLaneSelectingNornicDBRequiresCapability(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "resolution-lane-nornicdb.yaml")
	values := []byte(`
env:
  ESHU_GRAPH_BACKEND: neo4j
resolutionEngine:
  enabled: true
  env:
    ESHU_GRAPH_BACKEND: neo4j
  lanes:
    - name: provenance
      domains:
        - provenance_materialization
      env:
        ESHU_GRAPH_BACKEND: nornicdb
nornicdb:
  capabilities:
    relationshipMergePropertyIdentity: false
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write resolution lane values: %v", err)
	}

	output := renderHelmChartFailure(t, "-f", valuesPath)
	if !strings.Contains(
		output,
		"nornicdb.capabilities.relationshipMergePropertyIdentity=true",
	) {
		t.Fatalf("helm compatibility error = %q, want lane NornicDB relationship identity requirement", output)
	}
}

func TestHelmNeo4jBackendDoesNotRequireNornicDBRelationshipMergePropertyIdentity(t *testing.T) {
	t.Parallel()

	renderHelmChart(
		t,
		"--set", "env.ESHU_GRAPH_BACKEND=neo4j",
		"--set", "nornicdb.capabilities.relationshipMergePropertyIdentity=false",
		"--set", "schemaBootstrap.useHelmHooks=false",
	)
}

func TestHelmBundledNornicDBRejectsInvalidBindAddressShape(t *testing.T) {
	t.Parallel()

	output := renderHelmChartFailure(
		t,
		"--set", "nornicdb.enabled=true",
		"--set", "nornicdb.capabilities.relationshipMergePropertyIdentity=true",
		"--set", "schemaBootstrap.useHelmHooks=false",
		"--set", "nornicdb.bindAddress=123",
	)
	if !strings.Contains(output, "/nornicdb/bindAddress") {
		t.Fatalf("helm schema error = %q, want /nornicdb/bindAddress", output)
	}
}
