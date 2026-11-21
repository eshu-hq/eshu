// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"fmt"
	"maps"
	"strings"

	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	canonicalPhaseAdvisoryGroup = "advisory_group"
	advisoryGroupLabel          = "AdvisoryGroup"
)

// AdvisoryGroupNodeCreateCypher MERGEs the group's node on id alone,
// backed by the advisory_group_id unique constraint, so re-projecting the
// same group converges on one node, and stamps the CVE and advisory lists
// and the member count exactly when the MERGE creates the node. Every other
// node property is SET, never part of the identity. A matched node keeps
// its stamps here; the update statement below resyncs them.
//
// ON CREATE keeps creation unconditional and replay-safe without any
// conditional clause: on a matched node the clause is a no-op, so a replay
// sets zero properties (#10411 criterion 11). It is FOREACH-free by design:
// NornicDB cannot parse a FOREACH clause between or after MERGEs, so the
// pre-#11161 single-statement upsert dead-lettered on the gate-default
// backend ("UNWIND MATCH failed: end node variable ..."). The statement
// carries no UNWIND at all, so it never takes the WITH-then-UNWIND shape
// NornicDB drops (upstream #408).
const AdvisoryGroupNodeCreateCypher = `MERGE (g:AdvisoryGroup {id: $id})
ON CREATE SET g.cve_ids = $cve_ids, g.advisory_ids = $advisory_ids, g.member_count = $member_count`

// AdvisoryGroupNodeUpdateCypher resyncs the group node's stamps when the
// membership changed after the node was created. The WHERE gate skips the
// SET when every value already matches, so a replay sets zero properties
// (#10411 criterion 11).
//
// The gate is MATCH-WITH-WHERE by design: NornicDB rejects a WHERE directly
// after MATCH when the predicate compares list parameters ("RETURN clause
// contains empty expression"), while the WITH-carried form is proven on
// both backends. No MERGE precedes the gate: a WITH-WHERE gate after a MERGE
// is unreliable on NornicDB (the #11161 investigation found the node SET
// silently skipped and the replay counter wrong).
const AdvisoryGroupNodeUpdateCypher = `MATCH (g:AdvisoryGroup {id: $id})
WITH g WHERE coalesce(g.cve_ids, []) <> $cve_ids OR coalesce(g.advisory_ids, []) <> $advisory_ids OR coalesce(g.member_count, -1) <> $member_count
SET g.cve_ids = $cve_ids, g.advisory_ids = $advisory_ids, g.member_count = $member_count`

// AdvisoryGroupEdgeMergeCypher reconciles one advisory group's membership
// in UNWIND batches (issue #10411).
//
// The Repository is MATCHed, never MERGEd: an edge whose repository node is
// absent (retired, or never projected) contributes no edge and fabricates no
// stub. The group node is MERGEd on id alone: the node create above already
// guarantees it, so this MERGE always matches, but the MERGE takes the
// exclusive node lock that keeps every concurrent reconcile acquiring locks
// in the same repository, group, edge order. A MATCH would leave a shared
// lock that a racing duplicate upgrades into a mutual-wait deadlock on
// Neo4j (the #11161 parallel convergence proof pins this). The edge MERGE
// keys on package_id, the (repository, package) pair identity the
// advisorygroup.Edge contract fixes. No evidence_source, scope, or
// generation stamps exist on the node or the edge by design: membership
// only. Mutable stamps (ecosystem, purl) are left unset here; the update
// statement below stamps them.
//
// MERGE-only keeps creation unconditional and replay-safe with no clause
// after the MERGE at all: a matched edge is a no-op. An ON CREATE SET was
// tried and reverted: NornicDB silently skips ON CREATE after an
// UNWIND-driven MERGE (edge created but unstamped, counters zero), so stamps
// stay in the update statement on every backend. FOREACH-free by design,
// same as the node statements (#11161). The statement opens UNWIND-first
// like every sibling upsert, so it never takes the WITH-then-UNWIND shape
// NornicDB drops (upstream #408).
const AdvisoryGroupEdgeMergeCypher = `UNWIND $rows AS row
MATCH (r:Repository {id: row.repository_id})
MERGE (g:AdvisoryGroup {id: $id})
MERGE (r)-[e:AFFECTED_BY {package_id: row.package_id}]->(g)`

// AdvisoryGroupEdgeUpdateCypher stamps the edge's mutable ecosystem and purl
// when they changed after the edge was merged. The WHERE gate skips the SET
// when both stamps already match, so a replay sets zero properties (#10411
// criterion 11).
//
// The gate is MATCH-WHERE by design, with no WITH in between: NornicDB
// ignores a WHERE after WITH on this shape, while the WITH-free form is
// proven on both backends (#11161). No MERGE precedes the gate.
const AdvisoryGroupEdgeUpdateCypher = `UNWIND $rows AS row
MATCH (r:Repository {id: row.repository_id})-[e:AFFECTED_BY {package_id: row.package_id}]->(g:AdvisoryGroup {id: $id})
WHERE coalesce(e.ecosystem, '') <> row.ecosystem OR coalesce(e.purl, '') <> row.purl
SET e.ecosystem = row.ecosystem, e.purl = row.purl`

// AdvisoryGroupStaleEdgeRetractCypher deletes the group's AFFECTED_BY edges
// outside the current membership. It seeks the group by its unique id, so it
// never scans the AdvisoryGroup label; the keep list is built from the same
// group.Edges the upsert wrote, so a full reconcile retracts nothing.
//
// The keep check is pair-exact: each kept (repository, package) pair is
// flattened into one "repository_id|package_id" composite key (the same
// separator the advisory parity proof uses), so a repository and a package
// that each appear in different kept pairs do not keep their cross pair
// alive. The IN scans the bound $keepkeys parameter (bounded by the group's
// own degree), never an UNWIND variable. The composite form is deliberate:
// NornicDB never matches ANY over a parameter list of maps (the pre-#11161
// shape kept every edge on the gate-default backend), while IN over a flat
// string list is proven on both backends. The predicate complements the
// parameter, so this statement must never ride ExecuteGroup: the retry
// classifier keeps complement-of-parameter retracts terminal, and this
// writer dispatches sequentially like the RuntimeDeclarationWriter retracts.
const AdvisoryGroupStaleEdgeRetractCypher = `MATCH (g:AdvisoryGroup {id: $id})<-[e:AFFECTED_BY]-(r:Repository)
WHERE NOT (r.id + '|' + e.package_id) IN $keepkeys
DELETE e`

// AdvisoryGroupDeleteEdgelessCypher removes the group node when no
// AFFECTED_BY edge remains — after a stale-edge retract took the last edge,
// or when every qualifying repository was absent from the graph. Anchored
// by unique id; the WHERE-NOT pattern keeps the shape portable. The earlier
// OPTIONAL-MATCH-plus-count form evaluated its DELETE on NornicDB even for
// groups that still had edges (#11161), so the count gate must never come
// back.
const AdvisoryGroupDeleteEdgelessCypher = `MATCH (g:AdvisoryGroup {id: $id})
WHERE NOT (g)<-[:AFFECTED_BY]-()
DELETE g`

// AdvisoryGroupEmptyRetractCypher removes the group node and its edges when
// a reconcile carries no membership. It deletes the AFFECTED_BY edges and
// the node in one clause, naming the type it reaps so the materialized-edge
// retract registry can pin the family against this template (#6228
// retract-registry contract). No other writer MERGEs the AdvisoryGroup
// label, so deleting the node cannot strand another family's edges; the
// OPTIONAL MATCH keeps an already-edgeless node on the same path. The
// earlier split form (DELETE e, then WITH DISTINCT g, then DELETE g) left
// the node behind on NornicDB (#11161), so the two deletes must stay in one
// clause.
const AdvisoryGroupEmptyRetractCypher = `MATCH (g:AdvisoryGroup {id: $id})
OPTIONAL MATCH (g)<-[e:AFFECTED_BY]-()
DELETE e, g`

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
// edge list issues only the group edge-delete then node-delete. The reconcile is idempotent:
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
	keepkeys := make([]string, 0, len(group.Edges))
	for _, edge := range group.Edges {
		rows = append(rows, map[string]any{
			"repository_id": edge.RepositoryID,
			"package_id":    edge.PackageID,
			"ecosystem":     edge.Ecosystem,
			"purl":          edge.PURL,
		})
		keepkeys = append(keepkeys, edge.RepositoryID+"|"+edge.PackageID)
	}
	nodeParams := map[string]any{
		"id":           group.ID,
		"cve_ids":      group.CVEIDs,
		"advisory_ids": group.AdvisoryIDs,
		"member_count": len(group.CVEIDs) + len(group.AdvisoryIDs),
	}
	// tagStatement carries the advisory-group write metadata the dispatch
	// seam publishes beside every statement.
	tagStatement := func(stmt *sourcecypher.Statement, summary string) {
		stmt.Parameters[sourcecypher.StatementMetadataPhaseKey] = canonicalPhaseAdvisoryGroup
		stmt.Parameters[sourcecypher.StatementMetadataEntityLabelKey] = advisoryGroupLabel
		stmt.Parameters[sourcecypher.StatementMetadataSummaryKey] = summary
	}
	nodeCreate := sourcecypher.Statement{
		Operation:  sourcecypher.OperationCanonicalUpsert,
		Cypher:     AdvisoryGroupNodeCreateCypher,
		Parameters: maps.Clone(nodeParams),
	}
	tagStatement(&nodeCreate, fmt.Sprintf("label=%s node=%s create", advisoryGroupLabel, group.ID))
	nodeUpdate := sourcecypher.Statement{
		Operation:  sourcecypher.OperationCanonicalUpsert,
		Cypher:     AdvisoryGroupNodeUpdateCypher,
		Parameters: maps.Clone(nodeParams),
	}
	tagStatement(&nodeUpdate, fmt.Sprintf("label=%s node=%s update", advisoryGroupLabel, group.ID))
	mergeBatches := sourcecypher.BuildBatchedStatements(AdvisoryGroupEdgeMergeCypher, rows, w.batchSize)
	updateBatches := sourcecypher.BuildBatchedStatements(AdvisoryGroupEdgeUpdateCypher, rows, w.batchSize)
	batches := make([]sourcecypher.Statement, 0, 2+len(mergeBatches)+len(updateBatches)+2)
	batches = append(batches, nodeCreate, nodeUpdate)
	for index := range mergeBatches {
		batchRows := mergeBatches[index].Parameters["rows"].([]map[string]any)
		mergeBatches[index].Parameters["id"] = group.ID
		tagStatement(&mergeBatches[index], fmt.Sprintf(
			"label=%s rows=%d merge", advisoryGroupLabel, len(batchRows),
		))
		batches = append(batches, mergeBatches[index])
	}
	for index := range updateBatches {
		batchRows := updateBatches[index].Parameters["rows"].([]map[string]any)
		updateBatches[index].Parameters["id"] = group.ID
		tagStatement(&updateBatches[index], fmt.Sprintf(
			"label=%s rows=%d update", advisoryGroupLabel, len(batchRows),
		))
		batches = append(batches, updateBatches[index])
	}
	batches = append(batches, sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher:    AdvisoryGroupStaleEdgeRetractCypher,
		Parameters: map[string]any{
			"id":       group.ID,
			"keepkeys": keepkeys,
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
