// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestSubmodulePinDomainResolvesItsMaterializedEdgeTypes pins the registry
// entry the live baseline gate needs for this family (#6002).
//
// MaterializedEdgeDomainEdgeTypes is what `eshu-ifa assert-edges -domain`
// reads to learn which edge types a family may have materialized. Without an
// entry the command errors, so the family can never be proven on
// ifa-determinism no matter how complete its Odù, guard, or expected-edge set
// is. This is one type, PINS_SUBMODULE, but the test still pins the set
// explicitly so a second submodule_pin_edges edge type added later is caught
// by TestEverySingleTypeFamilyResolvesThroughTheResolver's registry-derived
// check rather than silently under-asserted here.
func TestSubmodulePinDomainResolvesItsMaterializedEdgeTypes(t *testing.T) {
	t.Parallel()

	got, err := MaterializedEdgeDomainEdgeTypes(submodulePinEdgesFamily)
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(%s) errored: %v; the live baseline gate cannot assert a family it cannot resolve", submodulePinEdgesFamily, err)
	}
	want := []string{"PINS_SUBMODULE"}
	if diff := compareEdgeTypeSet(want, got); diff != "" {
		t.Errorf("%s edge types: %s", submodulePinEdgesFamily, diff)
	}
}

// TestSubmodulePinDomainEdgeTypesComeFromTheWriterRegistry proves the set is
// registry-derived rather than hand-listed in the ifa package.
func TestSubmodulePinDomainEdgeTypesComeFromTheWriterRegistry(t *testing.T) {
	t.Parallel()

	reg, ok := cypher.SingleTypeMaterializedEdgeTypes(submodulePinEdgesFamily)
	if !ok {
		t.Fatalf("cypher.SingleTypeMaterializedEdgeTypes(%s) reports not-found; the single-type registry entry is missing", submodulePinEdgesFamily)
	}
	if len(reg) == 0 {
		t.Fatalf("cypher single-type registry entry for %s is empty; an empty registry makes any graph vacuously pass the live gate", submodulePinEdgesFamily)
	}
	got, err := MaterializedEdgeDomainEdgeTypes(submodulePinEdgesFamily)
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes: %v", err)
	}
	if len(got) != len(reg) {
		t.Fatalf("resolver returned %d edge type(s) but the writer registry has %d; the ifa set is not derived from the registry", len(got), len(reg))
	}
	for edgeType := range reg {
		if _, ok := got[edgeType]; !ok {
			t.Errorf("writer registry names %q but the resolver omits it", edgeType)
		}
	}
}

// TestSubmodulePinFamilyCassetteDerivesTheExpectedEdgeSet is the offline
// vacuity guard for #6002: the production extractor, over the committed
// cassette, reproduces the hand-derived expected set EXACTLY.
//
// This is deliberately NOT called coverage. It proves the extractor, not the
// gate: both PINS_SUBMODULE endpoints are MERGEd inline by the write
// template (no MATCH against a parsed-entity node), so this family carries
// none of the #5351 silent-inner-join-no-op risk -- but the live
// ifa-determinism assertion is still what proves the endpoints resolve to
// real, correctly identified graph nodes end to end.
//
// Derivation record for ifa-submodule-pin-family-expected-edges.json.
// Mirroring the documentation_edges/codeowners_ownership_edges precedent
// (commit 48d3bdf6a / #5994), this prose is kept in the Go doc comment
// rather than the JSON fixture's "note" field: a Go doc comment can name the
// symbols it describes and moves with them under review. Three
// PINS_SUBMODULE edges from ExtractSubmodulePinEdgeRowsWithQuarantine over
// the ifa-submodule-pin-family cassette: PIN A (vendor/libfoo) and PIN B
// (vendor/libfoo-mirror) are two DISTINCT relationships from
// repo-ifa-submodule-pin-family to the SAME
// repo-ifa-submodule-pin-target-foo repository --
// canonical_submodule_edges.go's write template keys the relationship MERGE
// on {path}, not on the (parent, target) pair alone, so one target reached by
// several paths is several edges, not one overwritten edge. PIN E
// (vendor/libbaz) is the third, to the distinct
// repo-ifa-submodule-pin-target-baz repository. PIN A-DUP deliberately
// produces NO additional edge: it repeats PIN A's exact (parent_repo_id,
// submodule_path) key, and the extractor's rowIndexByKey dedup collapses it
// into PIN A's row (keeping the later envelope's pinned_sha) rather than
// emitting a second edge -- proving the last-match-wins contract, not just
// adding a fourth entry here. PIN C (vendor/libunresolved) also produces NO
// edge: it carries a submodule_url but no resolved_repo_id, and the
// extractor must never guess a target Repository id for an unresolved
// submodule. Each edge's identity object carries path, the one property
// cypher.MaterializedEdgeIdentityProperties declares for this family, so
// both this pure guard and the live assert-edges verb prove per-path
// property truth rather than only the edge count between a (parent, target)
// pair. The fixture's top-level "odu" field is NOT validated against
// Catalog()/CatalogByName() or anything else -- ifa.LoadExpectedEdges decodes
// it and never reads it back -- so "odu:ifa-submodule-pin-family" here is a
// human label, not a resolvable identity.
func TestSubmodulePinFamilyCassetteDerivesTheExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := loadSubmodulePinFamilyOdu(submodulePinFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadSubmodulePinFamilyOdu: %v", err)
	}

	expectedEdges, err := LoadExpectedEdges(submodulePinFamilyExpectedEdgesPath(repoRoot), submodulePinEdgesFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	if len(expectedEdges) == 0 {
		t.Fatal("expected-edge set is empty; an empty expectation makes this guard vacuous")
	}

	generationID := submodulePinFamilyGenerationIDFromFacts(odu.Facts)
	rows, quarantined, err := reducer.ExtractSubmodulePinEdgeRowsWithQuarantine(odu.Facts, generationID)
	if err != nil {
		t.Fatalf("ExtractSubmodulePinEdgeRowsWithQuarantine: %v", err)
	}
	if len(quarantined) > 0 {
		t.Fatalf("%d fact(s) quarantined; the cassette no longer validates against the submodule.pin contract", len(quarantined))
	}

	actual := map[string]int{}
	for _, edge := range submodulePinRowsToExpectedEdges(rows) {
		actual[edge.Key()]++
	}
	expected := map[string]int{}
	for _, e := range expectedEdges {
		expected[e.Key()]++
	}

	var missing, extra []string
	for key, want := range expected {
		if actual[key] < want {
			missing = append(missing, key)
		}
	}
	for key, got := range actual {
		if got > expected[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("MISSING %d edge(s) the extractor no longer produces: %s", len(missing), strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Errorf("EXTRA %d edge(s) nobody derived: %s", len(extra), strings.Join(extra, ", "))
	}
	if len(rows) != len(expectedEdges) {
		t.Errorf("extractor produced %d row(s) for %d hand-derived edge(s) -- PIN A-DUP's last-match-wins dedup should keep the row count equal to the expected edge count, not merely the same key set", len(rows), len(expectedEdges))
	}
	if len(missing) == 0 && len(extra) == 0 {
		t.Logf("submodule_pin_edges: %d rows reproduce the expected %d-edge set exactly", len(rows), len(expectedEdges))
	}
}

// TestSubmodulePinFamilyIsCatalogedAndResolvable pins the production
// coverage seam, not just the extractor helper used by the test above. A
// manifest row cannot honestly resolve unless the installed binary carries
// the Odù and the materialized-edge resolver dispatches through the family's
// own exact guard.
//
// Two shared files carry the splices this depends on, both landed here:
// catalog_seed.go's catalogSeed slice adds submodulePinFamilyOdu(), and
// materialized_edges.go's Resolve switch adds the family's arm. They follow
// codeowners_ownership_edges exactly. Removing either is what this test
// catches.
func TestSubmodulePinFamilyIsCatalogedAndResolvable(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	catalog := CatalogByName()
	compiled, ok := catalog[submodulePinFamilyOduName]
	if !ok {
		t.Fatalf("CatalogByName omits %q -- restore `submodulePinFamilyOdu(),` in catalog_seed.go's catalogSeed; without it the installed binary cannot resolve this family's manifest row", submodulePinFamilyOduName)
	}
	fromCassette, err := loadSubmodulePinFamilyOdu(submodulePinFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadSubmodulePinFamilyOdu: %v", err)
	}
	if !reflect.DeepEqual(compiled, fromCassette) {
		t.Fatalf("compiled catalog Odù drifted from strict cassette projection\ncompiled: %#v\ncassette: %#v", compiled, fromCassette)
	}

	ok2, detail := (MaterializedEdgeOduResolver{Catalog: catalog, RepoRoot: repoRoot}).Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + submodulePinEdgesFamily,
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          submodulePinFamilyOduName,
	})
	if !ok2 {
		t.Fatalf("%s resolver rejected the cataloged Odù: %s -- restore the `case submodulePinEdgesFamily:` arm in materialized_edges.go's Resolve", submodulePinEdgesFamily, detail)
	}
	if !strings.Contains(detail, "expected 3-edge set exactly") {
		t.Fatalf("resolver detail = %q, want the exact edge count", detail)
	}
}

// TestSubmodulePinFamilyOduBuildsSixFacts exercises submodulePinFamilyOdu
// directly rather than through CatalogByName(), so a catalog_seed.go
// regression fails one test with a clear cause instead of taking the whole
// family-specific stack down with it.
func TestSubmodulePinFamilyOduBuildsSixFacts(t *testing.T) {
	t.Parallel()
	catalogOdu := submodulePinFamilyOdu()
	if catalogOdu.Odu.Name != submodulePinFamilyOduName {
		t.Fatalf("submodulePinFamilyOdu().Odu.Name = %q, want %q", catalogOdu.Odu.Name, submodulePinFamilyOduName)
	}
	// 1 repository fact + 5 submodule.pin facts (PIN A, B, A-DUP, E, C) + the
	// shared_followup fact that enqueues this family's reducer work item. The
	// follow-up is not decoration: without it the projector's fan-out never
	// creates a domain='submodule_pin' fact_work_items row, so the live gates
	// cannot drive the handler at all (mirroring codeowners_ownership_edges,
	// #5992). See submodulePinFamilySharedFollowupFact's doc comment.
	if len(catalogOdu.Odu.Facts) != 7 {
		t.Fatalf("submodulePinFamilyOdu() carries %d facts, want 7 (1 repository + 5 pins + 1 shared_followup)", len(catalogOdu.Odu.Facts))
	}
}

// TestResolveSubmodulePinMaterializedEdgesAcceptsTheCompiledOdu calls the
// guard function itself (resolveSubmodulePinMaterializedEdges), not just the
// parallel logic TestSubmodulePinFamilyCassetteDerivesTheExpectedEdgeSet
// hand-rolls, against the compiled catalog Odù directly -- proving the guard
// entry point works without going through the catalog_seed.go and
// materialized_edges.go wiring its cataloged/resolvable sibling exercises.
func TestResolveSubmodulePinMaterializedEdgesAcceptsTheCompiledOdu(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	ok, detail := resolveSubmodulePinMaterializedEdges(submodulePinFamilyOdu().Odu, submodulePinFamilyExpectedEdgesPath(repoRoot))
	if !ok {
		t.Fatalf("resolveSubmodulePinMaterializedEdges(compiled Odù) = false: %s", detail)
	}
	if !strings.Contains(detail, "expected 3-edge set exactly") {
		t.Fatalf("detail = %q, want the exact edge count", detail)
	}
}

// TestMissingSubmodulePinExpectedTypesCatchesAnUncoveredRegistryType proves
// the registry-exhaustiveness half fails closed: an expected-edge set naming
// zero edges of a registry type must be rejected, not silently pass.
func TestMissingSubmodulePinExpectedTypesCatchesAnUncoveredRegistryType(t *testing.T) {
	t.Parallel()
	missing := missingSubmodulePinExpectedTypes(nil, map[string]struct{}{"PINS_SUBMODULE": {}})
	if len(missing) != 1 || missing[0] != "PINS_SUBMODULE" {
		t.Fatalf("missingSubmodulePinExpectedTypes(no edges, {PINS_SUBMODULE}) = %v, want [PINS_SUBMODULE]", missing)
	}
}

// TestResolveSubmodulePinMaterializedEdgesReturnsZeroRowsFalse proves the
// guard's own non-vacuity assertion beyond the edge-set comparison: a
// production extractor that returns zero rows must fail the guard outright,
// never be silently compared against an empty actual set.
func TestResolveSubmodulePinMaterializedEdgesReturnsZeroRowsFalse(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	// An Odù with a repository fact but NO submodule.pin facts: the extractor
	// returns zero rows even though decoding succeeds cleanly.
	emptyOdu := Odu{
		Name: "odu:ifa-submodule-pin-empty-probe",
		Facts: []facts.Envelope{
			submodulePinFamilyRepositoryFact(submodulePinFamilyRepoID, "/repo-submodule-pin-parent"),
		},
	}
	ok, detail := resolveSubmodulePinMaterializedEdges(emptyOdu, submodulePinFamilyExpectedEdgesPath(repoRoot))
	if ok {
		t.Fatal("resolveSubmodulePinMaterializedEdges(zero submodule.pin facts) = true, want false -- a zero-row extraction must fail the guard, not vacuously compare against an empty actual set")
	}
	if !strings.Contains(detail, "zero rows") {
		t.Fatalf("detail = %q, want it to name the zero-row liveness failure", detail)
	}
}

// TestSubmodulePinFamilyGuardDetectsPropertyCorruption proves the guard's
// comparison, not just ExpectedEdge.Key() in isolation, catches a
// materialization bug that drops one path and duplicates another between the
// same (parent, target) pair. A pair-only key nets those two independent
// defects to the same multiset; the declared path identity keeps them apart.
func TestSubmodulePinFamilyGuardDetectsPropertyCorruption(t *testing.T) {
	t.Parallel()

	correctA := ExpectedEdge{
		RelationshipType: "PINS_SUBMODULE", SourceEntityID: "repo-x", TargetEntityID: "repo-target",
		Identity: map[string]string{"path": "vendor/libfoo"},
	}
	correctB := ExpectedEdge{
		RelationshipType: "PINS_SUBMODULE", SourceEntityID: "repo-x", TargetEntityID: "repo-target",
		Identity: map[string]string{"path": "vendor/libfoo-mirror"},
	}
	// The corrupted actual set: PIN A's edge is simply missing (its path went
	// unwritten), and PIN B's edge was written TWICE instead.
	corrupted := []ExpectedEdge{correctB, correctB}

	mismatch := compareSubmodulePinExpectedEdges("odu:probe", []ExpectedEdge{correctA, correctB}, corrupted)
	if mismatch == "" {
		t.Fatal("compareSubmodulePinExpectedEdges did not catch the property-corrupted set; this family's whole point is detecting what the shared ExpectedEdge mechanism cannot")
	}
	if !strings.Contains(mismatch, "vendor/libfoo") {
		t.Errorf("mismatch %q does not name the missing vendor/libfoo pin", mismatch)
	}
	t.Logf("confirmed: the property-aware guard catches a dropped path masked by an unrelated duplicate: %s", mismatch)
}

// TestResolveSubmodulePinMaterializedEdgesRejectsMalformedFixtures proves
// this family reads its expected-edge fixture through the SHARED strict
// loader (ifa.LoadExpectedEdges) rather than a family-local
// encoding/json.Unmarshal. Both sub-cases load cleanly under a permissive
// decoder and are silently accepted, so each one is a fixture-typo class the
// family would otherwise ship without noticing.
func TestResolveSubmodulePinMaterializedEdgesRejectsMalformedFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		fixture string
		want    string
	}{
		{
			name: "undeclared top-level field",
			fixture: `{
  "odu": "odu:ifa-submodule-pin-family",
  "descriptoin": "a typo'd key a permissive decoder drops on the floor",
  "edges": [{"relationship_type": "PINS_SUBMODULE", "source_entity_id": "r", "target_entity_id": "t", "identity": {"path": "vendor/lib"}}]
}`,
			want: "descriptoin",
		},
		{
			name: "undeclared identity property",
			fixture: `{
  "odu": "odu:ifa-submodule-pin-family",
  "edges": [{"relationship_type": "PINS_SUBMODULE", "source_entity_id": "r", "target_entity_id": "t", "identity": {"path": "vendor/lib", "pinned_sha": "deadbeef"}}]
}`,
			want: "pinned_sha",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "expected-edges.json")
			if err := os.WriteFile(path, []byte(tc.fixture), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			ok, detail := resolveSubmodulePinMaterializedEdges(submodulePinFamilyOdu().Odu, path)
			if ok {
				t.Fatalf("resolveSubmodulePinMaterializedEdges(%s) = true; a malformed fixture must fail the guard, not load through a permissive decoder", tc.name)
			}
			if !strings.Contains(detail, tc.want) {
				t.Fatalf("detail = %q, want it to name %q", detail, tc.want)
			}
		})
	}
}
