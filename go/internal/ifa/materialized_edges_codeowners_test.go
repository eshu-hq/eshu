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

// TestCodeownersOwnershipDomainResolvesItsMaterializedEdgeTypes pins the
// registry entry the live baseline gate needs for this family (#5992).
//
// MaterializedEdgeDomainEdgeTypes is what `eshu-ifa assert-edges -domain`
// reads to learn which edge types a family may have materialized. Without an
// entry the command errors, so the family can never be proven on
// ifa-determinism no matter how complete its Odù, guard, or expected-edge set
// is. This is one type, DECLARES_CODEOWNER -- unlike code_calls/
// inheritance_edges' multi-template undercounting trap, codeowners has only
// one writer template, but the test still pins the set explicitly so a
// second codeowners edge type added later is caught by
// TestEverySingleTypeFamilyResolvesThroughTheResolver's registry-derived
// check rather than silently under-asserted here.
func TestCodeownersOwnershipDomainResolvesItsMaterializedEdgeTypes(t *testing.T) {
	t.Parallel()

	got, err := MaterializedEdgeDomainEdgeTypes("codeowners_ownership_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(codeowners_ownership_edges) errored: %v; the live baseline gate cannot assert a family it cannot resolve", err)
	}
	want := []string{"DECLARES_CODEOWNER"}
	if diff := compareEdgeTypeSet(want, got); diff != "" {
		t.Errorf("codeowners_ownership_edges edge types: %s", diff)
	}
}

// TestCodeownersOwnershipDomainEdgeTypesComeFromTheWriterRegistry proves the
// set is registry-derived rather than hand-listed in the ifa package.
func TestCodeownersOwnershipDomainEdgeTypesComeFromTheWriterRegistry(t *testing.T) {
	t.Parallel()

	reg, ok := cypher.SingleTypeMaterializedEdgeTypes("codeowners_ownership_edges")
	if !ok {
		t.Fatal("cypher.SingleTypeMaterializedEdgeTypes(codeowners_ownership_edges) reports not-found; the single-type registry entry is missing")
	}
	if len(reg) == 0 {
		t.Fatal("cypher single-type registry entry for codeowners_ownership_edges is empty; an empty registry makes any graph vacuously pass the live gate")
	}
	got, err := MaterializedEdgeDomainEdgeTypes("codeowners_ownership_edges")
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

// TestCodeownersFamilyCassetteDerivesTheExpectedEdgeSet is the offline
// vacuity guard for #5992: the production extractor, over the committed
// cassette, reproduces the hand-derived expected set EXACTLY.
//
// This is deliberately NOT called coverage. It proves the extractor, not the
// gate: unlike code_calls/sql_relationships/documentation_edges, both
// DECLARES_CODEOWNER endpoints are MERGEd inline by the write template (no
// MATCH against a File/Function node), so this family carries none of the
// #5351 silent-inner-join-no-op risk -- but the live ifa-determinism
// assertion is still what proves the endpoints resolve to real, correctly
// identified graph nodes end to end (see the CodeownerTeam endpoint-identity
// gap this package's cmd/ifa sibling test documents).
//
// Derivation record for ifa-codeowners-family-expected-edges.json. The
// shared loader models a "note" field, so this prose COULD live in the JSON;
// it is kept here instead, mirroring the documentation_edges precedent
// (commit 48d3bdf6a / #5994), because a Go doc comment can name the symbols
// it describes and moves with them under review, while a JSON string cannot
// and silently rots. Five DECLARES_CODEOWNER edges from
// ExtractCodeownersOwnershipEdgeRowsWithQuarantine over the
// ifa-codeowners-family cassette: RULE A (*.md), RULE C's second owner token
// (*.go), and RULE B (docs/**) are three DISTINCT relationships from
// repo-ifa-codeowners-family to the SAME @org/docs team -- canonical_codeowners_edges.go's
// write template keys the relationship MERGE on {pattern, source_path}, not
// on the (repo, team) pair alone, so one team named by several rule patterns
// is several edges, not one overwritten edge. RULE C's first owner token
// (*.go -> @org/backend) is a fourth edge. RULE E (*.py under the second
// CODEOWNERS file docs/CODEOWNERS) is the fifth, to @org/infra. RULE D
// deliberately produces NO edge: it repeats RULE A's exact (repo_id,
// source_path, pattern, owner_ref) key at order_index 5, and the
// extractor's rowIndexByKey dedup collapses it into RULE A's row (keeping
// the higher order_index) rather than emitting a second edge -- proving the
// last-match-wins contract, not just adding a sixth entry here. Each edge's
// identity object carries pattern/source_path, the two properties
// cypher.MaterializedEdgeIdentityProperties declares for this family, so
// both this pure guard and the live assert-edges verb prove per-rule
// property truth rather than only the edge count between a (repo, team)
// pair -- see TestExpectedEdgeKeyDistinguishesCrossWiredCodeownersRules and
// TestExpectedEdgeKeyDistinguishesADroppedRuleFromAnUnrelatedDuplicate for
// the two proofs of what that identity buys. The fixture's top-level "odu"
// field is NOT validated against Catalog()/CatalogByName() or anything else
// -- ifa.LoadExpectedEdges decodes it and never reads it back -- so
// "odu:ifa-codeowners-family" here is a human label, not a resolvable
// identity.
func TestCodeownersFamilyCassetteDerivesTheExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := loadCodeownersFamilyOdu(codeownersFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadCodeownersFamilyOdu: %v", err)
	}

	expectedEdges, err := LoadExpectedEdges(codeownersFamilyExpectedEdgesPath(repoRoot), codeownersOwnershipFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}
	if len(expectedEdges) == 0 {
		t.Fatal("expected-edge set is empty; an empty expectation makes this guard vacuous")
	}

	generationID := codeownersOwnershipFamilyGenerationID(odu.Facts)
	rows, quarantined, err := reducer.ExtractCodeownersOwnershipEdgeRowsWithQuarantine(odu.Facts, generationID)
	if err != nil {
		t.Fatalf("ExtractCodeownersOwnershipEdgeRowsWithQuarantine: %v", err)
	}
	if len(quarantined) > 0 {
		t.Fatalf("%d fact(s) quarantined; the cassette no longer validates against the codeowners.ownership contract", len(quarantined))
	}

	actual := map[string]int{}
	for _, edge := range codeownersOwnershipRowsToExpectedEdges(rows) {
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
		t.Errorf("extractor produced %d row(s) for %d hand-derived edge(s) — RULE D's last-match-wins dedup should keep the row count equal to the expected edge count, not merely the same key set", len(rows), len(expectedEdges))
	}
	if len(missing) == 0 && len(extra) == 0 {
		t.Logf("codeowners_ownership_edges: %d rows reproduce the expected %d-edge set exactly", len(rows), len(expectedEdges))
	}
}

// TestCodeownersFamilyIsCatalogedAndResolvable pins the production coverage
// seam, not just the extractor helper used by the test above. A manifest row
// cannot honestly resolve unless the installed binary carries the Odù and the
// materialized-edge resolver dispatches through the family's own exact guard.
//
// This test is EXPECTED RED until two shared files (out of #5992's scope this
// phase, owned by the #5543 umbrella coordinator) are spliced:
//  1. catalog_seed.go's catalogSeed slice must add codeownersFamilyOdu().
//  2. materialized_edges.go's Resolve switch must add
//     `case "codeowners_ownership_edges":` dispatching to
//     resolveCodeownersOwnershipMaterializedEdges(odu,
//     codeownersFamilyExpectedEdgesPath(r.RepoRoot)).
//
// Both are documented as the exact one-line splices needed; this test proves
// the family-specific half is ready for them.
func TestCodeownersFamilyIsCatalogedAndResolvable(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	catalog := CatalogByName()
	compiled, ok := catalog[codeownersFamilyOduName]
	if !ok {
		t.Fatalf("CatalogByName omits %q -- catalog_seed.go needs `codeownersFamilyOdu(),` spliced into catalogSeed (out of #5992's scope this phase; see this test's doc comment)", codeownersFamilyOduName)
	}
	fromCassette, err := loadCodeownersFamilyOdu(codeownersFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadCodeownersFamilyOdu: %v", err)
	}
	if !reflect.DeepEqual(compiled, fromCassette) {
		t.Fatalf("compiled catalog Odù drifted from strict cassette projection\ncompiled: %#v\ncassette: %#v", compiled, fromCassette)
	}

	ok2, detail := (MaterializedEdgeOduResolver{Catalog: catalog, RepoRoot: repoRoot}).Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + "codeowners_ownership_edges",
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          codeownersFamilyOduName,
	})
	if !ok2 {
		t.Fatalf("codeowners_ownership_edges resolver rejected the cataloged Odù: %s -- materialized_edges.go needs a `case \"codeowners_ownership_edges\":` arm spliced into Resolve (out of #5992's scope this phase; see this test's doc comment)", detail)
	}
	if !strings.Contains(detail, "expected 5-edge set exactly") {
		t.Fatalf("resolver detail = %q, want the exact edge count", detail)
	}
}

// TestCodeownersFamilyOduBuildsFiveRuleFacts exercises codeownersFamilyOdu
// directly (not only through CatalogByName(), which cannot see it until
// catalog_seed.go splices it in), so this family-specific stack is fully
// proven independent of that shared-file dependency.
func TestCodeownersFamilyOduBuildsFiveRuleFacts(t *testing.T) {
	t.Parallel()
	catalogOdu := codeownersFamilyOdu()
	if catalogOdu.Odu.Name != codeownersFamilyOduName {
		t.Fatalf("codeownersFamilyOdu().Odu.Name = %q, want %q", catalogOdu.Odu.Name, codeownersFamilyOduName)
	}
	// 1 repository fact + 5 codeowners.ownership rule facts (RULE A-E).
	if len(catalogOdu.Odu.Facts) != 6 {
		t.Fatalf("codeownersFamilyOdu() carries %d facts, want 6 (1 repository + 5 rules)", len(catalogOdu.Odu.Facts))
	}
}

// TestResolveCodeownersOwnershipMaterializedEdgesAcceptsTheCompiledOdu calls
// the guard function itself (resolveCodeownersOwnershipMaterializedEdges),
// not just the parallel logic TestCodeownersFamilyCassetteDerivesTheExpectedEdgeSet
// hand-rolls, against the compiled catalog Odù directly -- proving the guard
// entry point works independent of the catalog_seed.go/materialized_edges.go
// splices this test's cataloged/resolvable sibling depends on.
func TestResolveCodeownersOwnershipMaterializedEdgesAcceptsTheCompiledOdu(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	ok, detail := resolveCodeownersOwnershipMaterializedEdges(codeownersFamilyOdu().Odu, codeownersFamilyExpectedEdgesPath(repoRoot))
	if !ok {
		t.Fatalf("resolveCodeownersOwnershipMaterializedEdges(compiled Odù) = false: %s", detail)
	}
	if !strings.Contains(detail, "expected 5-edge set exactly") {
		t.Fatalf("detail = %q, want the exact edge count", detail)
	}
}

// TestMissingCodeownersOwnershipExpectedTypesCatchesAnUncoveredRegistryType
// proves the registry-exhaustiveness half fails closed: an expected-edge set
// naming zero edges of a registry type must be rejected, not silently pass.
func TestMissingCodeownersOwnershipExpectedTypesCatchesAnUncoveredRegistryType(t *testing.T) {
	t.Parallel()
	missing := missingCodeownersOwnershipExpectedTypes(nil, map[string]struct{}{"DECLARES_CODEOWNER": {}})
	if len(missing) != 1 || missing[0] != "DECLARES_CODEOWNER" {
		t.Fatalf("missingCodeownersOwnershipExpectedTypes(no edges, {DECLARES_CODEOWNER}) = %v, want [DECLARES_CODEOWNER]", missing)
	}
}

// TestResolveCodeownersOwnershipMaterializedEdgesReturnsZeroRowsFalse proves
// the guard's own non-vacuity assertion beyond the edge-set comparison: a
// production extractor that returns zero rows must fail the guard outright,
// never be silently compared against an empty actual set (which a "MISSING"
// list alone would already catch, but this makes the liveness check
// explicit and independently testable, mirroring documentation_edges'
// zero-quarantine and code_calls' non-empty-repository-scope guard-specific
// assertions).
func TestResolveCodeownersOwnershipMaterializedEdgesReturnsZeroRowsFalse(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	// An Odù with a repository fact but NO codeowners.ownership facts: the
	// extractor returns zero rows even though decoding succeeds cleanly.
	emptyOdu := Odu{Name: "odu:ifa-codeowners-empty-probe", Facts: []facts.Envelope{codeownersFamilyRepositoryFact()}}
	ok, detail := resolveCodeownersOwnershipMaterializedEdges(emptyOdu, codeownersFamilyExpectedEdgesPath(repoRoot))
	if ok {
		t.Fatal("resolveCodeownersOwnershipMaterializedEdges(zero codeowners.ownership facts) = true, want false -- a zero-row extraction must fail the guard, not vacuously compare against an empty actual set")
	}
	if !strings.Contains(detail, "zero rows") {
		t.Fatalf("detail = %q, want it to name the zero-row liveness failure", detail)
	}
}

// TestCodeownersOwnershipFamilyGuardDetectsPropertyCorruption proves the
// guard's comparison, not just ExpectedEdge.Key() in isolation, catches a
// materialization bug that drops one rule and duplicates another between the
// same (repo, team) pair. A triple-only key nets those two independent
// defects to the same multiset; the declared pattern/source_path identity
// keeps them apart.
func TestCodeownersOwnershipFamilyGuardDetectsPropertyCorruption(t *testing.T) {
	t.Parallel()

	correctA := ExpectedEdge{
		RelationshipType: "DECLARES_CODEOWNER", SourceEntityID: "repo-x", TargetEntityID: "org/docs",
		Identity: map[string]string{"pattern": "*.md", "source_path": ".github/CODEOWNERS"},
	}
	correctB := ExpectedEdge{
		RelationshipType: "DECLARES_CODEOWNER", SourceEntityID: "repo-x", TargetEntityID: "org/docs",
		Identity: map[string]string{"pattern": "docs/**", "source_path": ".github/CODEOWNERS"},
	}
	// The corrupted actual set: RULE A's edge is simply missing (its pattern
	// went unwritten), and RULE B's edge was written TWICE instead.
	corrupted := []ExpectedEdge{correctB, correctB}

	mismatch := compareCodeownersOwnershipExpectedEdges("odu:probe", []ExpectedEdge{correctA, correctB}, corrupted)
	if mismatch == "" {
		t.Fatal("compareCodeownersOwnershipExpectedEdges did not catch the property-corrupted set; this family's whole point is detecting what the shared ExpectedEdge mechanism cannot")
	}
	if !strings.Contains(mismatch, "*.md") {
		t.Errorf("mismatch %q does not name the missing *.md rule", mismatch)
	}
	t.Logf("confirmed: the property-aware guard catches a dropped rule masked by an unrelated duplicate: %s", mismatch)
}

// TestResolveCodeownersOwnershipMaterializedEdgesRejectsMalformedFixtures
// proves this family reads its expected-edge fixture through the SHARED
// strict loader (ifa.LoadExpectedEdges) rather than a family-local
// encoding/json.Unmarshal. Both sub-cases load cleanly under a permissive
// decoder and are silently accepted, so each one is a fixture-typo class the
// family would otherwise ship without noticing.
func TestResolveCodeownersOwnershipMaterializedEdgesRejectsMalformedFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		fixture string
		want    string
	}{
		{
			name: "undeclared top-level field",
			fixture: `{
  "odu": "odu:ifa-codeowners-family",
  "descriptoin": "a typo'd key a permissive decoder drops on the floor",
  "edges": [{"relationship_type": "DECLARES_CODEOWNER", "source_entity_id": "r", "target_entity_id": "t", "identity": {"pattern": "*.md", "source_path": ".github/CODEOWNERS"}}]
}`,
			want: "descriptoin",
		},
		{
			name: "undeclared identity property",
			fixture: `{
  "odu": "odu:ifa-codeowners-family",
  "edges": [{"relationship_type": "DECLARES_CODEOWNER", "source_entity_id": "r", "target_entity_id": "t", "identity": {"pattern": "*.md", "source_path": ".github/CODEOWNERS", "order_index": "1"}}]
}`,
			want: "order_index",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "expected-edges.json")
			if err := os.WriteFile(path, []byte(tc.fixture), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			ok, detail := resolveCodeownersOwnershipMaterializedEdges(codeownersFamilyOdu().Odu, path)
			if ok {
				t.Fatalf("resolveCodeownersOwnershipMaterializedEdges(%s) = true; a malformed fixture must fail the guard, not load through a permissive decoder", tc.name)
			}
			if !strings.Contains(detail, tc.want) {
				t.Fatalf("detail = %q, want it to name %q", detail, tc.want)
			}
		})
	}
}
