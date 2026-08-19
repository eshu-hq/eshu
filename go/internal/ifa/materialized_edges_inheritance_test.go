// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestInheritanceFamilyOduResolvesItsExpectedEdgeSet proves the EXTRACTOR:
// reducer.ExtractInheritanceRows, over the cataloged Odù, reproduces the
// hand-derived edge set exactly (#5996).
//
// Deliberately not called coverage: this cannot catch a live writer break by
// itself. It is the extractor half; the manifest's live rows bind the same
// fixture to the backend assertions.
func TestInheritanceFamilyOduResolvesItsExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := CatalogByName()[inheritanceFamilyOduName]
	if odu.Name == "" {
		t.Fatalf("Odù %q is not in the catalog", inheritanceFamilyOduName)
	}

	ok, detail := resolveInheritanceMaterializedEdges(odu, inheritanceFamilyExpectedEdgesPath(repoRoot))
	if !ok {
		t.Fatalf("inheritance family Odù does not resolve: %s", detail)
	}
	if !strings.Contains(detail, "expected 5-edge set exactly") || !strings.Contains(detail, "all 4 registry types") {
		t.Fatalf("detail = %q, want it to name the exact edge count and registry-type count it proved", detail)
	}
	t.Logf("%s", detail)
}

// TestInheritanceFamilyCoversAllFourEdgeTypes stops the fixture degrading
// into a one- or two-type proof. The family owns four types
// (INHERITS/OVERRIDES/ALIASES/IMPLEMENTS) and the whole point of #5996 is
// exhaustiveness: an expected set that quietly lost OVERRIDES would still
// pass an exact-set test — it would simply be exact about less.
func TestInheritanceFamilyCoversAllFourEdgeTypes(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	expectedEdges, err := LoadExpectedEdges(inheritanceFamilyExpectedEdgesPath(repoRoot), "inheritance_edges")
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}

	present := map[string]struct{}{}
	for _, e := range expectedEdges {
		present[e.RelationshipType] = struct{}{}
	}
	registered, err := MaterializedEdgeDomainEdgeTypes("inheritance_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(inheritance_edges): %v", err)
	}
	var uncovered []string
	for edgeType := range registered {
		if _, ok := present[edgeType]; !ok {
			uncovered = append(uncovered, edgeType)
		}
	}
	sort.Strings(uncovered)
	if len(uncovered) > 0 {
		t.Errorf("the expected-edge set exercises no %v edge; the family registers them, so the fixture proves exhaustiveness over less than the family owns", uncovered)
	}
}

// TestInheritanceFamilyResolvesThroughTheManifestResolver proves the family
// is reachable the way the gate reaches it — by surface name through
// MaterializedEdgeOduResolver. A guard nothing dispatches to is not coverage.
func TestInheritanceFamilyResolvesThroughTheManifestResolver(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	resolver := MaterializedEdgeOduResolver{Catalog: CatalogByName(), RepoRoot: repoRoot}

	ok, detail := resolver.Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + "inheritance_edges",
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          inheritanceFamilyOduName,
	})
	if !ok {
		t.Fatalf("resolver.Resolve for inheritance_edges: %s", detail)
	}
}

// TestInheritanceFamilyExpectedSetRejectsAnExtraEdge is the vacuity guard's
// teeth test in the direction a "contains all expected" subset check would
// miss: an edge nobody derived by hand must fail as loudly as a missing one,
// or the graph could grow silently.
func TestInheritanceFamilyExpectedSetRejectsAnExtraEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := CatalogByName()[inheritanceFamilyOduName]

	expected, err := LoadExpectedEdges(inheritanceFamilyExpectedEdgesPath(repoRoot), "inheritance_edges")
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	short := sqlRelationshipExpectedEdgesFile{
		Odu:   inheritanceFamilyOduName,
		Edges: toSQLRelationshipExpectedEdges(expected[:len(expected)-1]),
	}
	path := writeSQLRelationshipExpectedEdges(t, short)

	ok, detail := resolveInheritanceMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against a short expected set; an edge nobody derived went unreported")
	}
	if !strings.Contains(detail, "EXTRA") {
		t.Errorf("detail = %q, want it to name the EXTRA edge", detail)
	}
}

// TestInheritanceFamilyExpectedSetRejectsAMissingEdge is the other
// direction: an expectation naming an edge the extractor does not produce
// must fail, or the fixture could quietly outrun the code — this is also the
// vacuity check named in the issue: an empty (or short) expected file must
// not pass silently.
func TestInheritanceFamilyExpectedSetRejectsAMissingEdge(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu := CatalogByName()[inheritanceFamilyOduName]

	expected, err := LoadExpectedEdges(inheritanceFamilyExpectedEdgesPath(repoRoot), "inheritance_edges")
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	padded := sqlRelationshipExpectedEdgesFile{
		Odu: inheritanceFamilyOduName,
		Edges: append(toSQLRelationshipExpectedEdges(expected), sqlRelationshipExpectedEdge{
			RelationshipType: "INHERITS",
			SourceEntityID:   "content-entity:e_deadbeefdead",
			TargetEntityID:   "content-entity:e_beefdeadbeef",
		}),
	}
	path := writeSQLRelationshipExpectedEdges(t, padded)

	ok, detail := resolveInheritanceMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against an expectation the extractor does not satisfy")
	}
	if !strings.Contains(detail, "MISSING") {
		t.Errorf("detail = %q, want it to name the MISSING edge", detail)
	}
}

// TestInheritanceFamilyVacuityGuardRejectsAnEmptyExpectedSet is the direct
// vacuity check the issue asks for: an expected-edge file with zero edges
// must fail to load rather than silently pass every comparison.
func TestInheritanceFamilyVacuityGuardRejectsAnEmptyExpectedSet(t *testing.T) {
	t.Parallel()
	odu := CatalogByName()[inheritanceFamilyOduName]

	empty := sqlRelationshipExpectedEdgesFile{Odu: inheritanceFamilyOduName, Edges: nil}
	path := writeSQLRelationshipExpectedEdges(t, empty)

	ok, detail := resolveInheritanceMaterializedEdges(odu, path)
	if ok {
		t.Fatal("guard passed against an empty expected-edge set; the gate would be vacuous")
	}
	if detail == "" {
		t.Fatal("guard returned no detail explaining the empty-set rejection")
	}
}

// TestInheritanceCatalogMatchesReplayCassette keeps the compiled catalog Odù
// and live replay input structurally equal at the facts.Envelope boundary
// after normalizing transport fields that replay derives: FactID, fencing,
// observation time, and SourceRef.
func TestInheritanceCatalogMatchesReplayCassette(t *testing.T) {
	t.Parallel()
	path := filepath.Join(repoRootDir(t), inheritanceFamilyCassetteRelPath)
	source, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("cassette.NewSource: %v", err)
	}
	generation, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("cassette source Next: %v", err)
	}
	if !ok {
		t.Fatal("cassette source returned no generation")
	}

	cassetteFacts := make([]facts.Envelope, 0, generation.FactCount())
	for envelope := range generation.Facts {
		cassetteFacts = append(cassetteFacts, envelope)
	}
	compiled := CatalogByName()[inheritanceFamilyOduName]
	if compiled.Name == "" {
		t.Fatalf("CatalogByName omits %q", inheritanceFamilyOduName)
	}
	if len(cassetteFacts) != len(compiled.Facts) {
		t.Fatalf("cassette has %d facts, compiled catalog Odù has %d", len(cassetteFacts), len(compiled.Facts))
	}
	for i := range cassetteFacts {
		got := cassetteFacts[i]
		got.FactID = ""
		got.FencingToken = 0
		got.ObservedAt = time.Time{}
		got.SourceRef = facts.Ref{}
		if !reflect.DeepEqual(got, compiled.Facts[i]) {
			t.Errorf("fact %d drifted between replay cassette and catalog\ncassette: %#v\ncatalog: %#v", i, got, compiled.Facts[i])
		}
	}
	if _, ok, err := source.Next(context.Background()); err != nil || ok {
		t.Fatalf("cassette declares more than one generation: ok=%v err=%v", ok, err)
	}
}

// TestInheritanceFamilyOduHasAFileFactForEveryContentEntity is the offline
// half of the #5351 live-proof Gotcha 1 regression: every content_entity's
// relative_path must have a matching file fact, or the projector's inner-join
// node write (`MATCH (f:File {path: row.file_path}) MERGE (n:Label ...)`)
// silently drops every entity in the batch against a live backend.
func TestInheritanceFamilyOduHasAFileFactForEveryContentEntity(t *testing.T) {
	t.Parallel()
	odu := CatalogByName()[inheritanceFamilyOduName]

	filePaths := map[string]struct{}{}
	for _, env := range odu.Facts {
		if env.FactKind != "file" {
			continue
		}
		filePaths[anyToStringValue(env.Payload["relative_path"])] = struct{}{}
	}
	if len(filePaths) == 0 {
		t.Fatal("inheritance Odù carries no file facts")
	}

	contentEntities := 0
	for _, env := range odu.Facts {
		if env.FactKind != "content_entity" {
			continue
		}
		contentEntities++
		relPath := anyToStringValue(env.Payload["relative_path"])
		if relPath == "" {
			t.Errorf("content entity %q carries no relative_path", env.StableFactKey)
			continue
		}
		if _, ok := filePaths[relPath]; !ok {
			t.Errorf("content entity %q names relative_path %q with no matching file fact; its graph-node write would silently no-op", env.StableFactKey, relPath)
		}
	}
	if contentEntities != 9 {
		t.Fatalf("inheritance Odù carries %d content entities, want 9", contentEntities)
	}
}

// TestInheritanceFamilyOduCarriesTheFollowupFact pins the #5992-class
// regression this family is exposed to: without the shared_followup fact
// naming reducer_domain "inheritance_materialization", no build*ReducerIntent
// probe exists for this domain (production enqueues it exclusively through
// inheritanceMaterializationFactEnvelope,
// go/internal/collector/git_followup_facts.go:279-302), so a live replay of
// this cassette would build zero reducer intents regardless of how correct
// the content_entity facts are.
func TestInheritanceFamilyOduCarriesTheFollowupFact(t *testing.T) {
	t.Parallel()
	odu := CatalogByName()[inheritanceFamilyOduName]

	for _, env := range odu.Facts {
		if env.FactKind != "shared_followup" {
			continue
		}
		if got := anyToStringValue(env.Payload["reducer_domain"]); got != "inheritance_materialization" {
			t.Fatalf("shared_followup reducer_domain = %q, want inheritance_materialization", got)
		}
		if got := anyToStringValue(env.Payload["repo_id"]); got != inheritanceFamilyRepoID {
			t.Fatalf("shared_followup repo_id = %q, want %q", got, inheritanceFamilyRepoID)
		}
		return
	}
	t.Fatal("inheritance Odù carries no shared_followup fact; the live gate could not enqueue this domain's reducer work item")
}

// TestExtractInheritanceRowsProducesNoRowsWithoutTheOdu is a small direct
// sanity check on the production seam this whole guard depends on: nil input
// yields nil output rather than panicking or fabricating rows.
func TestExtractInheritanceRowsProducesNoRowsWithoutTheOdu(t *testing.T) {
	t.Parallel()
	repoIDs, rows := reducer.ExtractInheritanceRows(nil)
	if repoIDs != nil || rows != nil {
		t.Fatalf("ExtractInheritanceRows(nil) = (%v, %v), want (nil, nil)", repoIDs, rows)
	}
}

func toSQLRelationshipExpectedEdges(edges []ExpectedEdge) []sqlRelationshipExpectedEdge {
	out := make([]sqlRelationshipExpectedEdge, 0, len(edges))
	for _, e := range edges {
		out = append(out, sqlRelationshipExpectedEdge{
			RelationshipType: e.RelationshipType,
			SourceEntityID:   e.SourceEntityID,
			TargetEntityID:   e.TargetEntityID,
		})
	}
	return out
}

func writeSQLRelationshipExpectedEdges(t *testing.T, file sqlRelationshipExpectedEdgesFile) string {
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
