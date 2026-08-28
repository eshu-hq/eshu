// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"sort"
	"strings"
)

// Shared helpers for the DIRECT-materialization family vacuity guards (#6228).
//
// A direct family reaches the graph straight from its own reducer port to a
// go/internal/storage/cypher writer, with no shared-projection intent row in
// between (reducer.DirectMaterializedEdgeFamilies()). Structurally its guard is
// the same three steps every shared-projection guard already runs -- load the
// hand-derived expected-edge-set fixture, assert it covers every relationship
// type the family's writer registry accepts, then run the production extractor
// over the Odù's facts and compare EXACTLY -- so the two helpers below are the
// family-agnostic halves of that shape, written once rather than copied per
// family.
//
// They are deliberately NOT reused from the submodule-pin or symbol-runtime
// guards. Those helpers carry family-specific failure text (a submodule path,
// an intent-row count) that would be actively misleading in a direct family's
// diff, and the direct half will grow more families than either of those
// shapes covers.

// missingDirectFamilyExpectedTypes reports any registry-owned relationship type
// the expected-edge fixture never exercises.
//
// This is the fail-closed half of a single-type family's guard. Today each
// direct family below registers exactly one relationship type, so the check
// reads as "the fixture names that one type" -- but it is a real check, not a
// non-empty assertion: the day a family's writer gains a second type with no
// matching expected-set entry, the family goes red on that commit rather than
// continuing to report exhaustive while asserting half its surface. Registry
// growth outrunning a fixture is the silent direction, and it is the direction
// this catches.
func missingDirectFamilyExpectedTypes(expected []ExpectedEdge, registry map[string]struct{}) []string {
	present := make(map[string]struct{}, len(expected))
	for _, edge := range expected {
		present[edge.RelationshipType] = struct{}{}
	}
	missing := make([]string, 0, len(registry))
	for edgeType := range registry {
		if _, ok := present[edgeType]; !ok {
			missing = append(missing, edgeType)
		}
	}
	sort.Strings(missing)
	return missing
}

// compareDirectFamilyExpectedEdges reports the exact-set mismatch between a
// family's hand-derived expectation and what its production extractor actually
// produced, comparing by MULTIPLICITY rather than membership.
//
// Multiplicity is what makes a fan-out family provable. iam_instance_profile_role's
// fixture deliberately has one instance profile attached to two distinct roles,
// so a regression that collapsed a profile's role list to its first entry would
// leave a set-membership comparison green on the surviving edge. Counting
// catches it, and ExpectedEdge.Key()'s injective encoding is what makes the
// count trustworthy for endpoint ids that could otherwise collide.
//
// Both directions are reported. A MISSING key is an edge the fixture says the
// writer must produce and the extractor did not; an EXTRA key is an edge the
// extractor produced that no hand derivation sanctions -- the direction a
// fabricated endpoint or a dropped negative control shows up in.
func compareDirectFamilyExpectedEdges(oduName, family string, expected, actual []ExpectedEdge) string {
	want := make(map[string]int, len(expected))
	got := make(map[string]int, len(actual))
	for _, edge := range expected {
		want[edge.Key()]++
	}
	for _, edge := range actual {
		got[edge.Key()]++
	}
	var missing, extra []string
	for key, count := range want {
		if got[key] < count {
			missing = append(missing, key)
		}
	}
	for key, count := range got {
		if count > want[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"odù %q: %s edge set does not match the %d hand-derived edge(s); MISSING: %s; EXTRA: %s",
		oduName, family, len(expected), strings.Join(missing, ", "), strings.Join(extra, ", "),
	)
}

// loadDirectFamilyExpectedEdges loads a direct family's expected-edge fixture
// and rejects an empty one.
//
// An empty expected set would make every comparison below vacuously true: zero
// expected edges match zero produced edges, and the guard would report covered
// for a family whose extractor had stopped producing anything at all. That is
// the exact false green the #5589 waiver rows exist to prevent, so it fails
// here rather than being caught by the registry-coverage check downstream
// (which an empty set would also pass for a family whose registry it happened
// to name).
func loadDirectFamilyExpectedEdges(path, family, oduName string) ([]ExpectedEdge, map[string]struct{}, string) {
	expected, err := LoadExpectedEdges(path, family)
	if err != nil {
		return nil, nil, err.Error()
	}
	if len(expected) == 0 {
		return nil, nil, fmt.Sprintf("odù %q: %s expected edges %s declares no edges; an empty expected set would make the gate vacuous", oduName, family, path)
	}
	registry, err := MaterializedEdgeDomainEdgeTypes(family)
	if err != nil {
		return nil, nil, err.Error()
	}
	if missing := missingDirectFamilyExpectedTypes(expected, registry); len(missing) > 0 {
		return nil, nil, fmt.Sprintf("odù %q: expected-edge set does not cover all registry types, missing: %v", oduName, missing)
	}
	return expected, registry, ""
}
