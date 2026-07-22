// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/storage/cypher"

// contentContainmentEdgeType is CONTAINS: written for every content-entity
// label (including every SQL entity label) by the generic File-to-entity
// containment writer (cypher.buildEntityStatementsWithContainment, wired via
// WithEntityContainmentInEntityUpsert in every production writer config). It
// is not part of any domain-scoped write-reason whitelist because it applies
// universally across entity types, so it is recorded here directly rather
// than derived from a per-domain map the way the SQL relationship types are.
const contentContainmentEdgeType = "CONTAINS"

// contentContainmentEdgeReason is the human-readable reason recorded for
// contentContainmentEdgeType in the materialized-edge registry.
const contentContainmentEdgeReason = "generic File->entity containment writer (cypher.buildEntityStatementsWithContainment)"

// structuralEdgeTypes are core structural graph relationships with a real
// writer that are outside the SQL-relationship and content-containment
// domains this registry otherwise derives from. They are registered directly
// (like contentContainmentEdgeType) rather than through a per-domain map.
// Added for the #5335 edge-materialization gate
// (impact_edge_materialization_gate.go): DEPENDS_ON and REPO_CONTAINS are
// traversed by blast-radius queries with no per-query coverage/complete
// disclosure field (repository, terraform_module), so the gate needs this
// registry to actually know they have writers instead of reporting them as a
// silent gap.
var structuralEdgeTypes = map[string]string{
	"DEPENDS_ON":    "repository dependency edge writer (reducer/code_import_repo_edge.go, reducer/package_consumption_repo_edge.go, reducer/dependency_domain.go)",
	"REPO_CONTAINS": "generic Repository->File containment writer (cypher/canonical_node_cypher.go MERGE (r)-[:REPO_CONTAINS]->(f))",
}

// EdgeCoverage reports whether a graph relationship type is written by a
// known edge writer, and if not, why. It backs the honest blast-radius
// response contract (#5330): a query's UNION branch whose edge type has no
// writer must be reported as unmaterialized rather than silently
// contributing zero rows to the affected set.
type EdgeCoverage struct {
	// EdgeType is the graph relationship type name (e.g. "INDEXES").
	EdgeType string
	// Materialized is true when a known writer produces this edge type.
	Materialized bool
	// Reason is a short human-readable explanation: the write-reason string
	// recorded on the edge when Materialized is true, or "no_writer" when
	// Materialized is false.
	Reason string
}

// materializedEdgeTypes returns the set of graph relationship types this
// registry knows to be written by a real edge writer, keyed by edge type name
// and mapping to a short human-readable reason. It is registry-derived, not a
// graph probe: it merges the SQL relationship edge-writer whitelist (the
// authoritative source for READS_FROM, WRITES_TO, REFERENCES_TABLE,
// HAS_COLUMN, TRIGGERS, EXECUTES, QUERIES_TABLE, INDEXES, and MIGRATES — see
// cypher.SQLRelationshipMaterializedEdgeTypes)
// with the always-on structural CONTAINS edge and structuralEdgeTypes
// (DEPENDS_ON, REPO_CONTAINS), plus the Crossplane (cypher.CrossplaneRelationship
// MaterializedEdgeTypes) and Flux (cypher.FluxRelationshipMaterializedEdgeTypes)
// canonical edge writers. Because it reads the writer whitelists directly, a
// new SQL relationship type or canonical edge type added to one of those
// whitelists flips this registry automatically without a second edit here.
func materializedEdgeTypes() map[string]string {
	out := cypher.SQLRelationshipMaterializedEdgeTypes()
	out[contentContainmentEdgeType] = contentContainmentEdgeReason
	for edgeType, reason := range structuralEdgeTypes {
		out[edgeType] = reason
	}
	for edgeType, reason := range cypher.CrossplaneRelationshipMaterializedEdgeTypes() {
		out[edgeType] = reason
	}
	for edgeType, reason := range cypher.FluxRelationshipMaterializedEdgeTypes() {
		out[edgeType] = reason
	}
	return out
}

// EdgeMaterializationCoverage reports whether edgeType is materialized by a
// known writer. It is reusable across blast-radius query domains — the SQL
// domain wired here (#5330) and the Crossplane domain (#5331) — and by a
// future static CI gate (#5335) that asserts a query's UNION branches only
// claim edge types with a real writer.
func EdgeMaterializationCoverage(edgeType string) EdgeCoverage {
	reasons := materializedEdgeTypes()
	if reason, ok := reasons[edgeType]; ok {
		return EdgeCoverage{EdgeType: edgeType, Materialized: true, Reason: reason}
	}
	return EdgeCoverage{EdgeType: edgeType, Materialized: false, Reason: "no_writer"}
}

// MaterializedEdgeTypeSet returns the set of edge types this registry knows
// to be materialized. Use this over repeated EdgeMaterializationCoverage
// calls when a caller (a test, or a future static gate) needs the whole set
// rather than one lookup.
func MaterializedEdgeTypeSet() map[string]struct{} {
	reasons := materializedEdgeTypes()
	out := make(map[string]struct{}, len(reasons))
	for edgeType := range reasons {
		out[edgeType] = struct{}{}
	}
	return out
}
