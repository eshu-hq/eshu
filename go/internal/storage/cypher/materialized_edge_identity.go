// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "fmt"

// materializedEdgeIdentityByFamily maps each Ifá-gated materialized-edge
// family (reducer.MaterializedEdgeFamilies) to the ordered set of
// relationship properties that participate in that family's edge MERGE
// identity, keyed by relationship type.
//
// The organizing principle: a family's asserted edge identity IS its
// writer's relationship MERGE key, nothing more and nothing less. Most
// families MERGE their relationship on the two endpoint node patterns alone
// (`MERGE (a)-[rel:TYPE]->(b)`), so their entry here is an explicit empty
// map — the family owns no relationship-property identity component.
// codeowners_ownership_edges and submodule_pin_edges are the two exceptions:
// their writers fold a relationship property into the MERGE key itself
// (canonical_codeowners_edges.go, canonical_submodule_edges.go) because two
// distinct source rows can otherwise collide onto the same (source, target)
// relationship pattern and silently overwrite each other.
//
// Every reducer.MaterializedEdgeFamilies() entry MUST have an explicit entry
// here, including an empty one — TestMaterializedEdgeIdentityByFamilyIsTotal
// enforces this so a newly enumerated family cannot land with no identity
// declaration. TestMaterializedEdgeIdentityMatchesRelationshipMergeSource
// (materialized_edge_identity_drift_test.go) is the separate drift guard
// that parses this package's own writer sources and asserts these
// declarations are a bijection with what the MERGE templates actually key
// on.
var materializedEdgeIdentityByFamily = map[string]map[string][]string{
	"repo_dependency":       {},
	"workload_dependency":   {},
	"code_calls":            {},
	"sql_relationships":     {},
	"shell_exec":            {},
	"inheritance_edges":     {},
	"documentation_edges":   {},
	"rationale_edges":       {},
	"deployable_unit_edges": {},
	"handles_route":         {},
	"runs_in":               {},
	"invokes_cloud_action":  {},
	"codeowners_ownership_edges": {
		"DECLARES_CODEOWNER": {"pattern", "source_path"},
	},
	"submodule_pin_edges": {
		"PINS_SUBMODULE": {"path"},
	},
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
// mutate the package-level table through the returned value.
func MaterializedEdgeIdentityProperties(family string) (map[string][]string, error) {
	entry, ok := materializedEdgeIdentityByFamily[family]
	if !ok {
		return nil, fmt.Errorf("cypher: no materialized-edge identity registered for family %q; register it in materializedEdgeIdentityByFamily before proving it on a live gate", family)
	}
	out := make(map[string][]string, len(entry))
	for edgeType, props := range entry {
		propsCopy := make([]string, len(props))
		copy(propsCopy, props)
		out[edgeType] = propsCopy
	}
	return out, nil
}
