// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialized

import (
	"fmt"
)

// materializedEdgeIdentityByFamily declares identity for the four
// reducer.MaterializedEdgeFamilies() entries NOT covered by
// singleTypeMaterializedEdgeFamilies: code_calls, sql_relationships, and
// inheritance_edges keep their own multi-type registries next to their
// writers (see the package doc comment in families.go), and repo_dependency shares
// the DEPENDS_ON type with workload_dependency. Three declare an explicit
// empty identity because their relationship MERGEs key on endpoint nodes alone.
// repo_dependency's RUNS_ON writer additionally keys on identity_key so its
// two production writers converge on one stored relationship identity.
// This matches the scope TestSingleTypeFamilyIdentityMatchesWriteCypher draws
// around singleTypeMaterializedEdgeFamilies: proving identity against a held
// write-Cypher const only works for a family with one write template in
// families.go; these four span several templates across several files, so their
// reasoning stays there too. (The retract-shape guard,
// TestMaterializedEdgeFamilyRegistryMatchesItsRetract, ranges over both the
// single-type and the multi-type direct tables.) The
// three empty declarations are backed globally, not per-family, by
// TestPropertyKeyedRelationshipMergesMatchKnownAllowList
// (materialized_edge_property_keyed_inventory_test.go), which scans this
// entire package's source for any property-keyed relationship MERGE and
// fails if an undeclared one appears. RUNS_ON is the sole property-keyed
// identity these four families own and is pinned by the exact registry and
// write-template tests.
var materializedEdgeIdentityByFamily = map[string]map[string][]string{
	"repo_dependency":   {"RUNS_ON": {"identity_key"}},
	"code_calls":        {},
	"sql_relationships": {},
	"inheritance_edges": {},
}

// MaterializedEdgeIdentityProperties returns a copy of the relationship
// property names that participate in family's edge MERGE identity, keyed by
// relationship type. A relationship type with no entry in the returned map
// (or an entry with zero properties) MERGEs on its endpoint nodes alone.
//
// An unregistered family fails closed with an error, mirroring
// MaterializedEdgeDomainEdgeTypes in go/internal/ifa rather than the nil-ok
// shape of MaterializedEdgeEndpointLabels: a family with no declared identity
// is a real, explicit "no relationship properties" entry, not the absence of
// one, so a lookup miss here is always a registration bug, never a valid
// steady state.
//
// The returned map (and each of its property slices) is a copy, mirroring
// SingleTypeMaterializedEdgeTypes's copy-on-read contract, so a caller cannot
// mutate the package-level tables through the returned value.
func MaterializedEdgeIdentityProperties(family string) (map[string][]string, error) {
	if entry, ok := singleTypeMaterializedEdgeFamilies[family]; ok {
		out := make(map[string][]string, len(entry.EdgeTypes))
		if len(entry.IdentityProperties) > 0 {
			for edgeType := range entry.EdgeTypes {
				propsCopy := make([]string, len(entry.IdentityProperties))
				copy(propsCopy, entry.IdentityProperties)
				out[edgeType] = propsCopy
			}
		}
		return out, nil
	}
	if entry, ok := multiTypeDirectMaterializedEdgeFamilies[family]; ok {
		out := make(map[string][]string, len(entry.EdgeTypes))
		if len(entry.IdentityProperties) > 0 {
			for edgeType := range entry.EdgeTypes {
				propsCopy := make([]string, len(entry.IdentityProperties))
				copy(propsCopy, entry.IdentityProperties)
				out[edgeType] = propsCopy
			}
		}
		return out, nil
	}
	if entry, ok := materializedEdgeIdentityByFamily[family]; ok {
		out := make(map[string][]string, len(entry))
		for edgeType, props := range entry {
			out[edgeType] = append([]string(nil), props...)
		}
		return out, nil
	}
	if _, ok := predicateScopedMaterializedEdgeFamilies[family]; ok {
		// A predicate-scoped family's types are an open prefixed vocabulary,
		// so no per-type map can be enumerated. Every writer MERGEs on
		// (source, type, target) with no relationship property map — pinned
		// against the held upsert consts by
		// TestPredicateScopedCloudResourceTemplatesCarryThePredicate — so the
		// explicit empty declaration is the identity contract: a type absent
		// from the returned map MERGEs on its endpoints alone.
		return map[string][]string{}, nil
	}
	return nil, fmt.Errorf("cypher: no materialized-edge identity registered for family %q; register it before proving it on a live gate", family)
}

// SingleTypeMaterializedEdgeFamilyNames returns the registered family names, so
// callers and tests can enumerate coverage without importing the table itself.
func SingleTypeMaterializedEdgeFamilyNames() []string {
	out := make([]string, 0, len(singleTypeMaterializedEdgeFamilies))
	for family := range singleTypeMaterializedEdgeFamilies {
		out = append(out, family)
	}
	return out
}

// SingleTypeMaterializedEdgeTypes returns the relationship types the named
// family materializes, keyed by type and mapped to a short reason.
//
// The second return is false for an unregistered family. Callers MUST fail
// closed on it: returning an empty set instead would make the live gate assert
// zero edges and pass any graph vacuously, which is precisely the false-green
// the #5543 waiver rows exist to prevent.
func SingleTypeMaterializedEdgeTypes(family string) (map[string]string, bool) {
	entry, ok := singleTypeMaterializedEdgeFamilies[family]
	if !ok {
		return nil, false
	}
	out := make(map[string]string, len(entry.EdgeTypes))
	for edgeType, reason := range entry.EdgeTypes {
		out[edgeType] = reason
	}
	return out, true
}
