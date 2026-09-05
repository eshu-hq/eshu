// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "fmt"

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
// types its write path materializes, the retract statement that reaps them,
// and (for a family whose relationship MERGE keys on more than its two
// endpoint nodes) the write template that owns that identity.
type materializedEdgeFamily struct {
	// EdgeTypes are the relationship types the family owns, mapped to a short
	// reason naming what writes each.
	EdgeTypes map[string]string
	// RetractCypher is the production retract template whose [rel:...]
	// alternation must name exactly EdgeTypes. Holding the real const (not a
	// copy of its text) is what makes the drift guard meaningful.
	RetractCypher string
	// IdentityCypher is the production write template whose relationship
	// MERGE clause is this family's source of truth for IdentityProperties.
	// Every single-type family in this table names one, so
	// TestSingleTypeFamilyIdentityMatchesWriteCypher can extract the real
	// MERGE property map from it rather than trusting a hand-copied literal
	// — the same "hold the const, not its text" principle as RetractCypher.
	IdentityCypher string
	// IdentityProperties are the relationship properties — beyond the two
	// endpoint nodes — that participate in this family's single edge type's
	// MERGE identity. Nil for a family whose relationship MERGEs on its
	// endpoints alone (`MERGE (a)-[rel:TYPE]->(b)`, no property map).
	// codeowners_ownership_edges and submodule_pin_edges are the two
	// exceptions: their writers fold a relationship property into the MERGE
	// key itself (canonical_codeowners_edges.go, canonical_submodule_edges.go)
	// because two distinct source rows can otherwise collide onto the same
	// (source, target) relationship pattern and silently overwrite each
	// other.
	IdentityProperties []string
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
		EdgeTypes:      map[string]string{"EXECUTES_SHELL": "shell invocation edge (batchCanonicalShellExecUpsertCypher)"},
		RetractCypher:  retractShellExecEdgesCypher,
		IdentityCypher: batchCanonicalShellExecUpsertCypher,
	},
	"workload_dependency": {
		EdgeTypes:      map[string]string{"DEPENDS_ON": "workload-to-workload dependency (batchCanonicalWorkloadDependencyUpsertCypher)"},
		RetractCypher:  retractWorkloadDependencyEdgesCypher,
		IdentityCypher: batchCanonicalWorkloadDependencyUpsertCypher,
	},
	"deployable_unit_edges": {
		EdgeTypes:      map[string]string{"CORRELATES_DEPLOYABLE_UNIT": "deployable-unit correlation (batchCanonicalDeployableUnitCorrelationUpsertCypher)"},
		RetractCypher:  retractDeployableUnitCorrelationEdgesCypher,
		IdentityCypher: batchCanonicalDeployableUnitCorrelationUpsertCypher,
	},
	"handles_route": {
		EdgeTypes:      map[string]string{"HANDLES_ROUTE": "handler-to-route binding (batchCanonicalHandlesRouteEdgeUpsertCypher)"},
		RetractCypher:  retractHandlesRouteEdgesCypher,
		IdentityCypher: batchCanonicalHandlesRouteEdgeUpsertCypher,
	},
	"runs_in": {
		EdgeTypes:      map[string]string{"RUNS_IN": "entity-runs-in-workload edge (batchCanonicalRunsInEdgeUpsertCypher)"},
		RetractCypher:  retractRunsInEdgesCypher,
		IdentityCypher: batchCanonicalRunsInEdgeUpsertCypher,
	},
	"invokes_cloud_action": {
		EdgeTypes:      map[string]string{"INVOKES_CLOUD_ACTION": "code-to-cloud-action invocation (batchCanonicalInvokesCloudActionUpsertCypher)"},
		RetractCypher:  retractInvokesCloudActionEdgesCypher,
		IdentityCypher: batchCanonicalInvokesCloudActionUpsertCypher,
	},
	"codeowners_ownership_edges": {
		EdgeTypes:          map[string]string{"DECLARES_CODEOWNER": "CODEOWNERS ownership declaration (batchCanonicalCodeownersOwnershipEdgeCypher)"},
		RetractCypher:      retractCodeownersOwnershipEdgesCypher,
		IdentityCypher:     batchCanonicalCodeownersOwnershipEdgeCypher,
		IdentityProperties: []string{"pattern", "source_path"},
	},
	"submodule_pin_edges": {
		EdgeTypes:          map[string]string{"PINS_SUBMODULE": "submodule commit pin (batchCanonicalSubmodulePinEdgeCypher)"},
		RetractCypher:      retractSubmodulePinEdgesCypher,
		IdentityCypher:     batchCanonicalSubmodulePinEdgeCypher,
		IdentityProperties: []string{"path"},
	},
	"documentation_edges": {
		EdgeTypes:      map[string]string{"DOCUMENTS": "doc-section-to-entity edge (batchCanonicalDocumentationEntityEdgeCypher)"},
		RetractCypher:  retractDocumentationEdgesCypher,
		IdentityCypher: batchCanonicalDocumentationEntityEdgeCypher,
	},
	"rationale_edges": {
		EdgeTypes:      map[string]string{"EXPLAINS": "rationale-to-entity edge (batchCanonicalRationaleExplainsEdgeCypher)"},
		RetractCypher:  retractRationaleEdgesCypher,
		IdentityCypher: batchCanonicalRationaleExplainsEdgeCypher,
	},
	// The first three DIRECT-materialization families registered here (#6228).
	// Unlike every entry above them, these reach the graph straight from their
	// own reducer port with no shared-projection intent row in between, so
	// reducer.DirectMaterializedEdgeFamilies() enumerates them rather than
	// reducer.MaterializedEdgeFamilies().
	//
	// All three types below are read off the Cypher the port executes, never off
	// the port's name and never off its statement-metadata label. That is the
	// #6181 rule, and all three families are cases where the shortcut would have
	// produced the wrong answer:
	//
	//   - kubernetes_namespace_environment's port is named
	//     WriteKubernetesNamespaceNodes and MERGEs a relationship.
	//   - iam_instance_profile_role's statement-metadata label is
	//     iamInstanceProfileRoleEdgeLabel ("IAM_INSTANCE_PROFILE_HAS_ROLE"),
	//     which is NOT a graph relationship type; the type its template MERGEs
	//     is HAS_ROLE.
	//   - workload_cloud_relationship's statement-metadata label is
	//     workloadCloudRelationshipEdgeLabel ("WORKLOAD_USES_CLOUD_RESOURCE"),
	//     which is NOT a graph relationship type; the type its template MERGEs
	//     is USES.
	//
	// Since #6309 two of the three carry coverage rows. Registering a family
	// here makes `eshu-ifa assert-edges -domain <family>` addressable and lets
	// its vacuity guard resolve; it does not assert that any live matrix
	// drives it. workload_cloud_relationship alone still carries its two
	// waiver rows in specs/ifa-materialized-edge-coverage-direct.v1.yaml for
	// that reason.
	//
	// kubernetes_namespace_environment's write template MERGEs the Environment
	// node with a property map and the relationship without one, so the
	// identity scan (which only matches a property map on a relationship
	// MERGE, `-[rel:TYPE {...}]`) correctly reports no identity properties:
	// this edge keys on its two endpoint nodes alone.
	"kubernetes_namespace_environment": {
		EdgeTypes:      map[string]string{"TARGETS_ENVIRONMENT": "namespace-to-environment binding (canonicalKubernetesNamespaceWithEnvironmentUpsertCypher)"},
		RetractCypher:  retractKubernetesNamespaceStaleTargetsEnvironmentCypher,
		IdentityCypher: canonicalKubernetesNamespaceWithEnvironmentUpsertCypher,
	},
	// iam_instance_profile_role's IdentityCypher holds the %s FORMAT const
	// itself, unformatted. The token substituted into it comes from
	// iamInstanceProfileRoleRelationshipVocabulary, a closed single-member set
	// screened per row by validateIAMInstanceProfileRoleRelationshipType, so
	// HAS_ROLE is the only type this writer can emit. The template MERGEs on
	// its two endpoint nodes alone, so the identity scan yields nothing
	// whether or not the %s has been substituted.
	"iam_instance_profile_role": {
		EdgeTypes:      map[string]string{"HAS_ROLE": "IAM instance-profile to role attachment (canonicalIAMInstanceProfileRoleEdgeUpsertCypherFormat)"},
		RetractCypher:  retractIAMInstanceProfileRoleEdgesCypher,
		IdentityCypher: canonicalIAMInstanceProfileRoleEdgeUpsertCypherFormat,
	},
	// workload_cloud_relationship follows the same single-vocabulary shape:
	// IdentityCypher holds the %s FORMAT const unformatted, and the token
	// substituted into it comes from workloadCloudRelationshipVocabulary, a
	// closed single-member set screened per row by
	// validateWorkloadCloudRelationshipType, so USES is the only type this
	// writer can emit. The template MERGEs on its two endpoint nodes alone
	// (the WorkloadInstance is matched, not merged), so the identity scan
	// yields nothing whether or not the %s has been substituted.
	// workloadCloudRelationshipEdgeLabel ("WORKLOAD_USES_CLOUD_RESOURCE") is
	// statement metadata carried beside the query, not a graph relationship
	// type -- the same #6181-shaped trap iam_instance_profile_role documents
	// one level below the port name.
	"workload_cloud_relationship": {
		EdgeTypes:      map[string]string{"USES": "workload-instance to cloud-resource attachment (workloadCloudRelationshipUpsertCypherFormat)"},
		RetractCypher:  retractWorkloadCloudRelationshipEdgesCypher,
		IdentityCypher: workloadCloudRelationshipUpsertCypherFormat,
	},
}

// materializedEdgeIdentityByFamily declares identity for the four
// reducer.MaterializedEdgeFamilies() entries NOT covered by
// singleTypeMaterializedEdgeFamilies: code_calls, sql_relationships, and
// inheritance_edges keep their own multi-type registries next to their
// writers (see the package doc comment above), and repo_dependency shares
// the DEPENDS_ON type with workload_dependency. Each declares an explicit
// empty identity — their relationship MERGEs key on endpoint nodes alone —
// matching the scope TestMaterializedEdgeFamilyRegistryMatchesItsRetract
// already draws around singleTypeMaterializedEdgeFamilies: proving identity
// against a held write-Cypher const (as
// TestSingleTypeFamilyIdentityMatchesWriteCypher does below) only works for a
// family with one write template in this file; these four span several
// templates across several files, so their reasoning stays there too. The
// empty declaration itself is backed globally, not per-family, by
// TestPropertyKeyedRelationshipMergesMatchKnownAllowList
// (materialized_edge_property_keyed_inventory_test.go), which scans this
// entire package's source for any property-keyed relationship MERGE and
// fails if one appears on a type these four families own.
var materializedEdgeIdentityByFamily = map[string]map[string][]string{
	"repo_dependency":   {},
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
	if _, ok := materializedEdgeIdentityByFamily[family]; ok {
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
