// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestRationaleFamilyOduResolvesItsExpectedEdgeSet proves the EXTRACTOR:
// ExtractRationaleEdgeRows, over the cataloged Odù, reproduces the hand-derived
// edge set exactly.
//
// Deliberately not called coverage. A manifest coverage row names a proof GATE,
// and neither gate executes this family: verify-ifa-determinism.sh asserts
// expected edges for sql_relationships only, and MaterializedEdgeDomainEdgeTypes
// rejects every other domain. Breaking the live writer would leave this green,
// so the family stays waived with what is and is not proven on the waiver.
func TestRationaleFamilyOduResolvesItsExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := CatalogByName()[rationaleFamilyOduName]
	if odu.Name == "" {
		t.Fatalf("Odù %q is not in the catalog", rationaleFamilyOduName)
	}

	ok, detail := resolveRationaleEdgeMaterializedEdges(odu, rationaleFamilyExpectedEdgesPath(repoRoot))
	if !ok {
		t.Fatalf("rationale family Odù does not resolve: %s", detail)
	}
	if !strings.Contains(detail, "expected 3-edge set exactly") {
		t.Errorf("detail = %q, want it to name the exact edge count it proved", detail)
	}
	t.Logf("%s", detail)
}

// TestRationaleFamilyResolvesThroughTheManifestResolver proves the family is
// reachable the way the gate reaches it — by surface name through
// MaterializedEdgeOduResolver. A guard nothing dispatches to is not coverage.
func TestRationaleFamilyResolvesThroughTheManifestResolver(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	resolver := MaterializedEdgeOduResolver{Catalog: CatalogByName(), RepoRoot: repoRoot}

	ok, detail := resolver.Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + "rationale_edges",
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          rationaleFamilyOduName,
	})
	if !ok {
		t.Fatalf("resolver.Resolve for rationale_edges: %s", detail)
	}
}

// TestRationaleFamilyExpectedSetRejectsAnExtraEdge is the teeth test in the
// direction a "contains all expected" check would miss: an edge nobody derived
// must fail as loudly as a missing one, or the graph can grow silently.
func TestRationaleFamilyExpectedSetRejectsAnExtraEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := CatalogByName()[rationaleFamilyOduName]

	expected, err := loadRationaleExpectedEdges(rationaleFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("loadRationaleExpectedEdges: %v", err)
	}
	short := rationaleExpectedEdgesFile{Odu: rationaleFamilyOduName, Edges: expected[:len(expected)-1]}
	path := writeRationaleExpectedEdges(t, short)

	ok, detail := resolveRationaleEdgeMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a short expected set; an edge nobody derived went unreported")
	}
	if !strings.Contains(detail, "EXTRA") {
		t.Errorf("detail = %q, want it to name the EXTRA edge", detail)
	}
}

// TestRationaleFamilyExpectedSetRejectsAMissingEdge is the other direction: an
// expectation naming an edge the extractor does not produce must fail, or the
// fixture could quietly outrun the code.
func TestRationaleFamilyExpectedSetRejectsAMissingEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := CatalogByName()[rationaleFamilyOduName]

	expected, err := loadRationaleExpectedEdges(rationaleFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("loadRationaleExpectedEdges: %v", err)
	}
	padded := rationaleExpectedEdgesFile{
		Odu: rationaleFamilyOduName,
		Edges: append(append([]rationaleExpectedEdge{}, expected...), rationaleExpectedEdge{
			RationaleUID:   "rationale:func:payments.Refund:why:deadbeefdeadbeef",
			TargetEntityID: "func:payments.Refund",
			TargetPath:     "services/payments/refund.go",
			RepoID:         rationaleFamilyRepoID,
			CommentKind:    "why",
		}),
	}
	path := writeRationaleExpectedEdges(t, padded)

	ok, detail := resolveRationaleEdgeMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against an expectation the extractor does not satisfy")
	}
	if !strings.Contains(detail, "MISSING") {
		t.Errorf("detail = %q, want it to name the MISSING edge", detail)
	}
}

// TestRationaleFamilyKindIsPartOfTheNodeIdentity pins the pair of edges that
// share an excerpt hash.
//
// Both come from the same comment text under different kinds. If the Rationale
// UID were built from entity + hash alone they would collapse to one edge, and
// every other assertion here would still pass with a graph missing an edge. The
// shared hash is what makes that collapse possible, so it is asserted directly
// rather than left implicit in the fixture.
func TestRationaleFamilyKindIsPartOfTheNodeIdentity(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	expected, err := loadRationaleExpectedEdges(rationaleFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("loadRationaleExpectedEdges: %v", err)
	}

	byHash := map[string][]rationaleExpectedEdge{}
	for _, e := range expected {
		parts := strings.Split(e.RationaleUID, ":")
		byHash[parts[len(parts)-1]] = append(byHash[parts[len(parts)-1]], e)
	}

	shared := 0
	for hash, edges := range byHash {
		if len(edges) < 2 {
			continue
		}
		shared++
		kinds := map[string]struct{}{}
		for _, e := range edges {
			kinds[e.CommentKind] = struct{}{}
		}
		if len(kinds) != len(edges) {
			t.Errorf("excerpt hash %s is shared by %d edges but only %d distinct kind(s); they would collapse if kind left the UID", hash, len(edges), len(kinds))
		}
	}
	if shared == 0 {
		t.Error("no two expected edges share an excerpt hash; the fixture no longer proves that comment kind is part of the Rationale node identity")
	}
}

// TestRationaleFamilyOduExercisesEveryExclusion pins the fixture's negative
// cases.
//
// The Odù's value is that its inputs derive exactly three edges. Trimmed to the
// well-formed comments, every other test here still passes while the fixture
// stops proving the extractor EXCLUDES anything — and exclusions are what
// regress silently.
func TestRationaleFamilyOduExercisesEveryExclusion(t *testing.T) {
	t.Parallel()
	odu := CatalogByName()[rationaleFamilyOduName]

	var tombstones, wrongKind, missingRepo, blankEntity int
	var blankKind, blankText, duplicates int
	seen := map[string]int{}

	for _, env := range odu.Facts {
		if env.FactKind != "content_entity" {
			wrongKind++
			continue
		}
		if env.IsTombstone {
			tombstones++
			continue
		}
		entityID := strings.TrimSpace(anyToStringValue(env.Payload["entity_id"]))
		if _, ok := env.Payload["repo_id"].(string); !ok {
			missingRepo++
			continue
		}
		if entityID == "" {
			blankEntity++
			continue
		}
		comments, _ := env.Payload["rationale_comments"].([]map[string]any)
		if meta, ok := env.Payload["entity_metadata"].(map[string]any); ok {
			if nested, ok := meta["rationale_comments"].([]map[string]any); ok {
				comments = append(comments, nested...)
			}
		}
		for _, c := range comments {
			// anyToStringValue rather than a direct type assertion: a fixture
			// edited with a missing key or a non-string value should fail this
			// test as an assertion, not panic the package's whole test binary.
			kind := strings.TrimSpace(anyToStringValue(c["kind"]))
			text := strings.TrimSpace(anyToStringValue(c["text"]))
			if kind == "" {
				blankKind++
				continue
			}
			if text == "" {
				blankText++
				continue
			}
			key := entityID + "\x00" + kind + "\x00" + text
			seen[key]++
			if seen[key] > 1 {
				duplicates++
			}
		}
	}

	for name, got := range map[string]int{
		"tombstoned content entity": tombstones,
		"non-content_entity kind":   wrongKind,
		"missing repo_id":           missingRepo,
		"blank entity_id":           blankEntity,
		"blank comment kind":        blankKind,
		"whitespace-only text":      blankText,
		"duplicate comment":         duplicates,
	} {
		if got != 1 {
			t.Errorf("fixture exercises the %q exclusion %d time(s), want exactly 1", name, got)
		}
	}
}

func writeRationaleExpectedEdges(t *testing.T, file rationaleExpectedEdgesFile) string {
	t.Helper()
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("marshal expected edges: %v", err)
	}
	path := filepath.Join(t.TempDir(), "expected-edges.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write expected edges: %v", err)
	}
	return path
}
