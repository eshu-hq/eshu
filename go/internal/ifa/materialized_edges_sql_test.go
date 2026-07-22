// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
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
// reducer.ExtractSQLRelationshipRows seam — no store, no graph backend, no
// Docker — must reproduce the hand-derived expected edge set EXACTLY: same
// count, same (relationship_type, source_entity_id, target_entity_id)
// triples, neither more nor fewer. A reducer regression that silently drops
// (or duplicates) one of the seven materialized types changes
// ExtractSQLRelationshipRows's output and this test catches it without any
// backend.
func TestSQLRelationshipPureDerivationMatchesExpectedEdgesExactly(t *testing.T) {
	t.Parallel()

	odu, ok := CatalogByName()[sqlFamilyOduName]
	if !ok {
		t.Fatalf("catalog has no %q Odù", sqlFamilyOduName)
	}

	expected, err := loadSQLRelationshipExpectedEdges(sqlFamilyExpectedEdgesPath(repoRootDir(t)))
	if err != nil {
		t.Fatalf("loadSQLRelationshipExpectedEdges: %v", err)
	}
	expectedSet := sqlRelationshipEdgeSet(expected)

	_, rows, stats := reducer.ExtractSQLRelationshipRows(odu.Facts)
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

	odu, ok := CatalogByName()[sqlFamilyDeltaOduName]
	if !ok {
		t.Fatalf("catalog has no %q Odù", sqlFamilyDeltaOduName)
	}

	expected, err := loadSQLRelationshipExpectedEdges(sqlFamilyDeltaExpectedEdgesPath(repoRootDir(t)))
	if err != nil {
		t.Fatalf("loadSQLRelationshipExpectedEdges: %v", err)
	}
	expectedSet := sqlRelationshipEdgeSet(expected)

	_, rows, _ := reducer.ExtractSQLRelationshipRows(odu.Facts)
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
	// the delta-retract path it exists for.
	if _, ok := actualSet["INDEXES|content-entity:sql-idx-users-email|content-entity:sql-tbl-orders"]; !ok {
		t.Error("delta derivation did not retarget INDEXES to content-entity:sql-tbl-orders; the delta-retract teeth is not firing")
	}
	if _, ok := actualSet["INDEXES|content-entity:sql-idx-users-email|content-entity:sql-tbl-users"]; ok {
		t.Error("delta derivation still carries the stale gen-1 INDEXES->public.users edge; retarget did not take effect")
	}
}
