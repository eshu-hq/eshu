package runtime

import (
	"strings"
	"testing"
)

func TestNornicDBComposeDefaultUsesPinnedMultiArchImage(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "docker-compose.yaml")
	oldDefault := "timothyswt/nornicdb-amd64-cpu:latest"
	if strings.Contains(content, oldDefault) {
		t.Fatalf("docker-compose.yaml still defaults to stale amd64-only image %q", oldDefault)
	}

	want := "image: ${NORNICDB_IMAGE:-timothyswt/nornicdb-cpu-bge:v1.1.3@sha256:42af69852ae0f34a905a0877668025d53b3783bb864549810d868e1bf94f3752}"
	if !strings.Contains(content, want) {
		t.Fatalf("docker-compose.yaml must default to a pinned multi-arch NornicDB image matching %q", want)
	}
}

func TestNornicDBComposeDoesNotForceAmd64Platform(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "docker-compose.yaml")
	oldDefault := "platform: ${NORNICDB_PLATFORM:-linux/amd64}"
	if strings.Contains(content, oldDefault) {
		t.Fatalf("docker-compose.yaml still forces amd64 with %q", oldDefault)
	}

	want := "platform: ${NORNICDB_PLATFORM:-}"
	if !strings.Contains(content, want) {
		t.Fatalf("docker-compose.yaml must leave NORNICDB_PLATFORM empty by default, want %q", want)
	}
}

func TestNornicDBComposeDisablesSearchIndexPersistence(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "docker-compose.yaml")
	want := `NORNICDB_PERSIST_SEARCH_INDEXES: "false"`
	if !strings.Contains(content, want) {
		t.Fatalf("docker-compose.yaml must not persist disabled NornicDB search indexes for graph-only startup, want %q", want)
	}
}

func TestNornicDBComposeDisablesEmbeddingsByDefault(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "docker-compose.yaml")
	want := `NORNICDB_EMBEDDING_ENABLED: "false"`
	if !strings.Contains(content, want) {
		t.Fatalf("docker-compose.yaml must disable NornicDB embeddings for indexing by default, want %q", want)
	}
}

func TestNornicDBComposeDisablesSearchIndexesByDefault(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.yaml")
	service := requireComposeService(t, doc, "nornicdb")

	for key, want := range map[string]string{
		"NORNICDB_SEARCH_BM25_ENABLED":   "false",
		"NORNICDB_SEARCH_VECTOR_ENABLED": "false",
		"NORNICDB_SEARCH_BM25_WARMING":   "lazy",
		"NORNICDB_SEARCH_VECTOR_WARMING": "lazy",
		"NORNICDB_ASYNC_WRITES_ENABLED":  "false",
		"NORNICDB_HEIMDALL_ENABLED":      "false",
		"NORNICDB_QDRANT_GRPC_ENABLED":   "false",
		"NORNICDB_EMBEDDING_ENABLED":     "false",
	} {
		assertComposeEnv(t, service, key, want)
	}
}

func TestNornicDBGraphOnlySearchStartupDocsTrackSupportedKnobs(t *testing.T) {
	t.Parallel()

	docs := readRepositoryFile(t, "../../..", "docs/public/run-locally/docker-compose.md")
	for _, want := range []string{
		"NORNICDB_EMBEDDING_ENABLED=false",
		"NORNICDB_PERSIST_SEARCH_INDEXES=false",
		"NORNICDB_SEARCH_BM25_ENABLED=false",
		"NORNICDB_SEARCH_VECTOR_ENABLED=false",
		"NORNICDB_SEARCH_BM25_WARMING=lazy",
		"NORNICDB_SEARCH_VECTOR_WARMING=lazy",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("docker compose docs missing NornicDB search startup note %q", want)
		}
	}

	compose := readRepositoryFile(t, "../../..", "docker-compose.yaml")
	if strings.Contains(compose, "NORNICDB_SEARCH_ENABLED") {
		t.Fatal("docker-compose.yaml must not advertise unsupported NORNICDB_SEARCH_ENABLED")
	}
}
