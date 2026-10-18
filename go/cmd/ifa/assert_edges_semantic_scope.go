// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
	materialized "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/materialized"
)

// assertSemanticEntityContainmentEdges is the repo-scoped exact-set assertion
// for `ifa assert-edges -domain semantic_entity_containment` (#8359 arbiter
// verdict (a)): a shared N={1,2,4} cell with the exact-set assert scoped to
// the family's own fixture repo -- no sibling subtraction, no dedicated cell.
//
// An edge is in scope iff its TARGET node's repo_id equals the fixture repo
// (the writer sets n.repo_id on the entity node --
// cypher/semantic_entity_statements.go -- while the File source node is a
// canonical node this family does not own, so filtering on the source side
// would fence on another writer's property) OR either endpoint resolves to
// an expected endpoint identity (the rationale-pattern fallback, keeping
// repository-property drift visible instead of silently reclassifying an
// expected edge as foreign). Inside the scope the comparison is the true
// exact multiset assertMaterializedEdges already proves: missing, extra, and
// duplicate are all red.
//
// The endpoint-label constraint plus the evidence_source stamp fence (the
// family's registered MaterializedEdgeEndpoint) still apply inside the
// scope, unchanged: the repo scope is an additional fence, never a widening
// of the stamp fence, and no sibling population is subtracted.
func assertSemanticEntityContainmentEdges(
	ctx context.Context,
	reader graphdump.Reader,
	repoID string,
	expected []materializededges.ExpectedEdge,
) error {
	edgeTypes, err := materializededges.MaterializedEdgeDomainEdgeTypes("semantic_entity_containment")
	if err != nil {
		return fmt.Errorf("ifa assert-edges: %w", err)
	}
	identity, err := materialized.MaterializedEdgeIdentityProperties("semantic_entity_containment")
	if err != nil {
		return fmt.Errorf("ifa assert-edges: %w", err)
	}
	endpoints, _ := materialized.MaterializedEdgeEndpointLabels("semantic_entity_containment")
	expectedEndpointIDs := make(map[string]struct{}, 2*len(expected))
	for _, e := range expected {
		expectedEndpointIDs[e.SourceEntityID] = struct{}{}
		expectedEndpointIDs[e.TargetEntityID] = struct{}{}
	}
	scoped := semanticScopeReader{next: reader, repoID: repoID, expectedEndpointIDs: expectedEndpointIDs}
	if err := assertMaterializedEdges(ctx, scoped, "semantic_entity_containment", edgeTypes, endpoints, identity, expected); err != nil {
		return err
	}
	return nil
}

// semanticScopeReader is a graphdump.Reader that passes through only the
// in-scope edges, so the shared exact-multiset comparison never sees a
// wholly-foreign population. Nodes stream untouched: only edges carry the
// repo scope this cell asserts.
type semanticScopeReader struct {
	next                graphdump.Reader
	repoID              string
	expectedEndpointIDs map[string]struct{}
}

func (r semanticScopeReader) StreamNodes(ctx context.Context, yield func(graphdump.Node) error) error {
	return r.next.StreamNodes(ctx, yield) //nolint:wrapcheck // pass-through keeps the backend's error identity.
}

func (r semanticScopeReader) StreamEdges(ctx context.Context, yield func(graphdump.Edge) error) error {
	err := r.next.StreamEdges(ctx, func(edge graphdump.Edge) error {
		if !semanticEdgeIsInScope(edge, r.repoID, r.expectedEndpointIDs) {
			return nil
		}
		return yield(edge)
	})
	if err != nil {
		return fmt.Errorf("ifa assert-edges: stream scoped semantic_entity_containment edges: %w", err)
	}
	return nil
}

// semanticEdgeIsInScope reports whether edge belongs to the fixture repo's
// asserted CONTAINS population: its target node's repo_id is the fixture
// repo, or either endpoint resolves (through endpointID's uid/id/label-keyed
// fallbacks) to an identity the expected set names.
func semanticEdgeIsInScope(edge graphdump.Edge, repoID string, expectedEndpointIDs map[string]struct{}) bool {
	if toRepo, _ := edge.ToProps["repo_id"].(string); toRepo == repoID {
		return true
	}
	if _, ok := expectedEndpointIDs[endpointID(edge.FromProps, edge.FromLabels)]; ok {
		return true
	}
	_, ok := expectedEndpointIDs[endpointID(edge.ToProps, edge.ToLabels)]
	return ok
}
