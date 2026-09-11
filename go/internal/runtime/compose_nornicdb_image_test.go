// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const nornicDBV131Image = "timothyswt/nornicdb-cpu-bge:v1.3.1@sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962"

func TestNornicDBComposeDefaultPinsV131PublishedImage(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.yaml")
	service := requireComposeService(t, doc, "nornicdb")
	if want := "${NORNICDB_IMAGE:-" + nornicDBV131Image + "}"; service.Image != want {
		t.Fatalf("nornicdb image = %q, want immutable v1.3.1 default %q", service.Image, want)
	}
	content := readRepositoryFile(t, "../../..", "docker-compose.yaml")
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

	docs := readRepositoryFile(t, "../../..", "docs/public/run-locally/docker-compose.md")
	docs = strings.Join(strings.Fields(docs), " ")
	for _, want := range []string{
		"NORNICDB_IMAGE",
		"NORNICDB_PULL_POLICY",
		"pull policy `missing`",
		"v1.3.1@sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962",
		"fresh graph volume",
		"Never start an older NornicDB binary on a volume modified by v1.3.1",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("docker compose docs missing exact-source override guidance %q", want)
		}
	}
}

func TestNornicDBRuntimeReadmeTracksV131PublishedDefault(t *testing.T) {
	t.Parallel()

	docs := readRepositoryFile(t, "../../..", "go/internal/runtime/README.md")
	want := "Compose pulls the immutable NornicDB v1.3.1 multi-architecture image"
	if !strings.Contains(strings.Join(strings.Fields(docs), " "), want) {
		t.Fatalf("runtime README missing current NornicDB published-image contract %q", want)
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

func TestNornicDBDefaultNoLongerCarriesSourceBuildControls(t *testing.T) {
	t.Parallel()

	for _, file := range []string{
		"docker-compose.yaml",
		"docs/public/run-locally/docker-compose.md",
		".github/workflows/ifa-determinism-gate.yml",
		".github/workflows/e2e-tests.yml",
		".github/workflows/golden-corpus-gate.yml",
		".github/workflows/frontend.yml",
		".github/workflows/value-flow-conformance-expectation.yml",
	} {
		content := readRepositoryFile(t, "../../..", file)
		if strings.Contains(content, "NORNICDB_HEADLESS") {
			t.Fatalf("%s still carries NORNICDB_HEADLESS after the default moved from a source build to a published image", file)
		}
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
		"Compose, Helm, and the R-5 replay gate pin the same NornicDB `v1.3.1` multi-architecture image by digest",
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
// Before #6296 the chart's image had no gate coverage at all: B-7 and the e2e
// workflows then drove a Compose-only source build rather than the chart's
// published image. Putting the chart and replay gate on one artifact bought
// that coverage, and this test keeps the current v1.3.1 digest in lockstep.
// scripts/test-verify-replay-tier.sh pins
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
	if chartRef != nornicDBV131Image {
		t.Fatalf("chart NornicDB image = %q, want validated v1.3.1 artifact %q", chartRef, nornicDBV131Image)
	}

	compose := readComposeDocument(t, "docker-compose.yaml")
	composeRef := strings.TrimSuffix(strings.TrimPrefix(requireComposeService(t, compose, "nornicdb").Image, "${NORNICDB_IMAGE:-"), "}")
	if composeRef != chartRef {
		t.Fatalf("docker-compose.yaml defaults to %q but deploy/helm/eshu/values.yaml renders %q", composeRef, chartRef)
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

	governanceProof := readRepositoryFile(t, "../../..", "scripts/run-k8s-two-team-governance-proof.sh")
	if !strings.Contains(governanceProof, `nornicdb_image="`+chartRef+`"`) {
		t.Fatalf("scripts/run-k8s-two-team-governance-proof.sh does not deploy the lockstep NornicDB image %q", chartRef)
	}

	governanceYAML := readRepositoryFile(t, "../../..", "deploy/helm/eshu/ci/governance-two-team-k8s.values.yaml")
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
