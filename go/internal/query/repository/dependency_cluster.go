// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// repositoryDependencyClusterEdgeLimit bounds the dependency-cluster edge
// pre-pass. The scoped read returns one row per admitted
// (:Repository)-[:DEPENDS_ON]->(:Repository) edge the caller is authorized to
// see, and the unscoped grouped read one row per source repository; the
// bound applies to both the row count and the flattened edge count, and
// keeps the grouping pre-pass cheap and predictable even on a dense
// whole-graph dependency set. At repo scale the repository-to-repository
// dependency edge count is far below this ceiling, so clustering stays complete
// in practice; if it is ever hit, loadRepositoryDependencyEdges reports
// Truncated so the caller discloses the read as partial (see
// repositoryDependencyEdgesDegradedReason) instead of silently presenting
// dependency-cluster membership and is_dependency as complete.
const repositoryDependencyClusterEdgeLimit = 50000

// repositoryDependencyClusterEdgeFetchLimit over-fetches one row past
// repositoryDependencyClusterEdgeLimit so a truncated read is detectable
// without a second count query, the same fetch-limit-plus-one pattern the
// repository list page query and the relationships package's RowLimit/
// FetchLimit already use.
const repositoryDependencyClusterEdgeFetchLimit = repositoryDependencyClusterEdgeLimit + 1

// repositoryDependencyEdge is one directed repository-to-repository dependency
// edge returned by the bounded edge pre-pass. Direction is irrelevant to
// clustering: the union-find pass treats edges as undirected so two
// repositories joined by a dependency in either direction land in the same
// connected component.
type repositoryDependencyEdge struct {
	Source string
	Target string
}

// repositoryDependencyClusterEdgeCypher returns the bounded per-edge Cypher
// that lists the repository-to-repository DEPENDS_ON edges a SCOPED caller
// may see. Both endpoints are anchored on the :Repository label, the
// relationship type is the fixed DEPENDS_ON, and the result is bounded by
// repositoryDependencyClusterEdgeFetchLimit. loadRepositoryDependencyEdges
// sends unscoped callers to RepositoryDependencyGroupedEdgeCypher instead.
//
// On NornicDB v1.3.3 this per-edge shape loads every :Repository and expands
// its full adjacency looking for DEPENDS_ON, so its cost grows with the
// files, workloads and other edges each repository owns: 0.635s median on a
// 500-repository graph with 200 files per repository, and 5.3-6.4s on a
// production-scale graph (#6794). The grouped read avoids that expansion but
// needs a WHERE-free statement, and a scoped caller's grant is a WHERE
// predicate, so scoped callers keep this shape and still pay that
// expansion. See RepositoryDependencyGroupedEdgeCypher and
// docs/internal/evidence/6786-repository-dependency-marker-and-relationship-repo-anchor.md.
//
// Keep both endpoint labels. Dropping them to seed from unlabelled endpoints
// is slower, and moving them into `WHERE s:Repository AND t:Repository` is
// wrong on NornicDB v1.3.3: the label predicate is ignored and every
// DEPENDS_ON edge comes back, including Workload-to-Workload edges (measured
// 2,317 rows against the true 300). The focused string tests in
// dependency_cluster_test.go assert "(s:Repository)-[:DEPENDS_ON]->(t:Repository)"
// verbatim; loadRepositoryDependencyEdges is a non_hot callsite in
// queryplan/testdata/query-source-coverage.yaml, so the queryplan
// validator's unlabeledMatchPattern check does not gate this query.
//
// For a scoped caller the same tenant predicate that guards the repository list
// is applied to BOTH the source and target repository, so a scoped caller can
// only observe cluster membership formed entirely from edges within their
// grant. A dependency or depender outside the grant cannot pull an in-grant
// repository into a cross-grant cluster. For shared/admin/local (allScopes)
// callers no predicate is added and the whole-graph DEPENDS_ON edge set is
// eligible.
//
// Every evidence source that projects (:Repository)-[:DEPENDS_ON]->(:Repository)
// — the resolver/cross-repo terraform edges, the projection/package-consumption
// edges, and the projection/code-imports edges — is consumed uniformly because
// clustering keys on the edge type, not its evidence_source.
func repositoryDependencyClusterEdgeCypher(access querycontract.RepositoryAccessFilter) string {
	where := ""
	if access.Scoped() {
		where = fmt.Sprintf(
			"\n\t\tWHERE %s AND %s",
			access.GraphCondition("s"),
			access.GraphCondition("t"),
		)
	}
	return fmt.Sprintf(`
		MATCH (s:Repository)-[:DEPENDS_ON]->(t:Repository)%s
		RETURN s.id AS source_id, t.id AS target_id
		ORDER BY source_id, target_id
		LIMIT %d
	`, where, repositoryDependencyClusterEdgeFetchLimit)
}

// repositoryDependencyEdgeRead is the outcome of the bounded dependency-edge
// pre-pass: the parsed edges (clipped to repositoryDependencyClusterEdgeLimit
// when the read hit the bound), whether the read was Truncated, and any
// graph-read Err. Truncated and Err exist so a caller discloses an
// incomplete read -- via logRepositoryDependencyEdgesDegradation and
// repositoryDependencyEdgesDegradedReason -- instead of silently presenting
// dependency-cluster membership or is_dependency as complete and
// authoritative when it might not be. Edges is nil (not merely empty) only
// when graph is nil or Err is set, matching the prior nil-on-error contract
// callers that only inspect Edges already rely on.
//
// Skipped reports that the RepositoryDependencyEdgeCountCypher probe proved
// the graph holds no DEPENDS_ON edges, so the edge scan never ran; that is
// complete evidence (every is_dependency is false, no clusters), not a
// degraded read. ProbeErr carries a probe failure for telemetry only: the scan
// still runs after it, so it never degrades the response.
type repositoryDependencyEdgeRead struct {
	Edges     []repositoryDependencyEdge
	Truncated bool
	Err       error
	Skipped   bool
	ProbeErr  error
}

// loadRepositoryDependencyEdges runs the bounded, correctly-scoped
// (:Repository)-[:DEPENDS_ON]->(:Repository) edge pre-pass and returns the
// parsed edges plus truncation/error evidence. On query error it returns no
// edges (callers degrade to "no known edges": non-cluster grouping,
// is_dependency=false) rather than failing the whole repository list --
// see logRepositoryDependencyEdgesDegradation's doc comment for why that
// degrade-not-fail choice still requires disclosure, not silence.
//
// This is the single graph read that backs both dependency-cluster grouping
// (buildRepositoryDependencyClusters) and the is_dependency marker
// (repositoryDependencyTargetSet) -- issue #6786 defect 1 replaced a
// per-row `EXISTS { MATCH (r)<-[:DEPENDS_ON]-(dep:Repository)... }` RETURN
// expression (always false on NornicDB v1.3.3, and invalid Cypher on Neo4j
// when the scoped grant predicate was spliced in) with this already-bounded,
// already-scoped edge read, computed once in Go for both purposes.
//
// Unscoped callers first run the DEPENDS_ON cardinality probe and then the
// grouped read (RepositoryDependencyGroupedEdgeCypher); scoped callers run
// only the grant-predicated per-edge read (repositoryDependencyClusterEdgeCypher).
// Both yield edges sorted by (source, target) and clipped to the same bound.
func loadRepositoryDependencyEdges(ctx context.Context, graph querycontract.GraphQuery, access querycontract.RepositoryAccessFilter) repositoryDependencyEdgeRead {
	if graph == nil {
		return repositoryDependencyEdgeRead{}
	}
	var probeErr error
	// The probe is unscoped, so it only runs for callers who may already see
	// the whole graph. Scoped callers never issue a statement without their
	// grant predicate; they go straight to the scoped edge scan.
	if !access.Scoped() {
		noEdges, err := probeRepositoryDependencyEdgesAbsent(ctx, graph)
		if err != nil {
			probeErr = err
		} else if noEdges {
			return repositoryDependencyEdgeRead{Edges: []repositoryDependencyEdge{}, Skipped: true}
		}
		// Unscoped callers take the grouped read NornicDB answers from the
		// DEPENDS_ON relationship-type index; see
		// RepositoryDependencyGroupedEdgeCypher for the measurement.
		rows, err := readGroupedRepositoryDependencyEdges(ctx, graph)
		if err != nil {
			return repositoryDependencyEdgeRead{Err: err, ProbeErr: probeErr}
		}
		edges, truncated := flattenGroupedRepositoryDependencyEdges(rows, repositoryDependencyClusterEdgeLimit)
		return repositoryDependencyEdgeRead{Edges: edges, Truncated: truncated, ProbeErr: probeErr}
	}
	rows, err := graph.Run(ctx, repositoryDependencyClusterEdgeCypher(access), access.GraphParams(nil))
	if err != nil {
		return repositoryDependencyEdgeRead{Err: err, ProbeErr: probeErr}
	}
	truncated := len(rows) > repositoryDependencyClusterEdgeLimit
	if truncated {
		rows = rows[:repositoryDependencyClusterEdgeLimit]
	}
	edges := make([]repositoryDependencyEdge, 0, len(rows))
	for _, row := range rows {
		source := querycontract.StringVal(row, "source_id")
		target := querycontract.StringVal(row, "target_id")
		if source == "" || target == "" {
			continue
		}
		edges = append(edges, repositoryDependencyEdge{Source: source, Target: target})
	}
	return repositoryDependencyEdgeRead{Edges: edges, Truncated: truncated, ProbeErr: probeErr}
}

// repositoryDependencyEdgesDegradedReason is the partial_reasons (repository
// list) / limitations (catalog) entry added when the dependency-edge
// pre-pass failed or was truncated. Its presence tells a caller that an
// is_dependency=false or missing dependency_cluster group on this response
// may be incomplete evidence rather than a confirmed negative -- the same
// "affirmative-false-claim" disclosure discipline #5764 established for the
// repository story's infrastructure-panel truncation.
const repositoryDependencyEdgesDegradedReason = "dependency_marker_evidence_incomplete"

// logRepositoryDependencyEdgesDegradation logs a structured warning and
// reports whether the caller must disclose degraded is_dependency /
// dependency-cluster evidence. The caller appends
// repositoryDependencyEdgesDegradedReason to partial_reasons (repository list)
// or limitations (catalog) and never folds it into truncated, which means
// "more repositories exist beyond this page" (#6786 review F1; see
// resolveRepositoryDependencyEvidence).
//
// The pre-pass degrades rather than fails the request on error or
// truncation: both listRepositories and listCatalog already treat it as a
// best-effort secondary signal layered onto an otherwise-complete page of
// repository rows (predates this function; see loadRepositoryDependencyEdges),
// and turning a healthy primary read into a 5xx over this auxiliary edge
// count would be a larger regression than an honestly disclosed partial
// answer. Accuracy is preserved by disclosure -- this log plus the response's
// partial_reasons/limitations entry -- not by manufacturing a failure for a
// request whose primary rows are otherwise complete. Applies identically to
// scoped and unscoped callers; nothing here branches on RepositoryAccessFilter.
func logRepositoryDependencyEdgesDegradation(ctx context.Context, logger *slog.Logger, operation string, result repositoryDependencyEdgeRead) bool {
	degraded := result.Err != nil || result.Truncated
	if !degraded || logger == nil {
		return degraded
	}
	logger.WarnContext(
		ctx, "repository query dependency-edge pre-pass degraded",
		telemetry.EventAttr("repository_query.dependency_edges_degraded"),
		log.Operation(operation),
		slog.Int("edge_count", len(result.Edges)),
		slog.Bool("truncated", result.Truncated),
		slog.Bool("error", result.Err != nil),
	)
	return true
}

// repositoryDependencyEvidence bundles the repository list's dependency-edge
// pre-pass results: connected-component Clusters (issue #3504), the
// is_dependency Targets set (issue #6786 defect 1), and any
// ExtraPartialReasons the degradation discipline requires (#6786 review F1
// -- never truncated).
type repositoryDependencyEvidence struct {
	Clusters            map[string]string
	Targets             map[string]struct{}
	ExtraPartialReasons []string
}

// resolveRepositoryDependencyEvidence runs one bounded edge query over
// (:Repository)-[:DEPENDS_ON]->(:Repository) and uses it for two purposes:
// connected-component clustering (the primary grouping signal, issue
// #3504) and the is_dependency marker (issue #6786 defect 1 -- replaces a
// per-row EXISTS-as-RETURN-expression that NornicDB v1.3.3 always
// evaluates false, and whose scoped grant predicate was invalid Cypher on
// Neo4j). Repositories that depend on each other share a cluster key,
// which the caller's per-row decoration gives precedence over the
// source-backed slug/owner/flag derivation; repositories in no dependency
// edge fall through to honest missing_evidence rather than a name
// heuristic.
//
// The pre-pass is instrumented with the existing stage timer so operators
// can diagnose its duration and edge count from the
// repository_query.stage_started / repository_query.stage_completed log
// events (operation=repository_list, stage=dependency_cluster_edges).
//
// A failed or truncated pre-pass degrades rather than fails this
// otherwise-healthy page (see logRepositoryDependencyEdgesDegradation's
// doc comment for why), but must not silently present is_dependency or
// dependency_cluster grouping as complete. Disclosure is via
// ExtraPartialReasons ONLY -- the caller MUST NOT fold it into truncated
// (or, downstream, result_limits.truncated / the
// repository_inventory_truncated reason): those fields mean "more
// repositories exist beyond this returned page" (see the
// OpenAPI/HTTP-API-reference contract for GET /api/v0/repositories), an
// unrelated claim about the PAGE that the dependency-edge pre-pass has no
// bearing on. A complete, non-truncated page whose auxiliary dependency
// evidence is incomplete must still report truncated=false -- otherwise a
// pager or agent that follows "truncated" asks for a next page that does
// not exist (#6786 review F1).
func resolveRepositoryDependencyEvidence(
	ctx context.Context,
	logger *slog.Logger,
	graph querycontract.GraphQuery,
	access querycontract.RepositoryAccessFilter,
) repositoryDependencyEvidence {
	clusterTimer := startRepositoryQueryStage(ctx, logger, "repository_list", "", "dependency_cluster_edges")
	dependencyRead := loadRepositoryDependencyEdges(ctx, graph, access)
	clusters := buildRepositoryDependencyClusters(dependencyRead.Edges)
	targets := repositoryDependencyTargetSet(dependencyRead.Edges)
	clusterTimer.Done(
		ctx,
		slog.Int("cluster_count", len(clusters)),
		slog.Int("edge_count", len(dependencyRead.Edges)),
		slog.Bool("truncated", dependencyRead.Truncated),
		slog.Bool("error", dependencyRead.Err != nil),
		slog.Bool("edge_scan_skipped", dependencyRead.Skipped),
	)
	logRepositoryDependencyClusterErrors(ctx, logger, "repository_list", dependencyRead)

	evidence := repositoryDependencyEvidence{Clusters: clusters, Targets: targets}
	if logRepositoryDependencyEdgesDegradation(ctx, logger, "repository_list", dependencyRead) {
		evidence.ExtraPartialReasons = append(evidence.ExtraPartialReasons, repositoryDependencyEdgesDegradedReason)
	}
	return evidence
}

// repositoryDependencyTargetSet returns the set of repository ids that are
// the target of at least one edge in edges -- i.e. the repositories some
// other (grant-admitted) repository depends on. This is the is_dependency
// marker: "True when at least one other repository depends on this one, i.e.
// it is the target of an admitted Repository-[:DEPENDS_ON]->Repository edge"
// (openapi components.go). Deriving it here from the edges the caller already
// fetched for clustering avoids a second graph round trip and keeps both
// signals consistent with the same scoped, bounded edge read.
func repositoryDependencyTargetSet(edges []repositoryDependencyEdge) map[string]struct{} {
	targets := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		targets[edge.Target] = struct{}{}
	}
	return targets
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
	if key, ok := clusters[querycontract.StringVal(repo, "id")]; ok {
		repo["group_key"] = key
		repo["group_source"] = repositoryGroupSourceDependencyCluster
		repo["group_truth"] = repositoryGroupTruthDerived
		repo["group_kind"] = "cluster"
		repo["group_reason"] = "grouped with repositories it transitively depends on or that depend on it (connected component over Repository DEPENDS_ON edges)"
		return repo
	}
	return decorateRepositoryGroupEvidence(repo)
}
