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

// TestDocumentationFamilyOduResolvesItsExpectedEdgeSet proves the EXTRACTOR:
// ExtractDocumentationEdgeRowsWithQuarantine, run over the cataloged Odù,
// reproduces the hand-derived edge set exactly.
//
// This is deliberately not called coverage. A coverage row in the manifest
// names a proof GATE, and neither gate executes this family today --
// verify-ifa-determinism.sh asserts expected edges for sql_relationships and
// code_calls only, and MaterializedEdgeDomainEdgeTypes rejects
// documentation_edges. Breaking the
// live writer's Cypher would leave this test green, so the family stays waived
// with what is and is not proven recorded on the waiver.
func TestDocumentationFamilyOduResolvesItsExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := CatalogByName()[documentationFamilyOduName]
	if odu.Name == "" {
		t.Fatalf("Odù %q is not in the catalog", documentationFamilyOduName)
	}

	ok, detail := resolveDocumentationEdgeMaterializedEdges(odu, documentationFamilyExpectedEdgesPath(repoRoot))
	if !ok {
		t.Fatalf("documentation family Odù does not resolve: %s", detail)
	}
	if !strings.Contains(detail, "reproduces the expected 2-edge set exactly") {
		t.Errorf("detail = %q, want it to name the exact edge count it proved", detail)
	}
	t.Logf("%s", detail)
}

// TestDocumentationFamilyResolvesThroughTheManifestResolver proves the vacuity
// guard is reachable by surface name through MaterializedEdgeOduResolver, not
// only by calling it directly — a guard nothing dispatches to would be dead on
// the day a coverage row is finally added. It does NOT assert that such a row
// exists today; it does not.
func TestDocumentationFamilyResolvesThroughTheManifestResolver(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	resolver := MaterializedEdgeOduResolver{Catalog: CatalogByName(), RepoRoot: repoRoot}

	ok, detail := resolver.Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + "documentation_edges",
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          documentationFamilyOduName,
	})
	if !ok {
		t.Fatalf("resolver.Resolve for documentation_edges: %s", detail)
	}
}

// TestDocumentationFamilyExpectedSetRejectsAnExtraEdge is the teeth test.
//
// The set is asserted exactly, so an edge nobody derived has to fail as loudly
// as a missing one. A gate that only checked "every expected edge is present"
// would certify a quietly larger graph — which is the failure mode the
// materialized-edge work exists to prevent, not one it can afford to have.
func TestDocumentationFamilyExpectedSetRejectsAnExtraEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := CatalogByName()[documentationFamilyOduName]

	expected, err := loadDocumentationExpectedEdges(documentationFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("loadDocumentationExpectedEdges: %v", err)
	}

	// Drop one expected edge and write the short set to a temp fixture: the
	// extractor now produces an edge the expectation does not contain.
	short := documentationExpectedEdgesFile{Odu: documentationFamilyOduName, Edges: expected[:len(expected)-1]}
	raw, err := json.Marshal(short)
	if err != nil {
		t.Fatalf("marshal short set: %v", err)
	}
	path := filepath.Join(t.TempDir(), "short-expected-edges.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write short set: %v", err)
	}

	ok, detail := resolveDocumentationEdgeMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a short expected set; an edge nobody derived went unreported")
	}
	if !strings.Contains(detail, "EXTRA") {
		t.Errorf("detail = %q, want it to name the EXTRA edge so a reader knows which direction drifted", detail)
	}
}

// TestDocumentationFamilyExpectedSetRejectsAMissingEdge is the other direction:
// an expectation naming an edge the extractor does not produce must fail, or
// the fixture could quietly outrun the code.
func TestDocumentationFamilyExpectedSetRejectsAMissingEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := CatalogByName()[documentationFamilyOduName]

	expected, err := loadDocumentationExpectedEdges(documentationFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("loadDocumentationExpectedEdges: %v", err)
	}
	padded := documentationExpectedEdgesFile{
		Odu: documentationFamilyOduName,
		Edges: append(append([]documentationExpectedEdge{}, expected...), documentationExpectedEdge{
			SectionUID:     "docsection:doc-platform-guide|sec-overview",
			TargetEntityID: "func:payments.Refund",
			TargetKind:     "function",
		}),
	}
	raw, err := json.Marshal(padded)
	if err != nil {
		t.Fatalf("marshal padded set: %v", err)
	}
	path := filepath.Join(t.TempDir(), "padded-expected-edges.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write padded set: %v", err)
	}

	ok, detail := resolveDocumentationEdgeMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against an expectation the extractor does not satisfy")
	}
	if !strings.Contains(detail, "MISSING") {
		t.Errorf("detail = %q, want it to name the MISSING edge", detail)
	}
}

// TestDocumentationFamilyOduExercisesEveryExclusion pins the fixture's negative
// cases.
//
// The Odù's value is that its seven mentions produce exactly two edges. If a
// later edit trims the fixture to the two happy-path mentions, every other test
// here still passes while the fixture stops proving the extractor EXCLUDES
// anything — the exclusions are the part most likely to regress silently.
func TestDocumentationFamilyOduExercisesEveryExclusion(t *testing.T) {
	t.Parallel()
	odu := CatalogByName()[documentationFamilyOduName]

	if got, want := len(odu.Facts), 7; got != want {
		t.Fatalf("fixture carries %d mention(s), want %d; the exclusion cases are the proof, not padding", got, want)
	}

	var nonExact, multiCandidate, serviceKind, blankSection, duplicate int
	seen := map[string]int{}
	for _, env := range odu.Facts {
		p := env.Payload
		status, _ := p["resolution_status"].(string)
		refs, _ := p["candidate_refs"].([]map[string]any)
		section, _ := p["section_id"].(string)
		if status != "exact" {
			nonExact++
			continue
		}
		if len(refs) != 1 {
			multiCandidate++
			continue
		}
		kind, _ := refs[0]["kind"].(string)
		id, _ := refs[0]["id"].(string)
		if kind == "service" {
			serviceKind++
			continue
		}
		if strings.TrimSpace(section) == "" {
			blankSection++
			continue
		}
		seen[section+"->"+id]++
		if seen[section+"->"+id] > 1 {
			duplicate++
		}
	}

	for name, got := range map[string]int{
		"non-exact resolution": nonExact,
		"multi-candidate":      multiCandidate,
		"service target kind":  serviceKind,
		"blank section id":     blankSection,
		"duplicate pair":       duplicate,
	} {
		if got != 1 {
			t.Errorf("fixture exercises the %q exclusion %d time(s), want exactly 1", name, got)
		}
	}
}
