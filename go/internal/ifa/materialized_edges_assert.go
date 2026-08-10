// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// ExpectedEdge is the exported identity triple of one hand-derived expected
// materialized graph edge — the shape `cmd/ifa`'s `assert-edges` verb
// compares a live graph against. It mirrors the unexported
// sqlRelationshipExpectedEdge the pure vacuity guard uses, exported so the
// live-gate verb reads the SAME expected-edge-set fixture through one loader
// rather than re-declaring the JSON shape.
type ExpectedEdge struct {
	// RelationshipType is the graph relationship type (e.g. "QUERIES_TABLE").
	RelationshipType string
	// SourceEntityID is the edge source node's canonical uid.
	SourceEntityID string
	// TargetEntityID is the edge target node's canonical uid.
	TargetEntityID string
}

// Key is the canonical set-membership key for exact-set comparison: the same
// `type|source|target` shape sqlRelationshipEdgeKey builds, so a live-graph
// edge and a fixture edge collide iff they are the same edge.
func (e ExpectedEdge) Key() string {
	return sqlRelationshipEdgeKey(e.RelationshipType, e.SourceEntityID, e.TargetEntityID)
}

// LoadExpectedEdges reads a hand-derived expected-edge-set fixture file (the
// same format the pure vacuity guard consumes) into the exported ExpectedEdge
// shape. It is the single loader `cmd/ifa`'s `assert-edges` verb uses, so the
// live gate and the pure `go test` guard can never drift on the fixture
// format.
func LoadExpectedEdges(path string) ([]ExpectedEdge, error) {
	edges, err := loadSQLRelationshipExpectedEdges(path)
	if err != nil {
		return nil, err
	}
	out := make([]ExpectedEdge, len(edges))
	for i, e := range edges {
		// Direct struct conversion: ExpectedEdge and sqlRelationshipExpectedEdge
		// have identical field types and order (only the JSON tags differ, which
		// a conversion ignores), so this stays correct if either grows a field —
		// the compiler rejects the conversion the moment their shapes diverge.
		out[i] = ExpectedEdge(e)
	}
	return out, nil
}

// edgeTypeSet projects a writer registry's type->reason map onto the set shape
// the assert path compares against. Keeping the projection in one helper is what
// lets a family's arm be a single line, so adding a family cannot accidentally
// ship a subtly different (for example, reason-keyed) set.
func edgeTypeSet(registry map[string]string) map[string]struct{} {
	out := make(map[string]struct{}, len(registry))
	for edgeType := range registry {
		out[edgeType] = struct{}{}
	}
	return out
}

// MaterializedEdgeDomainEdgeTypes returns the set of graph relationship types
// the given materialized-edge family's writer registry accepts, so a live
// `assert-edges` check knows which of a graph's edges belong to the family it
// is asserting (and ignores every unrelated edge type — CONTAINS, the GCP
// families, etc. — that shares the same graph).
//
// Every set is registry-derived (#5330 pattern), never hand-listed here, so an
// edge type added to a writer registry is asserted without a second edit. The
// multi-type families resolve through explicit arms because their sets span
// several templates and files: sql_relationships, code_calls (CALLS,
// REFERENCES, USES_METACLASS, INSTANTIATES), inheritance_edges (INHERITS,
// OVERRIDES, ALIASES, IMPLEMENTS) and repo_dependency (six repo-to-repo types
// plus RUNS_ON). The remaining families each materialize a single type and
// resolve from cypher's shared table.
//
// An unknown family returns an error rather than an empty set, so the caller
// fails closed: an empty type set would assert nothing and let any graph pass
// vacuously, which is exactly the false green the #5543 waiver rows exist to
// prevent.
func MaterializedEdgeDomainEdgeTypes(domain string) (map[string]struct{}, error) {
	switch domain {
	case "sql_relationships":
		return edgeTypeSet(cypher.SQLRelationshipMaterializedEdgeTypes()), nil
	case "code_calls":
		return edgeTypeSet(cypher.CodeCallMaterializedEdgeTypes()), nil
	case "inheritance_edges":
		return edgeTypeSet(cypher.InheritanceMaterializedEdgeTypes()), nil
	case "repo_dependency":
		return edgeTypeSet(cypher.RepoDependencyMaterializedEdgeTypes()), nil
	default:
		// The single-relationship-type families resolve from the shared registry
		// table. They are looked up rather than switched on so registering one is
		// a data change on the writer side, while the multi-type families above
		// keep explicit arms because their sets span several templates.
		if reg, ok := cypher.SingleTypeMaterializedEdgeTypes(domain); ok {
			return edgeTypeSet(reg), nil
		}
		return nil, fmt.Errorf("ifa: no materialized-edge family registered for domain %q; register its edge types beside its writer (multi-type) or in cypher.singleTypeMaterializedEdgeFamilies (single-type) before proving it on a live gate", domain)
	}
}
