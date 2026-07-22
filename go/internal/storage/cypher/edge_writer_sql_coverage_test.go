// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "testing"

// TestSQLRelationshipMaterializedEdgeTypesMatchesWriteReasons proves the
// exported registry is derived from sqlRelationshipWriteReasons (the
// authoritative write-path whitelist), not a hand-maintained duplicate, and
// returns a defensive copy (#5330 Task 1).
func TestSQLRelationshipMaterializedEdgeTypesMatchesWriteReasons(t *testing.T) {
	t.Parallel()

	got := SQLRelationshipMaterializedEdgeTypes()
	if len(got) != len(sqlRelationshipWriteReasons) {
		t.Fatalf("len(got) = %d, want %d (must match sqlRelationshipWriteReasons exactly)", len(got), len(sqlRelationshipWriteReasons))
	}
	for edgeType, reason := range sqlRelationshipWriteReasons {
		if got[edgeType] != reason {
			t.Errorf("got[%q] = %q, want %q", edgeType, got[edgeType], reason)
		}
	}
	// INDEXES must be present now that the SQL edge writer accepts it.
	if _, ok := got["INDEXES"]; !ok {
		t.Error(`got["INDEXES"] missing, want present`)
	}
	for _, edgeType := range []string{"REFERENCES_TABLE", "WRITES_TO"} {
		if _, ok := got[edgeType]; !ok {
			t.Errorf("got[%q] missing, want present", edgeType)
		}
	}
	// Defensive copy: mutating the result must not corrupt the package state.
	got["INJECTED"] = "should not leak"
	again := SQLRelationshipMaterializedEdgeTypes()
	if _, leaked := again["INJECTED"]; leaked {
		t.Fatal("SQLRelationshipMaterializedEdgeTypes returned mutable backing storage")
	}
}
