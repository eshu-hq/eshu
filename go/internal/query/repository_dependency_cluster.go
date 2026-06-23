package query

import (
	"context"
	"fmt"
)

// repositoryDependencyClusterEdgeLimit bounds the dependency-cluster edge
// pre-pass. The query returns one row per admitted
// (:Repository)-[:DEPENDS_ON]->(:Repository) edge the caller is authorized to
// see; the bound keeps the grouping pre-pass cheap and predictable even on a
// dense whole-graph dependency set. At repo scale the repository-to-repository
// dependency edge count is far below this ceiling, so clustering stays complete
// in practice; if it is ever hit the missing edges simply leave some repos in
// the honest non-cluster path rather than inventing membership.
const repositoryDependencyClusterEdgeLimit = 50000

// repositoryDependencyEdge is one directed repository-to-repository dependency
// edge returned by the bounded edge pre-pass. Direction is irrelevant to
// clustering: the union-find pass treats edges as undirected so two
// repositories joined by a dependency in either direction land in the same
// connected component.
type repositoryDependencyEdge struct {
	Source string
	Target string
}

// repositoryDependencyClusterEdgeCypher returns the bounded Cypher that lists
// the repository-to-repository DEPENDS_ON edges used to compute dependency
// clusters. Both endpoints are anchored on the :Repository label and the
// relationship type is the fixed DEPENDS_ON, so the planner seeds the scan from
// the id-indexed Repository population rather than an all-node scan. The result
// is bounded by repositoryDependencyClusterEdgeLimit.
//
// For a scoped caller the same tenant predicate that guards the repository list
// is applied to BOTH the source and target repository, so a scoped caller can
// only observe cluster membership formed entirely from edges within their
// grant. A dependency or depender outside the grant cannot pull an in-grant
// repository into a cross-grant cluster. For shared/admin/local (allScopes)
// callers no predicate is added and the whole-graph DEPENDS_ON edge set is
// eligible.
//
// Both evidence sources that project (:Repository)-[:DEPENDS_ON]->(:Repository)
// — the resolver/cross-repo terraform edges and the projection/package-consumption
// edges — are consumed uniformly because clustering keys on the edge type, not
// its evidence_source.
func repositoryDependencyClusterEdgeCypher(access repositoryAccessFilter) string {
	where := ""
	if access.scoped() {
		where = fmt.Sprintf(
			"\n\t\tWHERE %s AND %s",
			access.graphCondition("s"),
			access.graphCondition("t"),
		)
	}
	return fmt.Sprintf(`
		MATCH (s:Repository)-[:DEPENDS_ON]->(t:Repository)%s
		RETURN s.id AS source_id, t.id AS target_id
		ORDER BY source_id, target_id
		LIMIT %d
	`, where, repositoryDependencyClusterEdgeLimit)
}

// loadRepositoryDependencyClusters runs the bounded edge pre-pass and returns a
// map from repository id to its dependency-cluster key (the lexicographically
// smallest repository id in the connected component). Repositories that do not
// participate in any in-scope DEPENDS_ON edge are absent from the map and fall
// through to the non-cluster grouping path. On query error it returns an empty
// map so the caller degrades to non-cluster grouping rather than failing the
// whole repository list.
func loadRepositoryDependencyClusters(ctx context.Context, graph GraphQuery, access repositoryAccessFilter) map[string]string {
	if graph == nil {
		return map[string]string{}
	}
	rows, err := graph.Run(ctx, repositoryDependencyClusterEdgeCypher(access), access.graphParams(nil))
	if err != nil {
		return map[string]string{}
	}
	edges := make([]repositoryDependencyEdge, 0, len(rows))
	for _, row := range rows {
		source := StringVal(row, "source_id")
		target := StringVal(row, "target_id")
		if source == "" || target == "" {
			continue
		}
		edges = append(edges, repositoryDependencyEdge{Source: source, Target: target})
	}
	return buildRepositoryDependencyClusters(edges)
}

// buildRepositoryDependencyClusters computes connected components over the
// undirected dependency graph using union-find and returns a map from each
// participating repository id to its cluster key. The cluster key is the
// lexicographically smallest repository id in the component, which is stable
// across page boundaries and independent of edge or row ordering.
//
// Cycles (A->B->A) and self-loops (E->E) are handled naturally: union is
// idempotent, so repeated or reflexive edges never change membership and never
// loop. A repository touched only by a self-loop forms a single-node cluster
// keyed by its own id, because it genuinely participates in a DEPENDS_ON edge.
func buildRepositoryDependencyClusters(edges []repositoryDependencyEdge) map[string]string {
	uf := newRepositoryUnionFind()
	for _, edge := range edges {
		uf.add(edge.Source)
		uf.add(edge.Target)
		uf.union(edge.Source, edge.Target)
	}

	clusters := make(map[string]string, len(uf.parent))
	for id := range uf.parent {
		clusters[id] = uf.find(id)
	}
	return clusters
}

// repositoryUnionFind is a disjoint-set structure keyed by repository id. It
// uses union-by-min so the representative of every set is the lexicographically
// smallest id, giving a deterministic, page-stable cluster key without a
// separate reduction pass.
type repositoryUnionFind struct {
	parent map[string]string
}

func newRepositoryUnionFind() *repositoryUnionFind {
	return &repositoryUnionFind{parent: make(map[string]string)}
}

func (uf *repositoryUnionFind) add(id string) {
	if _, ok := uf.parent[id]; !ok {
		uf.parent[id] = id
	}
}

// find returns the representative (smallest id) of id's set with path
// compression.
func (uf *repositoryUnionFind) find(id string) string {
	root := id
	for uf.parent[root] != root {
		root = uf.parent[root]
	}
	for uf.parent[id] != root {
		uf.parent[id], id = root, uf.parent[id]
	}
	return root
}

// union merges the sets containing a and b, keeping the lexicographically
// smaller representative as the root so the cluster key is deterministic.
func (uf *repositoryUnionFind) union(a, b string) {
	ra, rb := uf.find(a), uf.find(b)
	if ra == rb {
		return
	}
	if ra < rb {
		uf.parent[rb] = ra
	} else {
		uf.parent[ra] = rb
	}
}

// decorateRepositoryGroupEvidenceWithClusters assigns grouping evidence to a
// repository row, giving the dependency cluster precedence over every other
// source. When the repository id is present in clusters it is grouped as a
// dependency cluster (the primary grouping signal for issue #3504); otherwise
// it falls through to the existing source-backed derivation, which ends in
// honest missing_evidence rather than a repository-name heuristic.
func decorateRepositoryGroupEvidenceWithClusters(repo map[string]any, clusters map[string]string) map[string]any {
	if key, ok := clusters[StringVal(repo, "id")]; ok {
		repo["group_key"] = key
		repo["group_source"] = repositoryGroupSourceDependencyCluster
		repo["group_truth"] = repositoryGroupTruthDerived
		repo["group_kind"] = "cluster"
		repo["group_reason"] = "grouped with repositories it transitively depends on or that depend on it (connected component over Repository DEPENDS_ON edges)"
		return repo
	}
	return decorateRepositoryGroupEvidence(repo)
}
