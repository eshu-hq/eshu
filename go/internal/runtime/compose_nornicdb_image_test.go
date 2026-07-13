// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

	want := "image: ${NORNICDB_IMAGE:-timothyswt/nornicdb-cpu-bge:v1.1.11@sha256:51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090}"
	if !strings.Contains(content, want) {
		t.Fatalf("docker-compose.yaml must default to a pinned multi-arch NornicDB image matching %q", want)
	}
}

func TestNornicDBPR261ComposeOverridePinsExactSourceCommit(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "docker-compose.nornicdb-pr261.yaml")
	for _, want := range []string{
		"image: eshu-nornicdb-pr261:149245885258",
		"pull_policy: never",
		"context: https://github.com/orneryd/NornicDB.git#1492458852588c884c32f70d27ea2ee07086769c",
		"dockerfile: docker/Dockerfile.cpu-bge",
		"org.opencontainers.image.revision: 1492458852588c884c32f70d27ea2ee07086769c",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("NornicDB PR #261 Compose override missing exact pin %q", want)
		}
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

func TestNornicDBEnvironmentDocsTrackGraphOnlySearchControls(t *testing.T) {
	t.Parallel()

	docs := readRepositoryFile(t, "../../..", "docs/public/reference/environment-ingestion-queues.md")
	for _, want := range []string{
		"| `NORNICDB_PERSIST_SEARCH_INDEXES` | `false` in Eshu Compose and Helm |",
		"| `NORNICDB_SEARCH_BM25_ENABLED` | `false` in Eshu Compose and Helm |",
		"| `NORNICDB_SEARCH_VECTOR_ENABLED` | `false` in Eshu Compose and Helm |",
		"| `NORNICDB_SEARCH_BM25_WARMING` | `lazy` in Eshu Compose and Helm |",
		"| `NORNICDB_SEARCH_VECTOR_WARMING` | `lazy` in Eshu Compose and Helm |",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("environment docs missing graph-only NornicDB control row %q", want)
		}
	}

	for _, stale := range []string{
		"| `NORNICDB_PERSIST_SEARCH_INDEXES` | `true` in Eshu Compose and Helm |",
		"Do not treat unpinned NornicDB BM25/vector disable or lazy-warming variables as",
		"uses persistence plus disabled embeddings as mitigation",
	} {
		if strings.Contains(docs, stale) {
			t.Fatalf("environment docs still carry stale NornicDB search startup guidance %q", stale)
		}
	}
}

func TestNornicDBGraphSearchSplitDesignTracksImplementedStabilization(t *testing.T) {
	t.Parallel()

	docs := readRepositoryFile(t, "../../..", "docs/internal/design/430-nornicdb-graph-search-split.md")
	if strings.Contains(docs, "Design only; no code, schema,") {
		t.Fatal("issue-430 design doc still says the graph-only startup stabilization has no code or config changes")
	}
	normalizedDocs := strings.Join(strings.Fields(docs), " ")
	for _, want := range []string{
		"Phase-1 stabilization status:",
		"Compose and Helm now pin NornicDB `v1.1.11`",
		"Runtime contract tests enforce the graph-only NornicDB controls",
	} {
		if !strings.Contains(normalizedDocs, want) {
			t.Fatalf("issue-430 design doc missing implemented stabilization status %q", want)
		}
	}
}
