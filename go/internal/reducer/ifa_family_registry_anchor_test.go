// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// checkIfaFamilyRegistryAnchorsAreUnique fails, naming both families, when
// two families declare the same IFA_FAMILY_ANCHOR. anchor is a Cypher MERGE
// substring: _ifa_generic_cell_failgraphwrite (ifa_fault_generic_cells.sh)
// matches it against the live Cypher statement stream to intercept and
// once-fault exactly one family's graph write, independent of which handler
// or wait_key produced the row (ifa_fault_generic_cells.sh's own comment:
// "Uniform across every blocker_kind ... no lock/wait blocker is needed").
// Two families sharing an anchor means the SAME once-fault trigger fires for
// BOTH -- one family's fail-graph-write cell would intercept the other
// family's write (or vice versa, non-deterministically, whichever MERGE the
// interceptor happens to see first) and the two cells would pass while
// proving nothing about the family whose anchor they thought they scoped.
// This is the pre-runtime form of the template-copy defect: copying a
// sibling family's row and forgetting to change IFA_FAMILY_ANCHOR to the new
// family's own edge-type MERGE template, caught here before it burns a live
// fault shard rather than after.
func checkIfaFamilyRegistryAnchorsAreUnique(anchors map[string]string) error {
	owner := make(map[string]string, len(anchors))
	families := make([]string, 0, len(anchors))
	for family := range anchors {
		families = append(families, family)
	}
	sort.Strings(families)

	for _, family := range families {
		anchor := anchors[family]
		if anchor == "" {
			continue
		}
		if prior, clash := owner[anchor]; clash {
			return fmt.Errorf("families %q and %q both declare IFA_FAMILY_ANCHOR=%q -- the once-fault fail-graph-write cell intercepts on this Cypher MERGE substring alone, so one family's cell would intercept the other's write and both would pass while proving nothing about the family whose scope they claim; give each family its own edge-type MERGE template", prior, family, anchor)
		}
		owner[anchor] = family
	}
	return nil
}

// TestIfaFamilyRegistryAnchorsAreUniqueCatchesCollision is the
// deliberate-break tooth: it proves checkIfaFamilyRegistryAnchorsAreUnique
// itself fires, and names both families, on a synthetic pair of rows sharing
// one anchor -- independent of file I/O, so it stays fast, deterministic, and
// exercised on every run regardless of whether the real registry ever
// actually collides.
func TestIfaFamilyRegistryAnchorsAreUniqueCatchesCollision(t *testing.T) {
	t.Parallel()

	synthetic := map[string]string{
		"handles_route": "MERGE (source)-[rel:HANDLES_ROUTE]->(target)",
		"runs_in":       "MERGE (source)-[rel:HANDLES_ROUTE]->(target)", // copied from handles_route and never changed
	}

	err := checkIfaFamilyRegistryAnchorsAreUnique(synthetic)
	if err == nil {
		t.Fatal("checkIfaFamilyRegistryAnchorsAreUnique(two families sharing one anchor) = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "handles_route") || !strings.Contains(err.Error(), "runs_in") {
		t.Errorf("error %q does not name both colliding families", err.Error())
	}
}

// TestIfaFamilyRegistryAnchorsAreUnique runs checkIfaFamilyRegistryAnchorsAreUnique
// against every family's real, live-parsed IFA_FAMILY_ANCHOR row -- never a
// hand-copied Go-side table of anchors (see parseIfaFamilyRegistryAnchors'
// own doc comment for why).
func TestIfaFamilyRegistryAnchorsAreUnique(t *testing.T) {
	t.Parallel()

	rowsDir := ifaFamilyRegistryRowsDir(t)
	anchors := parseIfaFamilyRegistryAnchors(t, rowsDir)
	if len(anchors) == 0 {
		t.Fatal("parsed zero IFA_FAMILY_ANCHOR rows -- registry format changed or the rows were emptied")
	}

	if err := checkIfaFamilyRegistryAnchorsAreUnique(anchors); err != nil {
		t.Fatal(err)
	}
}
