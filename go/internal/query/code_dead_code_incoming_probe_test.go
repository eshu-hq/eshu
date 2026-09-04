// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// The scoped incoming-edge probe answers two questions about one candidate at
// once: is anything the caller may read still calling it, and is anything they
// may not read still calling it. This file pins both answers and the shape that
// produces them.

// deadCodeIncomingProbeSource is one seeded incoming edge: the resolution
// method it carries and whether its source repository is inside the grant. A
// source the graph cannot attribute to any repository is inGrant=false, the
// same answer the backend gives for it.
type deadCodeIncomingProbeSource struct {
	method  string
	inGrant bool
}

// deadCodeIncomingProbeGraph answers the incoming-edge probe the way NornicDB
// v1.2.3 answered it on the seeded fixture, for whichever statement shape it is
// handed. It is not a re-implementation of the handler: it models the backend,
// and the difference between the two shapes is exactly what the live proof
// measured.
//
//   - the merged probe (an in_grant column) groups the seeded sources by
//     (entity, method, in_grant), so a source outside the grant stays its own
//     row even when a granted source carries the same method.
//   - the shipped pair collapses that case. Both probes RETURN DISTINCT the
//     (entity, method) pair, so the ungranted source's row is byte for byte the
//     granted one's and the diff between them is empty.
type deadCodeIncomingProbeGraph struct {
	sourcesByEntity map[string][]deadCodeIncomingProbeSource
	statements      []string
}

func (g *deadCodeIncomingProbeGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.statements = append(g.statements, cypher)
	entityIDs, _ := params["entity_ids"].([]string)
	merged := strings.Contains(cypher, "in_grant")
	grantBound := !merged && strings.Contains(cypher, "source_repo:Repository")

	rows := make([]map[string]any, 0)
	for _, entityID := range entityIDs {
		seen := make(map[string]struct{})
		for _, source := range g.sourcesByEntity[entityID] {
			if grantBound && !source.inGrant {
				continue
			}
			key := source.method
			if merged {
				key = source.method + "\x00" + boolKey(source.inGrant)
			}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			row := map[string]any{
				"incoming_entity_id": entityID,
				"resolution_method":  source.method,
			}
			if merged {
				row["in_grant"] = source.inGrant
				row["edge_count"] = 1
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (g *deadCodeIncomingProbeGraph) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func boolKey(value bool) string {
	if value {
		return "t"
	}
	return "f"
}

const deadCodeIncomingProbeEntity = "repo://tenant-a/granted-service#unusedHelper"

func runDeadCodeIncomingProbe(
	t *testing.T,
	sources []deadCodeIncomingProbeSource,
) (map[string]deadCodeIncomingEdge, *deadCodeIncomingProbeGraph) {
	t.Helper()

	graph := &deadCodeIncomingProbeGraph{
		sourcesByEntity: map[string][]deadCodeIncomingProbeSource{deadCodeIncomingProbeEntity: sources},
	}
	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Neo4j: graph}
	ctx := ContextWithAuthContext(
		context.Background(),
		codeGrantScopedAuthContext([]string{codeGrantGrantedRepo}),
	)
	incoming, err := handler.deadCodeResultsWithGraphIncomingEdges(ctx, []map[string]any{{
		"entity_id": deadCodeIncomingProbeEntity,
		"repo_id":   codeGrantGrantedRepo,
		"language":  "go",
		"labels":    []any{"Function"},
	}}, "Function")
	if err != nil {
		t.Fatalf("deadCodeResultsWithGraphIncomingEdges() error = %v, want nil", err)
	}
	return incoming, graph
}

// TestDeadCodeGraphProbeKeepsASameMethodUngrantedSource is the case the shipped
// pair of probes could not see.
//
// Two sources call the candidate: one the caller may read and one it may not,
// and both edges carry the same resolution method. Each probe RETURN DISTINCTs
// the (entity, method) pair, so both return the identical single row and the
// difference between them is empty -- the candidate reads as plainly reachable
// and the caller is never told a consumer was hidden from them. The SQL half
// has always answered permission_hidden_consumer here, because it decides the
// grant per row.
func TestDeadCodeGraphProbeKeepsASameMethodUngrantedSource(t *testing.T) {
	t.Parallel()

	incoming, _ := runDeadCodeIncomingProbe(t, []deadCodeIncomingProbeSource{
		{method: codeprovenance.MethodImportBinding, inGrant: true},
		{method: codeprovenance.MethodImportBinding, inGrant: false},
	})
	edge := incoming[deadCodeIncomingProbeEntity]
	if !edge.HiddenConsumer {
		t.Fatalf("edge = %#v, want the ungranted source reported as hidden even though a granted source carries the same resolution method", edge)
	}
	if got, want := edge.Method, codeprovenance.MethodImportBinding; got != want {
		t.Fatalf("Method = %q, want %q: the granted edge is still evidence", got, want)
	}
}

// TestDeadCodeGraphProbeRunsOneTraversalPerPage is the performance half of the
// same change. The scoped path expanded every incoming edge of every candidate
// twice, once per probe, and a high-fan-in symbol pays that in full before
// either statement's DISTINCT reduces anything.
func TestDeadCodeGraphProbeRunsOneTraversalPerPage(t *testing.T) {
	t.Parallel()

	_, graph := runDeadCodeIncomingProbe(t, []deadCodeIncomingProbeSource{
		{method: codeprovenance.MethodImportBinding, inGrant: true},
	})
	if got, want := len(graph.statements), 1; got != want {
		t.Fatalf("statement count = %d, want %d: one expansion per candidate page, not one per probe", got, want)
	}
}

// TestDeadCodeGraphProbeReadsEachSourceClass covers the four answers one
// candidate's incoming edges can produce.
func TestDeadCodeGraphProbeReadsEachSourceClass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		sources        []deadCodeIncomingProbeSource
		wantMethod     string
		wantHidden     bool
		wantConfidence float64
	}{
		{
			name:           "granted source alone is evidence",
			sources:        []deadCodeIncomingProbeSource{{method: codeprovenance.MethodImportBinding, inGrant: true}},
			wantMethod:     codeprovenance.MethodImportBinding,
			wantConfidence: codeprovenance.Confidence(codeprovenance.MethodImportBinding),
		},
		{
			name:       "ungranted source alone is only a marker",
			sources:    []deadCodeIncomingProbeSource{{method: codeprovenance.MethodImportBinding, inGrant: false}},
			wantHidden: true,
		},
		{
			name:       "unattributed source is hidden, not evidence",
			sources:    []deadCodeIncomingProbeSource{{method: codeprovenance.MethodUnspecified, inGrant: false}},
			wantHidden: true,
		},
		{
			name: "a weak granted source beside an ungranted one keeps both answers",
			sources: []deadCodeIncomingProbeSource{
				{method: codeprovenance.MethodRepoUniqueName, inGrant: true},
				{method: codeprovenance.MethodImportBinding, inGrant: false},
			},
			wantMethod:     codeprovenance.MethodRepoUniqueName,
			wantHidden:     true,
			wantConfidence: codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			incoming, _ := runDeadCodeIncomingProbe(t, testCase.sources)
			edge := incoming[deadCodeIncomingProbeEntity]
			if edge.HiddenConsumer != testCase.wantHidden {
				t.Fatalf("HiddenConsumer = %v, want %v (edge = %#v)", edge.HiddenConsumer, testCase.wantHidden, edge)
			}
			if edge.Method != testCase.wantMethod {
				t.Fatalf("Method = %q, want %q", edge.Method, testCase.wantMethod)
			}
			if edge.MaxConfidence != testCase.wantConfidence {
				t.Fatalf("MaxConfidence = %v, want %v: an edge the caller cannot see is not evidence", edge.MaxConfidence, testCase.wantConfidence)
			}
		})
	}
}
