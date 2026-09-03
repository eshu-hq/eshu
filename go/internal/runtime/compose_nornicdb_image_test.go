// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNornicDBComposeDefaultPinsMergedPR290ExactSourceCommit(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "docker-compose.yaml")
	oldDefault := "timothyswt/nornicdb-amd64-cpu:latest"
	if strings.Contains(content, oldDefault) {
		t.Fatalf("docker-compose.yaml still defaults to stale amd64-only image %q", oldDefault)
	}

	for _, want := range []string{
		"image: ${NORNICDB_IMAGE:-eshu-nornicdb-pr290:3722b483c02c}",
		"pull_policy: ${NORNICDB_PULL_POLICY:-build}",
		"context: https://github.com/orneryd/NornicDB.git#3722b483c02c38a8e046d198f8768f200f31023c",
		"dockerfile: docker/Dockerfile.cpu-bge",
		"org.opencontainers.image.revision: 3722b483c02c38a8e046d198f8768f200f31023c",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("docker-compose.yaml missing exact merged NornicDB PR #290 pin %q", want)
		}
	}

	if strings.Contains(content, "checksum=") {
		t.Fatal("docker-compose.yaml must not require the BuildKit source.git.checksum query feature")
	}
}

func TestNornicDBComposeDocumentsImageAndPullPolicyOverrides(t *testing.T) {
	t.Parallel()

	docs := readRepositoryFile(t, "../../..", "docs/public/run-locally/docker-compose.md")
	for _, want := range []string{
		"NORNICDB_IMAGE",
		"NORNICDB_PULL_POLICY",
		"pull policy `build`",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("docker compose docs missing exact-source override guidance %q", want)
		}
	}
}

func TestNornicDBRuntimeReadmeTracksPR290SourceBuildDefault(t *testing.T) {
	t.Parallel()

	docs := readRepositoryFile(t, "../../..", "go/internal/runtime/README.md")
	want := "Compose builds the exact orneryd/NornicDB#290 source commit"
	if !strings.Contains(strings.Join(strings.Fields(docs), " "), want) {
		t.Fatalf("runtime README missing current NornicDB source-build contract %q", want)
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

// TestNornicDBComposeHeadlessBuildArgDefaultsFalse pins the #6505 fix:
// docker-compose.yaml must thread the upstream Dockerfile.cpu-bge HEADLESS
// build arg through, defaulting to false so local runs keep the full UI
// build, while CI-only callers opt into the headless backend image via
// NORNICDB_HEADLESS=true (the pinned source's UI stage fails tsc).
func TestNornicDBComposeHeadlessBuildArgDefaultsFalse(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "docker-compose.yaml")
	want := "HEADLESS: ${NORNICDB_HEADLESS:-false}"
	if !strings.Contains(content, want) {
		t.Fatalf("docker-compose.yaml must default the NornicDB UI build to full (local dev), want %q", want)
	}

	docs := readRepositoryFile(t, "../../..", "docs/public/run-locally/docker-compose.md")
	if !strings.Contains(docs, "NORNICDB_HEADLESS") {
		t.Fatal("docker compose docs must document the NORNICDB_HEADLESS CI knob")
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
		"Helm pins NornicDB `v1.2.3` by digest; Compose temporarily pins the exact orneryd/NornicDB#290 source commit",
		"Runtime contract tests enforce the graph-only NornicDB controls",
	} {
		if !strings.Contains(normalizedDocs, want) {
			t.Fatalf("issue-430 design doc missing implemented stabilization status %q", want)
		}
	}
}

// replayTierImageAssignment matches the live gate's own image assignment in
// scripts/verify-replay-tier.sh.
var replayTierImageAssignment = regexp.MustCompile(`(?m)^NORNICDB_IMAGE="([^"]+)"$`)

// replayTierMirrorImagePin matches the anchored rg pattern that
// scripts/test-verify-replay-tier.sh uses to hold the gate's image steady. The
// pattern is a regex embedded in shell single quotes, so its dots arrive here
// backslash-escaped and are unescaped before comparison.
var replayTierMirrorImagePin = regexp.MustCompile(`\^NORNICDB_IMAGE="([^"]+)"\$`)

// digestedImageRef requires a full 64-hex sha256 digest. A tag-only reference
// must not satisfy the lockstep assertion: Docker Hub can retarget a tag
// without any repository change, which is the whole reason these pins exist.
var digestedImageRef = regexp.MustCompile(`^[^:@\s]+:[^@\s]+@sha256:[0-9a-f]{64}$`)

// TestHelmNornicDBImageMatchesReplayTierGate binds the chart's bundled NornicDB
// default to the artifact the R-5 replay gate actually exercises.
//
// Before #6296 the chart's image had no gate coverage at all: the B-7
// golden-corpus gate and the e2e workflows drive docker-compose.yaml, which
// builds the orneryd/NornicDB#290 source commit, not the chart's published
// image. Putting the chart and the replay gate on one build is what buys that
// coverage — and nothing enforced it. scripts/test-verify-replay-tier.sh pins
// the gate's own NORNICDB_IMAGE and TestNornicDBGraphSearchSplitDesignTracks-
// ImplementedStabilization pins the design doc's prose, but either file could
// move without the other and every existing test would stay green while the
// claim quietly stopped being true.
//
// The reference is compared whole (repository, tag, and digest), not digest
// alone: a chart that pointed a different repository at the same digest would
// still render an image the gate never ran.
func TestHelmNornicDBImageMatchesReplayTierGate(t *testing.T) {
	t.Parallel()

	valuesYAML := readRepositoryFile(t, "../../..", "deploy/helm/eshu/values.yaml")
	var values map[string]any
	if err := yaml.Unmarshal([]byte(valuesYAML), &values); err != nil {
		t.Fatalf("parse deploy/helm/eshu/values.yaml: %v", err)
	}
	image := helmMap(helmMap(values["nornicdb"])["image"])
	repository, _ := image["repository"].(string)
	tag, _ := image["tag"].(string)
	if repository == "" || tag == "" {
		t.Fatalf("nornicdb.image.repository/tag missing from deploy/helm/eshu/values.yaml, got repository=%q tag=%q", repository, tag)
	}
	chartRef := repository + ":" + tag
	if !digestedImageRef.MatchString(chartRef) {
		t.Fatalf("chart nornicdb image %q is not pinned by a full sha256 digest; a tag alone can be retargeted upstream without a repository change", chartRef)
	}

	gateScript := readRepositoryFile(t, "../../..", "scripts/verify-replay-tier.sh")
	gateMatch := replayTierImageAssignment.FindStringSubmatch(gateScript)
	if gateMatch == nil {
		t.Fatal("scripts/verify-replay-tier.sh has no NORNICDB_IMAGE=\"...\" assignment; the replay-tier lockstep assertion cannot be evaluated")
	}
	if gateRef := gateMatch[1]; gateRef != chartRef {
		t.Fatalf("scripts/verify-replay-tier.sh runs %q but deploy/helm/eshu/values.yaml renders %q; the chart's image must be the artifact the R-5 replay gate exercises", gateRef, chartRef)
	}

	mirrorScript := readRepositoryFile(t, "../../..", "scripts/test-verify-replay-tier.sh")
	mirrorMatch := replayTierMirrorImagePin.FindStringSubmatch(mirrorScript)
	if mirrorMatch == nil {
		t.Fatal("scripts/test-verify-replay-tier.sh no longer pins ^NORNICDB_IMAGE=\"...\"$; the gate's image pin has lost its mirror")
	}
	if mirrorRef := strings.ReplaceAll(mirrorMatch[1], `\.`, "."); mirrorRef != chartRef {
		t.Fatalf("scripts/test-verify-replay-tier.sh pins %q but deploy/helm/eshu/values.yaml renders %q", mirrorRef, chartRef)
	}

	operatorDocs := readRepositoryFile(t, "../../..", "docs/public/deploy/kubernetes/helm-routing-and-storage-values.md")
	if !strings.Contains(operatorDocs, tag) {
		t.Fatalf("docs/public/deploy/kubernetes/helm-routing-and-storage-values.md does not name the chart's nornicdb.image.tag %q, so operators read a stale pin", tag)
	}
}
