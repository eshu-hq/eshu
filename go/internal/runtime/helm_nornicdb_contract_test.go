package runtime

import "testing"

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
