// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialized

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
// and files and the reasoning belongs there. The single-type families each
// materialize one relationship type, so they are collected as one table in
// single_type_families.go rather than scattered
// as near-identical accessors. The multi-type DIRECT families
// (incident_routing_evidence, observability_coverage, security_group_sg_rule)
// are the deliberate exception: each owns one closed vocabulary and a fully
// held retract-variant set (full-scope shapes plus ledger-anchored ByUIDs
// shapes where the writer has them — which for security_group_sg_rule span
// two writer files), so their reasoning fits one row each in the separate
// multi-type table beside the single-type one (direct_families.go). Every
// entry in that multi-type table is a contract family whose retract is held
// and exactly-scoped — an edge-scoped alternation or, for kind-A
// evidenceNodeDetach entries, a node-detach on a sole-creator label — which
// is what lets the drift guard pin predicate shape on the multi-type table
// while this single-type table keeps its pre-contract shapes — except the
// node-scoped code_taint_evidence entry, whose evidenceNodeDetach check pins
// its own evidence_source plus scope predicates per variant.
//
// Every entry carries the production retract Cypher it must agree with.
// TestMaterializedEdgeFamilyRegistryMatchesItsRetract checks that const at
// test time — the alternation for a relAlternation family, the node-detach
// shape for an evidenceNodeDetach family — instead of comparing against a
// copied literal: a literal would pin this file against itself and keep
// passing after the retract changed underneath it.

// retractKind names how a family's production retract reaps its edges (#6228
// retract-registry contract,
// docs/internal/design/6228-materialized-edge-retract-registry-contract.md).
type retractKind uint8

const (
	// retractKindRelAlternation is the default: the retract names its owned
	// relationship types in a [rel:A|B] alternation while keeping the
	// evidence_source and scope predicates. The zero value, so every family
	// registered before the contract keeps this meaning without an edit.
	retractKindRelAlternation retractKind = iota
	// retractKindEvidenceNodeDetach covers a family whose edges hang off
	// private evidence nodes: the retract MATCHes the evidence nodes it owns
	// and DETACH DELETEs them, reaping the edges as a cascade. Only a family
	// that passes the sole-creator census (no other production writer MERGEs
	// its evidence label) may use this kind.
	retractKindEvidenceNodeDetach
)

// materializedEdgeFamily is one family's ownership record: the relationship
// types its write path materializes, the retract statement that reaps them,
// and (for a family whose relationship MERGE keys on more than its two
// endpoint nodes) the write template that owns that identity.
type materializedEdgeFamily struct {
	// EdgeTypes are the relationship types the family owns, mapped to a short
	// reason naming what writes each.
	EdgeTypes map[string]string
	// RetractKind names how the retract reaps the family's edges. The zero
	// value is retractKindRelAlternation; families whose retract DETACH
	// DELETEs private evidence nodes set retractKindEvidenceNodeDetach.
	RetractKind retractKind
	// RetractCypher is the production retract template whose [rel:...]
	// alternation must name exactly EdgeTypes. Holding the real const (not a
	// copy of its text) is what makes the drift guard meaningful. For an
	// evidenceNodeDetach family it is the production node-detach template the
	// shape check pins instead.
	RetractCypher string
	// RetractVariants holds every further production retract variant the
	// family owns (ledger-anchored ByUIDs shapes, stale-generation shapes).
	// RetractCypher plus RetractVariants must hold every production retract
	// variant: the drift guard checks all of them, so a variant edited without
	// its alternation (or detach shape) turns the build red instead of
	// silently narrowing what the family reaps.
	RetractVariants []string
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
	// Codeowners and submodule writers use identity properties to distinguish
	// multiple logical rows sharing one endpoint pair. Workload dependencies
	// use one constant identity property so concurrent source- and target-scope
	// writers converge on the same stored relationship.
	IdentityProperties []string
}
