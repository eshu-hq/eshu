// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"fmt"
	"strings"

	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	canonicalPhaseAdvisoryGroup = "advisory_group"
	advisoryGroupLabel          = "AdvisoryGroup"
)

// AdvisoryGroupUpsertCypher reconciles one advisory group's membership in
// UNWIND batches (issue #10411).
//
// The group node MERGEs on id alone, backed by the advisory_group_id unique
// constraint, so re-projecting the same group converges on one node. Every
// other node property is SET, never part of the identity: the CVE and
// advisory lists and the member count are rewritten in place.
//
// The Repository is MATCHed, never MERGEd: an edge whose repository node is
// absent (retired, or never projected) contributes no edge and fabricates no
// stub. The edge MERGE keys on package_id, the (repository, package) pair
// identity the advisorygroup.Edge contract fixes; ecosystem and purl are the
// only mutable stamps. No evidence_source, scope, or generation stamps exist
// on the node or the edge by design: membership only.
//
// The statement opens UNWIND-first like every sibling upsert, so it never
// takes the WITH-then-UNWIND shape NornicDB drops (upstream #408).
const AdvisoryGroupUpsertCypher = `UNWIND $rows AS row
MATCH (r:Repository {id: row.repository_id})
MERGE (g:AdvisoryGroup {id: $id})
FOREACH (_ IN CASE WHEN coalesce(g.cve_ids, []) <> $cve_ids OR coalesce(g.advisory_ids, []) <> $advisory_ids OR coalesce(g.member_count, -1) <> $member_count THEN [1] ELSE [] END |
  SET g.cve_ids = $cve_ids,
      g.advisory_ids = $advisory_ids,
      g.member_count = $member_count)
MERGE (r)-[e:AFFECTED_BY {package_id: row.package_id}]->(g)
FOREACH (_ IN CASE WHEN coalesce(e.ecosystem, '') <> row.ecosystem OR coalesce(e.purl, '') <> row.purl THEN [1] ELSE [] END |
  SET e.ecosystem = row.ecosystem,
      e.purl = row.purl)`

// AdvisoryGroupStaleEdgeRetractCypher deletes the group's AFFECTED_BY edges
// outside the current membership. It seeks the group by its unique id, so it
// never scans the AdvisoryGroup label; the keep list is built from the same
// group.Edges the upsert wrote, so a full reconcile retracts nothing.
//
// The keep check is pair-exact: a repository and a package that each appear
// in different kept pairs do not keep their cross pair alive. The ANY scans
// the bound $keep parameter (bounded by the group's own degree), never an
// UNWIND variable, keeping the shape outside the string-concat and
// UNWIND-operand dialect traps. The predicate complements the parameter, so
// this statement must never ride ExecuteGroup: the retry classifier keeps
// complement-of-parameter retracts terminal, and this writer dispatches
// sequentially like the RuntimeDeclarationWriter retracts.
const AdvisoryGroupStaleEdgeRetractCypher = `MATCH (g:AdvisoryGroup {id: $id})<-[e:AFFECTED_BY]-(r:Repository)
WHERE NOT ANY(k IN $keep WHERE k.repository_id = r.id AND k.package_id = e.package_id)
DELETE e`

// AdvisoryGroupDeleteEdgelessCypher removes the group node when no
// AFFECTED_BY edge remains — after a stale-edge retract took the last edge,
// or when every qualifying repository was absent from the graph. Anchored
// by unique id; the OPTIONAL MATCH keeps the shape portable.
const AdvisoryGroupDeleteEdgelessCypher = `MATCH (g:AdvisoryGroup {id: $id})
OPTIONAL MATCH (g)<-[e:AFFECTED_BY]-()
WITH g, count(e) AS degree
WHERE degree = 0
DELETE g`

// AdvisoryGroupEmptyRetractCypher removes the group node and its edges when
// a reconcile carries no membership. It deletes the AFFECTED_BY edges
// first, then the node, naming the type it reaps so the materialized-edge
// retract registry can pin the family against this template (#6228
// retract-registry contract). No other writer MERGEs the AdvisoryGroup
// label, so deleting the node cannot strand another family's edges; the
// OPTIONAL MATCH keeps an already-edgeless node on the same path.
const AdvisoryGroupEmptyRetractCypher = `MATCH (g:AdvisoryGroup {id: $id})
OPTIONAL MATCH (g)<-[e:AFFECTED_BY]-()
DELETE e
WITH DISTINCT g
DELETE g`

// AdvisoryGroupWriter reconciles advisory-group membership: one
// AdvisoryGroup node plus its Repository-[:AFFECTED_BY]-> edges per group.
type AdvisoryGroupWriter struct {
	executor  sourcecypher.Executor
	batchSize int
}

// NewAdvisoryGroupWriter returns an AdvisoryGroupWriter backed by the given
// Executor. A batchSize of 0 or less uses sourcecypher.DefaultBatchSize.
func NewAdvisoryGroupWriter(executor sourcecypher.Executor, batchSize int) *AdvisoryGroupWriter {
	if batchSize <= 0 {
		batchSize = sourcecypher.DefaultBatchSize
	}
	return &AdvisoryGroupWriter{executor: executor, batchSize: batchSize}
}

// ReconcileAdvisoryGroup writes group's membership: upsert batches for every
// edge, then one group-anchored retract of edges outside the group. An empty
// edge list issues only the group DETACH DELETE. The reconcile is idempotent:
// replaying the same group writes the same node and edges.
//
// WriteStats sums the driver result summaries of every statement executed,
// through the call's write-counts collector the same way the production Bolt
// seam publishes them. Statements no executor reported counters for (test
// fakes) contribute nothing.
func (w *AdvisoryGroupWriter) ReconcileAdvisoryGroup(
	ctx context.Context,
	group advisorygroup.Group,
) (advisorygroup.WriteStats, error) {
	if w.executor == nil {
		return advisorygroup.WriteStats{}, fmt.Errorf("advisory group writer executor is required")
	}
	if strings.TrimSpace(group.ID) == "" {
		return advisorygroup.WriteStats{}, fmt.Errorf("advisory group id is required")
	}
	collector := sourcecypher.NewFilteredWriteCountsCollector(
		sourcecypher.WriteCountsCollectorFromContext(ctx),
		func(sourcecypher.WriteCountEntry) bool { return true },
	)
	ctx = sourcecypher.WithWriteCountsCollector(ctx, collector)

	if len(group.Edges) == 0 {
		if err := w.execute(ctx, []sourcecypher.Statement{{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher:    AdvisoryGroupEmptyRetractCypher,
			Parameters: map[string]any{
				"id": group.ID,
			},
		}}); err != nil {
			return advisorygroup.WriteStats{}, err
		}
		return collectAdvisoryGroupStats(collector), nil
	}

	rows := make([]map[string]any, 0, len(group.Edges))
	keep := make([]map[string]any, 0, len(group.Edges))
	for _, edge := range group.Edges {
		rows = append(rows, map[string]any{
			"repository_id": edge.RepositoryID,
			"package_id":    edge.PackageID,
			"ecosystem":     edge.Ecosystem,
			"purl":          edge.PURL,
		})
		keep = append(keep, map[string]any{
			"repository_id": edge.RepositoryID,
			"package_id":    edge.PackageID,
		})
	}
	batches := sourcecypher.BuildBatchedStatements(AdvisoryGroupUpsertCypher, rows, w.batchSize)
	for index := range batches {
		batchRows := batches[index].Parameters["rows"].([]map[string]any)
		batches[index].Parameters["id"] = group.ID
		batches[index].Parameters["cve_ids"] = group.CVEIDs
		batches[index].Parameters["advisory_ids"] = group.AdvisoryIDs
		batches[index].Parameters["member_count"] = len(group.CVEIDs) + len(group.AdvisoryIDs)
		batches[index].Parameters[sourcecypher.StatementMetadataPhaseKey] = canonicalPhaseAdvisoryGroup
		batches[index].Parameters[sourcecypher.StatementMetadataEntityLabelKey] = advisoryGroupLabel
		batches[index].Parameters[sourcecypher.StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=%s rows=%d", advisoryGroupLabel, len(batchRows),
		)
	}
	batches = append(batches, sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher:    AdvisoryGroupStaleEdgeRetractCypher,
		Parameters: map[string]any{
			"id":   group.ID,
			"keep": keep,
		},
	}, sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher:    AdvisoryGroupDeleteEdgelessCypher,
		Parameters: map[string]any{
			"id": group.ID,
		},
	})
	if err := w.execute(ctx, batches); err != nil {
		return advisorygroup.WriteStats{}, err
	}
	return collectAdvisoryGroupStats(collector), nil
}

// collectAdvisoryGroupStats sums the collected per-statement Bolt counters
// into WriteStats. A nil collector (capture was somehow skipped) reports
// zeros rather than failing the reconcile.
func collectAdvisoryGroupStats(collector *sourcecypher.WriteCountsCollector) advisorygroup.WriteStats {
	stats := advisorygroup.WriteStats{}
	if collector == nil {
		return stats
	}
	for _, entry := range collector.Entries() {
		stats.NodesCreated += int(entry.Counters.NodesCreated)
		stats.NodesDeleted += int(entry.Counters.NodesDeleted)
		stats.EdgesCreated += int(entry.Counters.RelationshipsCreated)
		stats.EdgesDeleted += int(entry.Counters.RelationshipsDeleted)
		stats.PropertiesSet += int(entry.Counters.PropertiesSet)
	}
	return stats
}

// execute runs statements one at a time through Execute. The stale-edge
// retract complements its $keep parameter, so it must not ride ExecuteGroup
// (see the Cypher constant); the upsert batches are independent
// per-group rows, so sequential auto-commit costs no correctness.
func (w *AdvisoryGroupWriter) execute(ctx context.Context, stmts []sourcecypher.Statement) error {
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return sourcecypher.WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
