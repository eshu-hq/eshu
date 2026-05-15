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

	want := "image: ${NORNICDB_IMAGE:-timothyswt/nornicdb-cpu-bge@sha256:"
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

func TestNornicDBComposePersistsSearchIndexes(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "docker-compose.yaml")
	want := `NORNICDB_PERSIST_SEARCH_INDEXES: "true"`
	if !strings.Contains(content, want) {
		t.Fatalf("docker-compose.yaml must persist NornicDB search indexes for large-graph restarts, want %q", want)
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
