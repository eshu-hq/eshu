// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestDocumentationFamilyOduResolvesItsExpectedEdgeSet proves the EXTRACTOR:
// ExtractDocumentationEdgeRowsWithQuarantine, run over the CATALOGED (compiled)
// Odù, reproduces the hand-derived edge set exactly. Both dimensions are now
// live-proven and covered (#5994); see
// TestDocumentationFamilyCassetteDerivesTheExpectedEdgeSet
// (documentation_family_odu_test.go) for the same assertion run directly
// against the cassette-loaded Odù, and TestDocumentationFamilyIsCatalogedAndResolvable
// for the proof that the two are identical.
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
	if !strings.Contains(detail, "reproduces the expected 3-edge set exactly") {
		t.Errorf("detail = %q, want it to name the exact edge count it proved", detail)
	}
	t.Logf("%s", detail)
}

// TestDocumentationFamilyResolvesThroughTheManifestResolver proves the vacuity
// guard is reachable by surface name through MaterializedEdgeOduResolver, not
// only by calling it directly.
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

	expected, err := LoadExpectedEdges(documentationFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}

	// Drop one expected edge and write the short set to a temp fixture: the
	// extractor now produces an edge the expectation does not contain.
	path := writeDocumentationExpectedEdgesFixture(t, expected[:len(expected)-1])

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

	expected, err := LoadExpectedEdges(documentationFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	padded := append(append([]ExpectedEdge{}, expected...), ExpectedEdge{
		RelationshipType: "DOCUMENTS",
		SourceEntityID:   "docsection:doc-platform-guide|sec-overview",
		TargetEntityID:   "content-entity:e_258daea46e5b",
	})
	path := writeDocumentationExpectedEdgesFixture(t, padded)

	ok, detail := resolveDocumentationEdgeMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against an expectation the extractor does not satisfy")
	}
	if !strings.Contains(detail, "MISSING") {
		t.Errorf("detail = %q, want it to name the MISSING edge", detail)
	}
}

// writeDocumentationExpectedEdgesFixture writes edges in the SAME
// {odu, edges: [{relationship_type, source_entity_id, target_entity_id}]}
// shape LoadExpectedEdges reads, so these teeth tests exercise the identical
// fixture format the live gate's -expected flag consumes.
func writeDocumentationExpectedEdgesFixture(t *testing.T, edges []ExpectedEdge) string {
	t.Helper()
	type expectedEdgeJSON struct {
		RelationshipType string `json:"relationship_type"`
		SourceEntityID   string `json:"source_entity_id"`
		TargetEntityID   string `json:"target_entity_id"`
	}
	type expectedEdgesFileJSON struct {
		Odu   string             `json:"odu"`
		Edges []expectedEdgeJSON `json:"edges"`
	}
	jsonEdges := make([]expectedEdgeJSON, 0, len(edges))
	for _, e := range edges {
		jsonEdges = append(jsonEdges, expectedEdgeJSON(e))
	}
	raw, err := json.Marshal(expectedEdgesFileJSON{Odu: documentationFamilyOduName, Edges: jsonEdges})
	if err != nil {
		t.Fatalf("marshal expected-edges fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "documentation-expected-edges.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write expected-edges fixture: %v", err)
	}
	return path
}

// TestDocumentationFamilyOduExercisesEveryExclusion pins the cataloged Odù's
// negative cases.
//
// The Odù's value is that its eight mentions produce exactly three edges. If a
// later edit trims the fixture to the three happy-path mentions, every other
// test here still passes while the fixture stops proving the extractor
// EXCLUDES anything — the exclusions are the part most likely to regress
// silently. Filters to documentation_entity_mention facts first: the Odù now
// projects the full cassette (repository, file, content_entity,
// documentation_document, and shared_followup facts too), not only mentions.
func TestDocumentationFamilyOduExercisesEveryExclusion(t *testing.T) {
	t.Parallel()
	odu := CatalogByName()[documentationFamilyOduName]

	var mentions []facts.Envelope
	for _, env := range odu.Facts {
		if env.FactKind == facts.DocumentationEntityMentionFactKind {
			mentions = append(mentions, env)
		}
	}
	if got, want := len(mentions), 8; got != want {
		t.Fatalf("fixture carries %d mention(s), want %d; the exclusion cases are the proof, not padding", got, want)
	}

	var nonExact, multiCandidate, serviceKind, blankSection, duplicate int
	seen := map[string]int{}
	for _, env := range mentions {
		p := env.Payload
		status, _ := p["resolution_status"].(string)
		refs, _ := p["candidate_refs"].([]any)
		section, _ := p["section_id"].(string)
		if status != "exact" {
			nonExact++
			continue
		}
		if len(refs) != 1 {
			multiCandidate++
			continue
		}
		ref, _ := refs[0].(map[string]any)
		kind, _ := ref["kind"].(string)
		id, _ := ref["id"].(string)
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
