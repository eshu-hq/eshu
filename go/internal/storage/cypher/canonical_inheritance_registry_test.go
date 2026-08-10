// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"sort"
	"strings"
	"testing"
)

// inheritanceRetractDisjunction is the relationship-type alternation the
// inheritance retract deletes, read back out of the built statement rather than
// copied as a literal.
//
// Copying the alternation here would defeat the point: the test would then pin
// this file against itself and keep passing after the retract changed.
func inheritanceRetractDisjunction(t *testing.T) string {
	t.Helper()
	stmts := buildInheritanceRetractStatements("scope_id", "scopes", []string{"scope-1"}, "parser/inheritance")
	if len(stmts) == 0 {
		t.Fatal("inheritance retract built no statements; a retract that deletes nothing would leave every edge stale")
	}
	const marker = "-[rel:"
	idx := strings.Index(stmts[0].Cypher, marker)
	if idx < 0 {
		t.Fatalf("inheritance retract statement has no [rel:...] alternation: %s", stmts[0].Cypher)
	}
	rest := stmts[0].Cypher[idx+len(marker):]
	end := strings.Index(rest, "]")
	if end < 0 {
		t.Fatalf("inheritance retract statement has an unterminated [rel:...]: %s", stmts[0].Cypher)
	}
	return rest[:end]
}

// TestInheritanceRegistryMatchesRetractDisjunction binds
// inheritanceMaterializedEdgeTypes to what the retract actually deletes (#5996).
//
// The write side spans two files (INHERITS/OVERRIDES/ALIASES here, IMPLEMENTS in
// canonical_implements_edges.go) while the retract names all four in one
// alternation. Nothing else stops those from drifting, and drift is silent in
// both directions: a written-but-not-retracted type survives reprojection as a
// stale edge, and a retracted type missing from this registry is one the Ifá
// baseline gate stops asserting while still calling inheritance_edges exhaustive.
func TestInheritanceRegistryMatchesRetractDisjunction(t *testing.T) {
	t.Parallel()

	retractSet := map[string]struct{}{}
	for _, relType := range strings.Split(inheritanceRetractDisjunction(t), "|") {
		relType = strings.TrimSpace(relType)
		if relType == "" {
			t.Fatal("inheritance retract alternation contains an empty alternative; a stray pipe deletes nothing and reads as coverage")
		}
		retractSet[relType] = struct{}{}
	}

	registry := InheritanceMaterializedEdgeTypes()
	var missingFromRegistry, missingFromRetract []string
	for relType := range retractSet {
		if _, ok := registry[relType]; !ok {
			missingFromRegistry = append(missingFromRegistry, relType)
		}
	}
	for relType := range registry {
		if _, ok := retractSet[relType]; !ok {
			missingFromRetract = append(missingFromRetract, relType)
		}
	}
	sort.Strings(missingFromRegistry)
	sort.Strings(missingFromRetract)

	if len(missingFromRegistry) > 0 {
		t.Errorf("retract deletes %v but InheritanceMaterializedEdgeTypes() omits them; the Ifá baseline gate would ignore those edges while calling inheritance_edges exhaustive",
			missingFromRegistry)
	}
	if len(missingFromRetract) > 0 {
		t.Errorf("InheritanceMaterializedEdgeTypes() names %v but the retract does not delete them; those edges would survive every reprojection as stale truth",
			missingFromRetract)
	}
}

// TestInheritanceRegistryIsACopy proves the accessor hands out a copy, so a
// caller cannot shrink what the live gate asserts for the rest of the process.
func TestInheritanceRegistryIsACopy(t *testing.T) {
	t.Parallel()

	first := InheritanceMaterializedEdgeTypes()
	if len(first) == 0 {
		t.Fatal("registry is empty; an empty type set makes any graph vacuously pass the live gate")
	}
	delete(first, "INHERITS")

	if _, ok := InheritanceMaterializedEdgeTypes()["INHERITS"]; !ok {
		t.Error("deleting from a returned map mutated the package registry; the accessor must return a copy")
	}
}
