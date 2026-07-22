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

// MaterializedEdgeDomainEdgeTypes returns the set of graph relationship types
// the given materialized-edge family's writer registry accepts, so a live
// `assert-edges` check knows which of a graph's edges belong to the family it
// is asserting (and ignores every unrelated edge type — CONTAINS, DEPENDS_ON,
// the GCP families, etc. — that shares the same graph). It is registry-derived
// (#5330 pattern), never hand-listed: the sql_relationships set comes straight
// from cypher.SQLRelationshipMaterializedEdgeTypes(), so an additional SQL edge type
// added to that writer registry is asserted here without a second edit. An
// unknown family returns nil so the caller can fail closed with a clear
// message rather than silently asserting an empty type set (which would make
// any graph vacuously pass).
func MaterializedEdgeDomainEdgeTypes(domain string) (map[string]struct{}, error) {
	switch domain {
	case "sql_relationships":
		reg := cypher.SQLRelationshipMaterializedEdgeTypes()
		out := make(map[string]struct{}, len(reg))
		for edgeType := range reg {
			out[edgeType] = struct{}{}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("ifa: no materialized-edge family registered for domain %q (only sql_relationships has live assert-edges coverage as of #5351)", domain)
	}
}
