// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/taghistory"
)

// builtFromEdge is one BUILT_FROM relationship as the reducer actually writes
// it. Edge identity is {scope_id, evidence_source}
// (canonicalProvenanceBuiltFromCypher,
// go/internal/storage/cypher/provenance_edge_writer.go), so one
// image<->repository pair carries one edge per scope and evidence source, and
// the retraction statement beside it exists precisely to remove one scope's
// support without disturbing another's. Parallel edges are the designed model,
// not a corrupt graph.
type builtFromEdge struct {
	digest         string
	repositoryID   string
	scopeID        string
	evidenceSource string
}

// identity is the edge's MERGE key, so two edges on the same image<->repository
// pair are distinct relationships exactly when their {scope_id,
// evidence_source} differs. The test asserts the seed really holds that many
// distinct identities, so its fan-out is genuine edge multiplicity rather than
// a duplicated fixture that would prove nothing.
func (e builtFromEdge) identity() [4]string {
	return [4]string{e.digest, e.repositoryID, e.scopeID, e.evidenceSource}
}

// parallelEdgeTagHistoryGraph is a GraphQuery double that models that edge
// multiplicity, and honours DISTINCT the way the backend does: it emits one row
// per matching EDGE, and collapses them to one row per (digest, repository_id)
// only when the statement it was handed asks for DISTINCT.
//
// That is what makes TestTagHistoryDistinctBoundsBuiltFromFanOut sensitive to
// the word DISTINCT in taghistory.BuiltFromCypher rather than merely asserting
// a constant (#6564 re-review finding 2). Every other tag-history double
// returns a canned edge list and cannot see the difference.
type parallelEdgeTagHistoryGraph struct {
	history        []map[string]any
	edges          []builtFromEdge
	builtFromRows  int
	builtFromReads int
}

func (g *parallelEdgeTagHistoryGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if strings.Contains(cypher, "BUILT_FROM") {
		return g.runBuiltFrom(cypher, params), nil
	}
	offset, _ := params["offset"].(int)
	limit, _ := params["limit"].(int)
	if offset >= len(g.history) {
		return nil, nil
	}
	end := offset + limit
	if end > len(g.history) {
		end = len(g.history)
	}
	return append([]map[string]any(nil), g.history[offset:end]...), nil
}

// runBuiltFrom expands $digests over the seeded edges. Without DISTINCT every
// parallel edge is its own row, which is the pre-fix fan-out; with DISTINCT the
// (digest, repository_id) projection collapses them, which is what bounds the
// result set by distinct source repositories instead of by scope and evidence
// source multiplicity.
func (g *parallelEdgeTagHistoryGraph) runBuiltFrom(cypher string, params map[string]any) []map[string]any {
	g.builtFromReads++
	digests, _ := params["digests"].([]string)
	wanted := make(map[string]struct{}, len(digests))
	for _, digest := range digests {
		wanted[digest] = struct{}{}
	}
	distinct := strings.Contains(cypher, "RETURN DISTINCT")
	seen := map[[2]string]struct{}{}
	rows := make([]map[string]any, 0, len(g.edges))
	for _, edge := range g.edges {
		if _, ok := wanted[edge.digest]; !ok {
			continue
		}
		if distinct {
			pair := [2]string{edge.digest, edge.repositoryID}
			if _, repeated := seen[pair]; repeated {
				continue
			}
			seen[pair] = struct{}{}
		}
		rows = append(rows, map[string]any{"digest": edge.digest, "repository_id": edge.repositoryID})
	}
	g.builtFromRows += len(rows)
	return rows
}

func (*parallelEdgeTagHistoryGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// newParallelEdgeTagHistoryGraph seeds rows observations, each resolving its own
// digest that is BUILT_FROM repo-granted and repo-other, with one edge per
// (scope, evidence source) pair on each.
func newParallelEdgeTagHistoryGraph(rows int, scopes, sources []string) *parallelEdgeTagHistoryGraph {
	graph := &parallelEdgeTagHistoryGraph{}
	for i := 0; i < rows; i++ {
		digest := fmt.Sprintf("sha256:d%03d", i)
		graph.history = append(graph.history, tagHistoryRowMap(
			fmt.Sprintf("t%03d", i),
			digest,
			"",
			fmt.Sprintf("2026-06-01T%02d:%02d:00Z", i/60, i%60),
			false,
		))
		for _, repositoryID := range []string{"repo-granted", "repo-other"} {
			for _, scopeID := range scopes {
				for _, source := range sources {
					graph.edges = append(graph.edges, builtFromEdge{
						digest:         digest,
						repositoryID:   repositoryID,
						scopeID:        scopeID,
						evidenceSource: source,
					})
				}
			}
		}
	}
	return graph
}

// TestTagHistoryDistinctBoundsBuiltFromFanOut closes #6564 re-review finding 2:
// without DISTINCT, taghistory.BuiltFromCypher returned one row per BUILT_FROM
// EDGE, so the fan-out scaled with scope and evidence-source multiplicity
// rather than with distinct source repositories. A two-scope, two-source
// deployment carries four edges per image<->repository pair, and a full page of
// 200 digests each built from two repositories produced 1600 rows -- over
// taghistory.BuiltFromMaxRows, so LookupBuiltFromRepositories failed the read
// closed and the handler 500ed.
//
// The caller was entitled to every row on that page: an unscoped caller reading
// the identical page succeeded, and only the grant-bound caller the binding
// exists to serve got the error. DISTINCT collapses the parallel edges, which
// is semantically identical for the only consumer (anyRepositoryGranted needs
// set membership) and drops no granted edge.
//
// RED before the fix: status 500, "tag history BUILT_FROM lookup exceeded its
// row bound".
func TestTagHistoryDistinctBoundsBuiltFromFanOut(t *testing.T) {
	t.Parallel()

	const rows = taghistory.MaxLimit
	scopes := []string{"scope-a", "scope-b"}
	sources := []string{"oci_registry", "attestation"}
	graph := newParallelEdgeTagHistoryGraph(rows, scopes, sources)

	rawEdges := rows * 2 * len(scopes) * len(sources)
	if rawEdges <= taghistory.BuiltFromMaxRows {
		t.Fatalf(
			"seed produces %d raw edges, which is within the %d-row bound: the seed must exceed it or this test proves nothing",
			rawEdges, taghistory.BuiltFromMaxRows,
		)
	}
	identities := map[[4]string]struct{}{}
	for _, edge := range graph.edges {
		identities[edge.identity()] = struct{}{}
	}
	if got, want := len(identities), rawEdges; got != want {
		t.Fatalf(
			"seed holds %d distinct BUILT_FROM edge identities, want %d: parallel edges must differ by {scope_id, evidence_source}",
			got, want,
		)
	}

	w := serveTagHistoryAs(
		t,
		graph,
		scopedTagHistoryAuth("repo-granted"),
		fmt.Sprintf("%s&limit=%d", tagHistoryGrantTarget, rows),
	)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf(
			"status = %d, want %d: an entitled scoped caller must not be 500ed by parallel BUILT_FROM edges; body = %s",
			got, want, w.Body.String(),
		)
	}

	data := decodeTagHistoryBody(t, w)
	if got, want := len(tagHistoryResultTags(t, data)), rows; got != want {
		t.Fatalf("count = %d, want %d: every row is BUILT_FROM the granted repository", got, want)
	}
	if got := data["truncated"]; got != false {
		t.Fatalf("truncated = %#v, want false: the seeded history ends inside this page", got)
	}

	if graph.builtFromRows > taghistory.BuiltFromMaxRows {
		t.Fatalf(
			"BUILT_FROM lookup returned %d rows, above the %d-row bound (raw edges: %d)",
			graph.builtFromRows, taghistory.BuiltFromMaxRows, rawEdges,
		)
	}
	if got, want := graph.builtFromRows, rows*2; got != want {
		t.Fatalf(
			"BUILT_FROM lookup returned %d rows, want %d (one per distinct digest<->repository pair)",
			got, want,
		)
	}
	if got, want := graph.builtFromReads, 1; got != want {
		t.Fatalf("BUILT_FROM reads = %d, want %d: one lookup per window", got, want)
	}
}
