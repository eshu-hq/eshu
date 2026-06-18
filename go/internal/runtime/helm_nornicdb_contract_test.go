package runtime

import (
	"strings"
	"testing"
)

func TestHelmBundledNornicDBUsesGraphOnlySearchControls(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(t, "--set", "nornicdb.enabled=true", "--set", "schemaBootstrap.useHelmHooks=false")
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

	manifests := renderHelmChart(t, "--set", "nornicdb.enabled=true", "--set", "schemaBootstrap.useHelmHooks=false")
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

func TestHelmBundledNornicDBRejectsInvalidBindAddressShape(t *testing.T) {
	t.Parallel()

	output := renderHelmChartFailure(t,
		"--set", "nornicdb.enabled=true",
		"--set", "schemaBootstrap.useHelmHooks=false",
		"--set", "nornicdb.bindAddress=123",
	)
	if !strings.Contains(output, "/nornicdb/bindAddress") {
		t.Fatalf("helm schema error = %q, want /nornicdb/bindAddress", output)
	}
}
