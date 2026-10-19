// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
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
// The endpoint-label constraint still applies inside the scope, unchanged.
// Stamp discipline moved here from the registered MaterializedEdgeEndpoint
// fence (#8359): the expected set is backend-explicit — the base file
// declares only the semantic stamp (Neo4j truth, where the legacy writer
// owns every edge), the NornicDB variant additionally declares the canonical
// stamp for its canonical-owned pairs (where the canonical writer owns the
// edge and the semantic writer only enriches the node). The scope reader
// admits exactly the stamps the loaded file declares, and a non-semantic
// stamp only on a declared endpoint pair, so the scan's registry stamp fence
// (cleared for this path below) cannot re-narrow what the file asserts and
// no sibling population is subtracted.
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
	// The scope reader below enforces stamp discipline from the loaded
	// expected set, so the scan must not re-apply the registry's
	// semantic-only fence: it would drop the variant's canonical-stamped
	// pairs before the comparison ever sees them. Copied per call — the
	// registry entry itself keeps the semantic fence for its other readers.
	if constrained, ok := endpoints["CONTAINS"]; ok {
		constrained.EvidenceSource = ""
		endpoints["CONTAINS"] = constrained
	}
	admission := semanticStampAdmissionForExpected(expected)
	expectedEndpointIDs := make(map[string]struct{}, 2*len(expected))
	for _, e := range expected {
		expectedEndpointIDs[e.SourceEntityID] = struct{}{}
		expectedEndpointIDs[e.TargetEntityID] = struct{}{}
	}
	scoped := semanticScopeReader{next: reader, repoID: repoID, expectedEndpointIDs: expectedEndpointIDs, admission: admission}
	if err := assertMaterializedEdges(ctx, scoped, "semantic_entity_containment", edgeTypes, endpoints, identity, expected); err != nil {
		return err
	}
	return nil
}

// semanticStampAdmission is the file-driven stamp discipline for one
// assert-edges run: which evidence_source values the loaded expected set
// declares, and on which endpoint pairs a non-semantic stamp may appear.
type semanticStampAdmission struct {
	stamps map[string]struct{}
	pairs  map[string]struct{}
}

// semanticStampAdmissionForExpected derives the admission from the loaded
// expected set, never from a backend flag: the base file admits only the
// semantic stamp (today's behavior exactly), the NornicDB variant
// additionally admits the canonical stamp on its declared pairs.
func semanticStampAdmissionForExpected(expected []materializededges.ExpectedEdge) semanticStampAdmission {
	admission := semanticStampAdmission{
		stamps: make(map[string]struct{}, 2),
		pairs:  make(map[string]struct{}, len(expected)),
	}
	for _, e := range expected {
		if stamp := e.Properties["evidence_source"]; stamp != "" {
			admission.stamps[stamp] = struct{}{}
		}
		admission.pairs[e.SourceEntityID+"\x00"+e.TargetEntityID] = struct{}{}
	}
	return admission
}

// semanticScopeReader is a graphdump.Reader that passes through only the
// in-scope edges, so the shared exact-multiset comparison never sees a
// wholly-foreign population. Nodes stream untouched: only edges carry the
// repo scope this cell asserts.
type semanticScopeReader struct {
	next                graphdump.Reader
	repoID              string
	expectedEndpointIDs map[string]struct{}
	admission           semanticStampAdmission
}

func (r semanticScopeReader) StreamNodes(ctx context.Context, yield func(graphdump.Node) error) error {
	return r.next.StreamNodes(ctx, yield) //nolint:wrapcheck // pass-through keeps the backend's error identity.
}

func (r semanticScopeReader) StreamEdges(ctx context.Context, yield func(graphdump.Edge) error) error {
	err := r.next.StreamEdges(ctx, func(edge graphdump.Edge) error {
		if !semanticEdgeIsInScope(edge, r.repoID, r.expectedEndpointIDs, r.admission) {
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
// fallbacks) to an identity the expected set names — and its stamp discipline
// holds. The semantic stamp keeps the historical rule (repo scope or endpoint
// fallback). Any other stamp must both be declared by the loaded expected set
// and sit on a declared endpoint pair: that admits the NornicDB variant's
// canonical-owned pairs while keeping a same-repo, same-label, other-writer
// edge (the plain Function's canonical CONTAINS) out of the comparison
// instead of red-ing it as an extra. A stamp the file never declares is
// dropped exactly as the old registry fence dropped it.
func semanticEdgeIsInScope(
	edge graphdump.Edge,
	repoID string,
	expectedEndpointIDs map[string]struct{},
	admission semanticStampAdmission,
) bool {
	fromID := endpointID(edge.FromProps, edge.FromLabels)
	toID := endpointID(edge.ToProps, edge.ToLabels)
	if toRepo, _ := edge.ToProps["repo_id"].(string); toRepo != repoID {
		if _, ok := expectedEndpointIDs[fromID]; !ok {
			if _, ok := expectedEndpointIDs[toID]; !ok {
				return false
			}
		}
	}
	stamp, _ := edge.Props["evidence_source"].(string)
	if stamp == sourcecypher.SemanticEntityEvidenceSource {
		return true
	}
	if _, ok := admission.stamps[stamp]; !ok {
		return false
	}
	_, ok := admission.pairs[fromID+"\x00"+toID]
	return ok
}
