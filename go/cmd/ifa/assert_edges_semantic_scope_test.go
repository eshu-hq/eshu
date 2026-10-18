// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
)

const semanticScopeTestRepoID = "repo-ifa-semantic-entity-containment"

func semanticScopeTestExpected(t *testing.T) []materializededges.ExpectedEdge {
	t.Helper()
	path := filepath.Join(repoRootDir(t), "go/internal/ifa/testdata/semanticentitycontainment/ifa-semantic-entity-containment-family-expected-edges.json")
	repoID, expected, err := materializededges.LoadSemanticEntityContainmentExpectedEdges(path)
	if err != nil {
		t.Fatalf("LoadSemanticEntityContainmentExpectedEdges: %v", err)
	}
	if repoID != semanticScopeTestRepoID {
		t.Fatalf("fixture repo_id = %q, want %q (fail-closed from the fixture, never a literal)", repoID, semanticScopeTestRepoID)
	}
	if len(expected) != 4 {
		t.Fatalf("committed semantic expected count = %d, want exact 4", len(expected))
	}
	return expected
}

func semanticScopeTestEdge(fromUID, fromRepo string, fromLabels []string, toUID, toRepo string, toLabels []string) graphdump.Edge {
	fromProps := map[string]any{"uid": fromUID}
	if fromRepo != "" {
		fromProps["repo_id"] = fromRepo
	}
	return graphdump.Edge{
		Type:       "CONTAINS",
		FromLabels: fromLabels,
		FromProps:  fromProps,
		ToLabels:   toLabels,
		ToProps:    map[string]any{"uid": toUID, "repo_id": toRepo},
		Props:      map[string]any{"evidence_source": "parser/semantic-entities"},
	}
}

func semanticScopeTestGraph() []graphdump.Edge {
	repo := semanticScopeTestRepoID
	return []graphdump.Edge{
		semanticScopeTestEdge(repo+":app/fetch.py", "", []string{"File"}, "content-entity:sec-annotation", repo, []string{"Annotation"}),
		semanticScopeTestEdge(repo+":app/fetch.py", "", []string{"File"}, "content-entity:sec-py-func", repo, []string{"Function"}),
		semanticScopeTestEdge(repo+":lib/worker.ex", "", []string{"File"}, "content-entity:sec-ex-var", repo, []string{"Variable"}),
		semanticScopeTestEdge(repo+":pkg/handle.go", "", []string{"File"}, "content-entity:sec-go-func", repo, []string{"Function"}),
	}
}

func TestAssertSemanticEntityContainmentExactSetGreen(t *testing.T) {
	t.Parallel()
	expected := semanticScopeTestExpected(t)
	if err := assertSemanticEntityContainmentEdges(context.Background(), fakeEdgeReader{edges: semanticScopeTestGraph()}, semanticScopeTestRepoID, expected); err != nil {
		t.Fatalf("exact fixture population rejected: %v", err)
	}
}

func TestAssertSemanticEntityContainmentMissingIsRed(t *testing.T) {
	t.Parallel()
	expected := semanticScopeTestExpected(t)
	graph := semanticScopeTestGraph()[:3]
	err := assertSemanticEntityContainmentEdges(context.Background(), fakeEdgeReader{edges: graph}, semanticScopeTestRepoID, expected)
	if err == nil {
		t.Fatal("dropped producer edge accepted; missing must be red")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %q, want the missing diagnostic", err.Error())
	}
}

func TestAssertSemanticEntityContainmentSameRepoExtraIsRed(t *testing.T) {
	t.Parallel()
	expected := semanticScopeTestExpected(t)
	repo := semanticScopeTestRepoID
	graph := append(semanticScopeTestGraph(), semanticScopeTestEdge(
		repo+":pkg/handle.go", "", []string{"File"}, "content-entity:sec-py-plain", repo, []string{"Function"},
	))
	err := assertSemanticEntityContainmentEdges(context.Background(), fakeEdgeReader{edges: graph}, semanticScopeTestRepoID, expected)
	if err == nil {
		t.Fatal("same-repo spurious edge accepted; extra must be red")
	}
	if !strings.Contains(err.Error(), "extra") {
		t.Fatalf("error = %q, want the extra diagnostic", err.Error())
	}
}

func TestAssertSemanticEntityContainmentDuplicateIsRed(t *testing.T) {
	t.Parallel()
	expected := semanticScopeTestExpected(t)
	graph := append(semanticScopeTestGraph(), semanticScopeTestGraph()[0])
	err := assertSemanticEntityContainmentEdges(context.Background(), fakeEdgeReader{edges: graph}, semanticScopeTestRepoID, expected)
	if err == nil {
		t.Fatal("duplicated edge accepted; duplicate must be red")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error = %q, want the duplicate diagnostic", err.Error())
	}
}

// TestAssertSemanticEntityContainmentForeignRepoTouchingExpectedIsRed is the
// seeded violation for the repo scope: a foreign-repo edge that reuses an
// expected endpoint stays in scope through the rationale-pattern fallback,
// so repository-property drift (or cross-repo pollution) cannot hide behind
// the scope filter.
func TestAssertSemanticEntityContainmentForeignRepoTouchingExpectedIsRed(t *testing.T) {
	t.Parallel()
	expected := semanticScopeTestExpected(t)
	graph := append(semanticScopeTestGraph(), semanticScopeTestEdge(
		"repo-foreign:app/other.py", "", []string{"File"}, "content-entity:sec-py-func", "repo-foreign", []string{"Function"},
	))
	err := assertSemanticEntityContainmentEdges(context.Background(), fakeEdgeReader{edges: graph}, semanticScopeTestRepoID, expected)
	if err == nil {
		t.Fatal("foreign-repo edge reusing an expected endpoint accepted; the endpoint fallback must keep it red")
	}
	if !strings.Contains(err.Error(), "extra") {
		t.Fatalf("error = %q, want the extra diagnostic", err.Error())
	}
}

// TestAssertSemanticEntityContainmentWhollyForeignGhostIsInvisible documents
// the verdict's accepted blind spot: a ghost edge wholly in a foreign repo,
// touching no expected endpoint, is out of scope and ignored. It is bounded
// by per-repo ownership plus the bolt retract proof plus the cross-N digest,
// not by this cell.
func TestAssertSemanticEntityContainmentWhollyForeignGhostIsInvisible(t *testing.T) {
	t.Parallel()
	expected := semanticScopeTestExpected(t)
	graph := append(semanticScopeTestGraph(), semanticScopeTestEdge(
		"repo-foreign:app/other.py", "", []string{"File"}, "content-entity:foreign", "repo-foreign", []string{"Function"},
	))
	if err := assertSemanticEntityContainmentEdges(context.Background(), fakeEdgeReader{edges: graph}, semanticScopeTestRepoID, expected); err != nil {
		t.Fatalf("wholly-foreign ghost edge red the cell: %v (the verdict scopes it out; remove this test only with an arbiter ruling)", err)
	}
}

// TestAssertSemanticEntityContainmentIgnoresSiblingCanonicalPopulation pins
// the no-sibling-coupling half of the verdict: the canonical node writer's
// own CONTAINS edges in the same graph (different endpoints, different
// stamp) must not move this family's exact set.
func TestAssertSemanticEntityContainmentIgnoresSiblingCanonicalPopulation(t *testing.T) {
	t.Parallel()
	expected := semanticScopeTestExpected(t)
	graph := append(semanticScopeTestGraph(), graphdump.Edge{
		Type:       "CONTAINS",
		FromLabels: []string{"Repository"},
		FromProps:  map[string]any{"id": "repo-ifa-semantic-entity-containment"},
		ToLabels:   []string{"Directory"},
		ToProps:    map[string]any{"id": "repo-ifa-semantic-entity-containment:pkg"},
		Props:      map[string]any{"evidence_source": "projector/canonical"},
	})
	if err := assertSemanticEntityContainmentEdges(context.Background(), fakeEdgeReader{edges: graph}, semanticScopeTestRepoID, expected); err != nil {
		t.Fatalf("canonical writer's own CONTAINS population red the cell: %v", err)
	}
}

// TestRunAssertEdgesCommandDispatchesSemanticRepoScope proves the
// -domain semantic_entity_containment branch engages the repo scope: a
// wholly-foreign ghost passes here while the unscoped generic comparison
// would fail it as an extra. Without this, the branch could silently be the
// generic path and the scope a dead letter.
func TestRunAssertEdgesCommandDispatchesSemanticRepoScope(t *testing.T) {
	// Load the fixture through the same helper the command uses: this
	// fail-fasts an invalid fixture (repo/count asserted inside) before
	// the dispatch runs, and the ghost below must stay foreign to it.
	expected := semanticScopeTestExpected(t)
	for _, e := range expected {
		if e.TargetEntityID == "content-entity:foreign" {
			t.Fatalf("fixture unexpectedly contains the foreign ghost target")
		}
	}
	path := filepath.Join(repoRootDir(t), "go/internal/ifa/testdata/semanticentitycontainment/ifa-semantic-entity-containment-family-expected-edges.json")
	graph := append(semanticScopeTestGraph(), semanticScopeTestEdge(
		"repo-foreign:app/other.py", "", []string{"File"}, "content-entity:foreign", "repo-foreign", []string{"Function"},
	))
	original := openAssertEdgesReader
	openAssertEdgesReader = func(context.Context) (graphdump.Reader, func(), error) {
		return fakeEdgeReader{edges: graph}, func() {}, nil
	}
	t.Cleanup(func() { openAssertEdgesReader = original })

	var stdout, stderr strings.Builder
	if err := runAssertEdgesCommand(
		context.Background(),
		[]string{"-domain", "semantic_entity_containment", "-expected", path},
		&stdout, &stderr,
	); err != nil {
		t.Fatalf("scoped branch rejected the foreign ghost the scope exists to ignore: %v", err)
	}
}

// TestLoadSemanticEntityContainmentExpectedEdgesFailsClosedOnBlankRepo pins
// the fail-closed half of the verdict: a fixture that stops declaring its
// repo must error, never silently scope to nothing (or everything).
func TestLoadSemanticEntityContainmentExpectedEdgesFailsClosedOnBlankRepo(t *testing.T) {
	t.Parallel()
	raw := `{"odu":"odu:test","edges":[{"relationship_type":"CONTAINS","source_entity_id":"a","target_entity_id":"b"}]}`
	path := filepath.Join(t.TempDir(), "semantic-expected.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, _, err := materializededges.LoadSemanticEntityContainmentExpectedEdges(path); err == nil {
		t.Fatal("edge without repo_id loaded clean; the loader must fail closed")
	}
}
