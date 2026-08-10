// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// Materialized-edge family registry for the Ifá exhaustiveness gates (#5543).
//
// ifa.MaterializedEdgeDomainEdgeTypes reads these sets to scope
// `eshu-ifa assert-edges -domain <family>` to the relationship types a family
// actually owns, ignoring every unrelated type sharing the graph. Getting a set
// wrong fails in two directions, and they are not symmetric:
//
//   - TOO SMALL is silent. The gate stops asserting the omitted types, so a
//     family that half-materializes still reports exhaustive. This is the
//     dangerous direction and the one a casual reading produces.
//   - TOO LARGE is loud but wrong. The family claims edges it does not own, so
//     another domain's regression surfaces as a spurious failure here.
//
// The multi-type families (code_calls, inheritance_edges) keep their own
// registries next to their writers, because their types span several templates
// and files and the reasoning belongs there. The families below each materialize
// a single relationship type, so they are collected here rather than scattered
// as ten near-identical accessors.
//
// Every entry carries the production retract Cypher it must agree with.
// TestMaterializedEdgeFamilyRegistryMatchesItsRetract extracts the alternation
// from that const at test time instead of comparing against a copied literal —
// a literal would pin this file against itself and keep passing after the
// retract changed underneath it.

// materializedEdgeFamily is one family's ownership record: the relationship
// types its write path materializes, and the retract statement that reaps them.
type materializedEdgeFamily struct {
	// EdgeTypes are the relationship types the family owns, mapped to a short
	// reason naming what writes each.
	EdgeTypes map[string]string
	// RetractCypher is the production retract template whose [rel:...]
	// alternation must name exactly EdgeTypes. Holding the real const (not a
	// copy of its text) is what makes the drift guard meaningful.
	RetractCypher string
}

// singleTypeMaterializedEdgeFamilies indexes the one-relationship-type families
// by their reducer domain constant value.
//
// Each type below is corroborated by three independent sources: the write
// template's MERGE, the retract alternation, and the domain's own producer.
// codeowners_ownership_edges and submodule_pin_edges are the two whose MERGE is
// assembled at runtime rather than written as a literal in the template file, so
// for those the retract is the load-bearing confirmation.
var singleTypeMaterializedEdgeFamilies = map[string]materializedEdgeFamily{
	"shell_exec": {
		EdgeTypes:     map[string]string{"EXECUTES_SHELL": "shell invocation edge (batchCanonicalShellExecUpsertCypher)"},
		RetractCypher: retractShellExecEdgesCypher,
	},
	"workload_dependency": {
		EdgeTypes:     map[string]string{"DEPENDS_ON": "workload-to-workload dependency (batchCanonicalWorkloadDependencyUpsertCypher)"},
		RetractCypher: retractWorkloadDependencyEdgesCypher,
	},
	"deployable_unit_edges": {
		EdgeTypes:     map[string]string{"CORRELATES_DEPLOYABLE_UNIT": "deployable-unit correlation (batchCanonicalDeployableUnitCorrelationUpsertCypher)"},
		RetractCypher: retractDeployableUnitCorrelationEdgesCypher,
	},
	"handles_route": {
		EdgeTypes:     map[string]string{"HANDLES_ROUTE": "handler-to-route binding (batchCanonicalHandlesRouteEdgeUpsertCypher)"},
		RetractCypher: retractHandlesRouteEdgesCypher,
	},
	"runs_in": {
		EdgeTypes:     map[string]string{"RUNS_IN": "entity-runs-in-workload edge (batchCanonicalRunsInEdgeUpsertCypher)"},
		RetractCypher: retractRunsInEdgesCypher,
	},
	"invokes_cloud_action": {
		EdgeTypes:     map[string]string{"INVOKES_CLOUD_ACTION": "code-to-cloud-action invocation (batchCanonicalInvokesCloudActionUpsertCypher)"},
		RetractCypher: retractInvokesCloudActionEdgesCypher,
	},
	"codeowners_ownership_edges": {
		EdgeTypes:     map[string]string{"DECLARES_CODEOWNER": "CODEOWNERS ownership declaration (batchCanonicalCodeownersOwnershipEdgeCypher)"},
		RetractCypher: retractCodeownersOwnershipEdgesCypher,
	},
	"submodule_pin_edges": {
		EdgeTypes:     map[string]string{"PINS_SUBMODULE": "submodule commit pin (batchCanonicalSubmodulePinEdgeCypher)"},
		RetractCypher: retractSubmodulePinEdgesCypher,
	},
	"documentation_edges": {
		EdgeTypes:     map[string]string{"DOCUMENTS": "doc-section-to-entity edge (batchCanonicalDocumentationEntityEdgeCypher)"},
		RetractCypher: retractDocumentationEdgesCypher,
	},
	"rationale_edges": {
		EdgeTypes:     map[string]string{"EXPLAINS": "rationale-to-entity edge (batchCanonicalRationaleExplainsEdgeCypher)"},
		RetractCypher: retractRationaleEdgesCypher,
	},
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
