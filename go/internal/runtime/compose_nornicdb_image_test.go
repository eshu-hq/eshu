// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const nornicDBPinnedImage = "ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1"

func TestNornicDBComposeDefaultPinsPublishedImage(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.yaml")
	service := requireComposeService(t, doc, "nornicdb")
	if want := "${NORNICDB_IMAGE:-" + nornicDBPinnedImage + "}"; service.Image != want {
		t.Fatalf("nornicdb image = %q, want immutable pinned default %q", service.Image, want)
	}
	content := readRepositoryFile(t, deployCheckoutRoot(t), "compose/docker-compose.yaml")
	if want := "pull_policy: ${NORNICDB_PULL_POLICY:-missing}"; !strings.Contains(content, want) {
		t.Fatalf("nornicdb service missing cache-safe immutable pull policy %q", want)
	}
	var raw struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		t.Fatalf("parse docker-compose.yaml for raw service keys: %v", err)
	}
	if build, ok := raw.Services["nornicdb"]["build"]; ok {
		t.Fatalf("nornicdb default unexpectedly carries a source build: %#v", build)
	}
}

func TestNornicDBComposeDocumentsImageAndPullPolicyOverrides(t *testing.T) {
	t.Parallel()

	docs := readRepositoryFile(t, deployCheckoutRoot(t), "docs/run-locally/docker-compose.md")
	docs = strings.Join(strings.Fields(docs), " ")
	for _, want := range []string{
		"NORNICDB_IMAGE",
		"NORNICDB_PULL_POLICY",
		"pull policy `missing`",
		"fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1",
		"fresh graph volume",
		"Never start an older NornicDB binary on a volume modified by v1.3.3",
		"reports `NornicDB v1.3.3`: upstream's v1.3.2 tag retained a stale embedded VERSION file",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("docker compose docs missing exact-source override guidance %q", want)
		}
	}
}

func TestNornicDBRuntimeReadmeTracksPinnedDefault(t *testing.T) {
	t.Parallel()

	docs := readRepositoryFile(t, "../../..", "go/internal/runtime/README.md")
	want := "Compose pulls the immutable eshu-hq self-built NornicDB image by default"
	if !strings.Contains(strings.Join(strings.Fields(docs), " "), want) {
		t.Fatalf("runtime README missing current NornicDB published-image contract %q", want)
	}
}

func TestNornicDBComposeDefaultsToLiveProvenAmd64Platform(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.yaml")
	service := requireComposeService(t, doc, "nornicdb")
	want := "${NORNICDB_PLATFORM:-linux/amd64}"
	if service.Platform != want {
		t.Fatalf("nornicdb platform = %q, want live-proven default %q", service.Platform, want)
	}
}

func TestNornicDBComposeDefaultsToFreshV132FormatGraphVolume(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.yaml")
	service := requireComposeService(t, doc, "nornicdb")
	assertComposeNamedVolume(t, service, "nornicdb_v132_data", "/data")
	if _, ok := doc.Volumes["nornicdb_v132_data"]; !ok {
		t.Fatal("docker-compose.yaml does not declare the v1.3.2-format graph volume reused by v1.3.3")
	}
	if _, ok := doc.Volumes["nornicdb_data"]; ok {
		t.Fatal("docker-compose.yaml still declares the pre-v1.3.2 graph volume")
	}
}

func TestNornicDBDefaultNoLongerCarriesSourceBuildControls(t *testing.T) {
	t.Parallel()

	for _, file := range []struct {
		root, rel string
	}{
		{deployCheckoutRoot(t), "compose/docker-compose.yaml"},
		{deployCheckoutRoot(t), "docs/run-locally/docker-compose.md"},
		{"../../..", ".github/workflows/ifa-determinism-gate.yml"},
		{"../../..", ".github/workflows/e2e-tests.yml"},
		{"../../..", ".github/workflows/golden-corpus-gate.yml"},
		{"../../..", ".github/workflows/frontend.yml"},
	} {
		content := readRepositoryFile(t, file.root, file.rel)
		if strings.Contains(content, "NORNICDB_HEADLESS") {
			t.Fatalf("%s still carries NORNICDB_HEADLESS after the default moved from a source build to a published image", file.rel)
		}
	}
}

func TestNornicDBComposeDisablesSearchIndexPersistence(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, deployCheckoutRoot(t), "compose/docker-compose.yaml")
	want := `NORNICDB_PERSIST_SEARCH_INDEXES: "false"`
	if !strings.Contains(content, want) {
		t.Fatalf("docker-compose.yaml must not persist disabled NornicDB search indexes for graph-only startup, want %q", want)
	}
}

func TestNornicDBComposeDisablesEmbeddingsByDefault(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, deployCheckoutRoot(t), "compose/docker-compose.yaml")
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

	docs := readRepositoryFile(t, deployCheckoutRoot(t), "docs/run-locally/docker-compose.md")
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

	compose := readRepositoryFile(t, deployCheckoutRoot(t), "compose/docker-compose.yaml")
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
		"Compose and Helm pin the same eshu-hq self-built NornicDB image by digest; the R-5 replay gate runs Neo4j since #10223",
		"Runtime contract tests enforce the graph-only NornicDB controls",
	} {
		if !strings.Contains(normalizedDocs, want) {
			t.Fatalf("issue-430 design doc missing implemented stabilization status %q", want)
		}
	}
}

// digestedImageRef requires a full 64-hex sha256 digest. A tag-only reference
// must not satisfy the lockstep assertion: Docker Hub can retarget a tag
// without any repository change, which is the whole reason these pins exist.
var digestedImageRef = regexp.MustCompile(`^[^:@\s]+:[^@\s]+@sha256:[0-9a-f]{64}$`)

// TestHelmNornicDBImageMatchesComposeDefault binds the chart's bundled NornicDB
// default to the Compose default and the validated pinned artifact.
//
// Before #6296 the chart's image had no gate coverage at all: B-7 and the e2e
// workflows then drove a Compose-only source build rather than the chart's
// published image. Putting the chart and Compose default on one artifact bought
// that coverage, and this test keeps the current pinned digest in lockstep.
// TestNornicDBGraphSearchSplitDesignTracksImplementedStabilization pins the
// design doc's prose.
//
// Since #10223 the R-5 replay gate runs Neo4j, so it no longer exercises this
// image: the replay-tier legs retired with the retarget, and this test now
// holds only chart, Compose default, pin, governance proof, and operator docs
// in static lockstep. Zero CI execution coverage for NornicDB remains, per the
// owner's NornicDB-off decision.
//
// The reference is compared whole (repository, tag, and digest), not digest
// alone: a chart that pointed a different repository at the same digest would
// silently stop matching the pinned artifact this test holds.
func TestHelmNornicDBImageMatchesComposeDefault(t *testing.T) {
	t.Parallel()

	valuesYAML := readRepositoryFile(t, deployCheckoutRoot(t), "helm/eshu/values.yaml")
	var values map[string]any
	if err := yaml.Unmarshal([]byte(valuesYAML), &values); err != nil {
		t.Fatalf("parse helm/eshu/values.yaml: %v", err)
	}
	image := helmMap(helmMap(values["nornicdb"])["image"])
	repository, _ := image["repository"].(string)
	tag, _ := image["tag"].(string)
	if repository == "" || tag == "" {
		t.Fatalf("nornicdb.image.repository/tag missing from helm/eshu/values.yaml, got repository=%q tag=%q", repository, tag)
	}
	chartRef := repository + ":" + tag
	if !digestedImageRef.MatchString(chartRef) {
		t.Fatalf("chart nornicdb image %q is not pinned by a full sha256 digest; a tag alone can be retargeted upstream without a repository change", chartRef)
	}
	if chartRef != nornicDBPinnedImage {
		t.Fatalf("chart NornicDB image = %q, want validated pinned artifact %q", chartRef, nornicDBPinnedImage)
	}

	compose := readComposeDocument(t, "docker-compose.yaml")
	composeRef := strings.TrimSuffix(strings.TrimPrefix(requireComposeService(t, compose, "nornicdb").Image, "${NORNICDB_IMAGE:-"), "}")
	if composeRef != chartRef {
		t.Fatalf("docker-compose.yaml defaults to %q but helm/eshu/values.yaml renders %q", composeRef, chartRef)
	}

	operatorDocs := readRepositoryFile(t, deployCheckoutRoot(t), "docs/kubernetes/helm-routing-and-storage-values.md")
	if !strings.Contains(operatorDocs, tag) {
		t.Fatalf("docs/kubernetes/helm-routing-and-storage-values.md does not name the chart's nornicdb.image.tag %q, so operators read a stale pin", tag)
	}

	// The k8s two-team governance proof script moved to eshu-deploy with the
	// chart; its lockstep-image assertion travels in the dest-repo change
	// proposal, not here: the pinned checkout predates the proof's arrival.

	governanceYAML := readRepositoryFile(t, deployCheckoutRoot(t), "helm/eshu/ci/governance-two-team-k8s.values.yaml")
	var governanceValues map[string]any
	if err := yaml.Unmarshal([]byte(governanceYAML), &governanceValues); err != nil {
		t.Fatalf("parse governance-two-team-k8s.values.yaml: %v", err)
	}
	governanceImage := helmMap(helmMap(governanceValues["nornicdb"])["image"])
	governanceRef, _ := governanceImage["repository"].(string)
	governanceTag, _ := governanceImage["tag"].(string)
	if governanceRef+":"+governanceTag != chartRef {
		t.Fatalf("governance proof renders %q, want lockstep image %q", governanceRef+":"+governanceTag, chartRef)
	}
}
