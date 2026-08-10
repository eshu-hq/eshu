// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"sort"
	"strings"
	"testing"
)

// TestCodeCallRegistryMatchesRetractDisjunction binds codeCallMaterializedEdgeTypes
// to the retract's default relationship-type disjunction (#5991).
//
// The two lists are written in different places and different shapes — a map here,
// a pipe-delimited Cypher fragment in canonical_retract.go — and nothing but this
// test stops them from drifting. Drift is silent and asymmetric in a way that
// matters: a type written but not retracted survives every reprojection as a
// stale edge, and a type retracted but missing from this registry is one the Ifá
// live baseline gate stops asserting while still calling the family exhaustive.
//
// The retract string's ORDER is deliberately not asserted. A relationship-type
// disjunction is a set, and edge_writer_test.go already pins the "CALLS|REFERENCES"
// prefix as a literal substring; sorting the disjunction to match this map's
// iteration order would break that test for no semantic gain.
func TestCodeCallRegistryMatchesRetractDisjunction(t *testing.T) {
	t.Parallel()

	// The default arm is the whole-domain case: the evidence-source-scoped arms
	// are deliberate subsets (parser/code-calls omits USES_METACLASS), so only
	// the default names everything the domain can write.
	disjunction := codeCallRetractRelTypes("")
	retractSet := map[string]struct{}{}
	for _, relType := range strings.Split(disjunction, "|") {
		relType = strings.TrimSpace(relType)
		if relType == "" {
			t.Fatalf("retract disjunction %q contains an empty alternative; a stray pipe would delete nothing and read as coverage", disjunction)
		}
		retractSet[relType] = struct{}{}
	}

	registry := CodeCallMaterializedEdgeTypes()
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
		t.Errorf("retract deletes %v but CodeCallMaterializedEdgeTypes() omits them; the Ifá baseline gate would ignore those edges while calling code_calls exhaustive",
			missingFromRegistry)
	}
	if len(missingFromRetract) > 0 {
		t.Errorf("CodeCallMaterializedEdgeTypes() names %v but the default retract disjunction does not delete them; those edges would survive every reprojection as stale truth",
			missingFromRetract)
	}
}

// TestCodeCallRegistryIsACopy proves the accessor cannot be used to mutate the
// package's source of truth.
//
// It returns a map, and Go maps are reference types: handing out the package
// variable directly would let any caller — including a test running in parallel
// — delete an edge type and silently shrink what the live gate asserts for every
// later caller in the process.
func TestCodeCallRegistryIsACopy(t *testing.T) {
	t.Parallel()

	first := CodeCallMaterializedEdgeTypes()
	if len(first) == 0 {
		t.Fatal("registry is empty; an empty type set makes any graph vacuously pass the live gate")
	}
	delete(first, "CALLS")

	second := CodeCallMaterializedEdgeTypes()
	if _, ok := second["CALLS"]; !ok {
		t.Error("deleting from a returned map mutated the package registry; the accessor must return a copy")
	}
}
