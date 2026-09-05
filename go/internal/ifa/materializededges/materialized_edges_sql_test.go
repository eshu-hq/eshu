// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/sqlrelationship"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestSQLRelationshipExpectedEdgesCoverEveryRegistryType is the exhaustiveness
// half of the #5351 vacuity guard: the hand-derived expected-edge-set file
// must name at least one edge of every relationship type
// cypher.SQLRelationshipMaterializedEdgeTypes() (the writer's own registry)
// accepts. A type missing from the expected set would mean an 8th writer
// type added later has nothing forcing the fixture to grow, silently
// defeating the exhaustiveness this gate exists to prove.
func TestSQLRelationshipExpectedEdgesCoverEveryRegistryType(t *testing.T) {
	t.Parallel()

	edges, err := loadSQLRelationshipExpectedEdges(sqlFamilyExpectedEdgesPath(repoRootDir(t)))
	if err != nil {
		t.Fatalf("loadSQLRelationshipExpectedEdges: %v", err)
	}

	seenTypes := make(map[string]int, len(edges))
	for _, e := range edges {
		seenTypes[e.RelationshipType]++
	}

	registry := cypher.SQLRelationshipMaterializedEdgeTypes()
	if len(registry) == 0 {
		t.Fatal("cypher.SQLRelationshipMaterializedEdgeTypes() returned no types; the registry itself is broken")
	}
	for edgeType := range registry {
		if seenTypes[edgeType] == 0 {
			t.Errorf("expected-edge-set file names no edge of registry type %q", edgeType)
		}
	}
}

// TestSQLRelationshipPureDerivationMatchesExpectedEdgesExactly is the #5351
// pure-derivation lockstep (FAILING-FIRST by design before the resolver
// existed): running odu:ifa-sql-family's own facts through the production
// sqlrelationship.ExtractSQLRelationshipRows seam — no store, no graph backend, no
// Docker — must reproduce the hand-derived expected edge set EXACTLY: same
// count, same (relationship_type, source_entity_id, target_entity_id)
// triples, neither more nor fewer. A reducer regression that silently drops
// (or duplicates) one of the seven materialized types changes
// ExtractSQLRelationshipRows's output and this test catches it without any
// backend.
func TestSQLRelationshipPureDerivationMatchesExpectedEdgesExactly(t *testing.T) {
	t.Parallel()

	odu, ok := ifa.CatalogByName()[sqlFamilyOduName]
	if !ok {
		t.Fatalf("catalog has no %q Odù", sqlFamilyOduName)
	}

	expected, err := loadSQLRelationshipExpectedEdges(sqlFamilyExpectedEdgesPath(repoRootDir(t)))
	if err != nil {
		t.Fatalf("loadSQLRelationshipExpectedEdges: %v", err)
	}
	expectedSet := sqlRelationshipEdgeSet(expected)

	_, rows, stats := sqlrelationship.ExtractSQLRelationshipRows(odu.Facts)
	actual := sqlRelationshipRowsToExpectedEdges(rows)
	actualSet := sqlRelationshipEdgeSet(actual)

	if stats.UnresolvedReadTargets != 0 || stats.AmbiguousReadTargets != 0 {
		t.Errorf("unexpected READS_FROM resolution stats: unresolved=%d ambiguous=%d", stats.UnresolvedReadTargets, stats.AmbiguousReadTargets)
	}
	if stats.UnresolvedMigrationTargets != 0 || stats.AmbiguousMigrationTargets != 0 {
		t.Errorf("unexpected MIGRATES resolution stats: unresolved=%d ambiguous=%d", stats.UnresolvedMigrationTargets, stats.AmbiguousMigrationTargets)
	}
	if stats.UnresolvedReferenceTargets != 0 || stats.AmbiguousReferenceTargets != 0 {
		t.Errorf("unexpected REFERENCES_TABLE resolution stats: unresolved=%d ambiguous=%d", stats.UnresolvedReferenceTargets, stats.AmbiguousReferenceTargets)
	}
	if stats.UnresolvedWriteTargets != 0 || stats.AmbiguousWriteTargets != 0 {
		t.Errorf("unexpected WRITES_TO resolution stats: unresolved=%d ambiguous=%d", stats.UnresolvedWriteTargets, stats.AmbiguousWriteTargets)
	}

	if len(actual) != len(expected) {
		t.Fatalf("ExtractSQLRelationshipRows produced %d edges, want %d (expected=%v actual=%v)", len(actual), len(expected), expected, actual)
	}
	for key := range expectedSet {
		if _, ok := actualSet[key]; !ok {
			t.Errorf("expected edge %s missing from ExtractSQLRelationshipRows output", key)
		}
	}
	for key := range actualSet {
		if _, ok := expectedSet[key]; !ok {
			t.Errorf("ExtractSQLRelationshipRows produced unexpected edge %s not in the hand-derived expected set", key)
		}
	}
}

// TestSQLRelationshipDeltaPureDerivationMatchesExpectedEdgesExactly is the
// gen-2 delta counterpart: the delta Odù's own facts (db/schema.sql only,
// INDEXES retargeted to public.orders) must reproduce the delta expected set
// exactly, proving the delta-retract input shape independent of any live
// backend.
func TestSQLRelationshipDeltaPureDerivationMatchesExpectedEdgesExactly(t *testing.T) {
	t.Parallel()

	odu, ok := ifa.CatalogByName()[sqlFamilyDeltaOduName]
	if !ok {
		t.Fatalf("catalog has no %q Odù", sqlFamilyDeltaOduName)
	}

	expected, err := loadSQLRelationshipExpectedEdges(sqlFamilyDeltaExpectedEdgesPath(repoRootDir(t)))
	if err != nil {
		t.Fatalf("loadSQLRelationshipExpectedEdges: %v", err)
	}
	expectedSet := sqlRelationshipEdgeSet(expected)

	_, rows, _ := sqlrelationship.ExtractSQLRelationshipRows(odu.Facts)
	actual := sqlRelationshipRowsToExpectedEdges(rows)
	actualSet := sqlRelationshipEdgeSet(actual)

	if len(actual) != len(expected) {
		t.Fatalf("delta ExtractSQLRelationshipRows produced %d edges, want %d (expected=%v actual=%v)", len(actual), len(expected), expected, actual)
	}
	for key := range expectedSet {
		if _, ok := actualSet[key]; !ok {
			t.Errorf("expected delta edge %s missing from ExtractSQLRelationshipRows output", key)
		}
	}
	for key := range actualSet {
		if _, ok := expectedSet[key]; !ok {
			t.Errorf("delta ExtractSQLRelationshipRows produced unexpected edge %s not in the hand-derived expected set", key)
		}
	}

	// The teeth: gen-2 retargets INDEXES from public.users to public.orders.
	// If this specific edge is missing, the fixture silently stopped proving
	// the delta-retract path it exists for. Keys built via sqlRelationshipEdgeKey
	// itself, not a hand-copied literal, so this stays correct if the key
	// encoding ever changes again.
	retargeted := sqlRelationshipEdgeKey("INDEXES", "content-entity:sql-idx-users-email", "content-entity:sql-tbl-orders")
	stale := sqlRelationshipEdgeKey("INDEXES", "content-entity:sql-idx-users-email", "content-entity:sql-tbl-users")
	if _, ok := actualSet[retargeted]; !ok {
		t.Error("delta derivation did not retarget INDEXES to content-entity:sql-tbl-orders; the delta-retract teeth is not firing")
	}
	if _, ok := actualSet[stale]; ok {
		t.Error("delta derivation still carries the stale gen-1 INDEXES->public.users edge; retarget did not take effect")
	}
}

// TestLoadSQLRelationshipExpectedEdgesRejectsTrailingJSON is the regression
// test for a real false-green vector: json.Decoder.Decode reads exactly one
// JSON value off the stream and stops, silently ignoring anything that
// follows -- unlike json.Unmarshal, which validates that the whole input is
// one complete value. A fixture holding a valid object immediately followed
// by a second, unrelated concatenated object decodes only the first and
// silently drops the second: authored edges vanish while this loader reports
// success and the exact-set gate runs (and may still pass) against a
// truncated expectation.
func TestLoadSQLRelationshipExpectedEdgesRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	first := `{"odu":"odu:x","note":"n","edges":[{"relationship_type":"QUERIES_TABLE","source_entity_id":"a","target_entity_id":"b"}]}`
	second := `{"odu":"odu:y","note":"n","edges":[{"relationship_type":"HAS_COLUMN","source_entity_id":"c","target_entity_id":"d"}]}`
	path := filepath.Join(t.TempDir(), "trailing.json")
	if err := os.WriteFile(path, []byte(first+second), 0o600); err != nil {
		t.Fatalf("write temp fixture: %v", err)
	}

	if _, err := loadSQLRelationshipExpectedEdges(path); err == nil {
		t.Fatal("loadSQLRelationshipExpectedEdges accepted a fixture with a second, concatenated JSON object trailing the first")
	}
}

// TestSQLRelationshipEdgeKeyIsInjective is the regression test for the same
// injectivity defect TestExpectedEdgeKeyIsInjective proves for ExpectedEdge.Key():
// sqlRelationshipEdgeKey joined its three components with a raw "|" and no
// escaping, so a source ending in "|X" and a target of "Y" rendered
// identically to a source of just "S" and a target of "X|Y" (once the
// relationship type and leading component are held fixed). This function is
// ExpectedEdge.Key()'s own delegate for the empty-Identity path AND
// sql_relationships' direct comparison key, so the defect here silently
// weakens the exact-set assertion for every family with no declared
// identity -- twelve of fourteen, including sql_relationships and
// documentation_edges, both already proven live.
func TestSQLRelationshipEdgeKeyIsInjective(t *testing.T) {
	t.Parallel()

	keyA := sqlRelationshipEdgeKey("QUERIES_TABLE", "S|X", "Y")
	keyB := sqlRelationshipEdgeKey("QUERIES_TABLE", "S", "X|Y")
	if keyA == keyB {
		t.Fatalf("distinct (source, target) pairs collided onto one key %q; the exact-set comparison would silently merge them", keyA)
	}
}
