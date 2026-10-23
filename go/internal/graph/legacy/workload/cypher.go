// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workload

// The reads below are deliberately single-clause, identity-anchored matches
// with a bound relationship variable, and they compare the legacy shape as
// `'workload:' + w.name = w.id` (expression on the left). On the pinned
// NornicDB build `w.id = 'workload:' + w.name`, every `EXISTS {}` subquery,
// and every `OPTIONAL MATCH ... WITH count(...)` composition return wrong
// answers without an error (docs/public/reference/nornicdb-pitfalls.md and
// docs/internal/evidence/8795-legacy-workload-retirement.md), so the negative
// clauses (no instance, a scoped twin, only safe relationships) are decided in
// Go by [Evaluate] over these reads. Neo4j answers all shapes identically.

// candidatePageCypher pages the name-only Workload nodes without a
// generation_id, keyset-ordered by id. Both parameters are always bound:
// $after as a string (empty to start) and $limit as an integer.
const candidatePageCypher = `MATCH (w:Workload)
WHERE 'workload:' + w.name = w.id AND w.generation_id IS NULL AND w.id > $after
RETURN w.id AS id, w.name AS name, w.repo_id AS repo_id
ORDER BY w.id
LIMIT $limit`

// candidatesByIDsCypher reads the named nodes that still have the legacy
// shape. $ids is always bound as a non-nil list: `IN null` is null and would
// silently match nothing.
const candidatesByIDsCypher = `MATCH (w:Workload)
WHERE w.id IN $ids AND 'workload:' + w.name = w.id AND w.generation_id IS NULL
RETURN w.id AS id, w.name AS name, w.repo_id AS repo_id
ORDER BY w.id`

// instanceEdgeCypher finds candidates a WorkloadInstance reaches by edge.
const instanceEdgeCypher = `MATCH (i:WorkloadInstance)-[r:INSTANCE_OF]->(w:Workload)
WHERE w.id IN $ids
RETURN DISTINCT w.id AS id`

// instancePropertyCypher finds candidates a WorkloadInstance names in its
// workload_id property, the join the handle resolver uses. It seeks the
// workload_instance_workload_id index.
const instancePropertyCypher = `MATCH (i:WorkloadInstance)
WHERE i.workload_id IN $ids
RETURN DISTINCT i.workload_id AS id`

// twinCypher finds the generation-stamped scoped Workloads among $twin_ids
// and the Repository nodes that DEFINE them. It anchors on the Workload id
// constraint.
const twinCypher = `MATCH (rp:Repository)-[d:DEFINES]->(t:Workload)
WHERE t.id IN $twin_ids AND t.generation_id IS NOT NULL
RETURN t.id AS id, rp.id AS repo_id`

// relationshipsCypher lists every relationship incident on the candidates.
const relationshipsCypher = `MATCH (w:Workload)-[r]-(o)
WHERE w.id IN $ids
RETURN w.id AS id, type(r) AS type, startNode(r) = w AS outgoing, o.id AS other_id, labels(o) AS other_labels`

// DeleteNeo4jCypher retires the named legacy nodes and is itself the gate:
// every retirement clause is evaluated inside the statement, so a node that
// gained an instance, lost its twin, or gained an unsafe relationship after
// the dry run is simply not matched. It is Neo4j-only (the clauses it uses
// are the ones NornicDB mis-evaluates) and runs as one auto-commit
// transaction over at most MaxBatch ids, anchored on the Workload id
// constraint (`w.id IN $ids`). It does not UNWIND the delete.
//
// Null review: $ids is always a bound list; a null name, repo_id, or twin
// makes its equality null and the node is not matched; the unsafe-relationship
// subquery's predicate is wrapped in coalesce so a null other-end id counts
// as unsafe instead of passing.
const DeleteNeo4jCypher = `MATCH (w:Workload)
WHERE w.id IN $ids
  AND 'workload:' + w.name = w.id
  AND w.generation_id IS NULL
  AND NOT (w)<-[:INSTANCE_OF]-(:WorkloadInstance)
  AND NOT EXISTS { MATCH (i:WorkloadInstance) WHERE i.workload_id = w.id }
  AND EXISTS {
    MATCH (:Repository {id: w.repo_id})-[:DEFINES]->(t:Workload {id: 'workload:' + w.repo_id + ':' + w.name})
    WHERE t.generation_id IS NOT NULL
  }
  AND NOT EXISTS {
    MATCH (w)-[r]-(o)
    WHERE NOT ((type(r) = 'SAME_NAME' AND o:Workload) OR (type(r) = 'DEFINES' AND startNode(r) = o AND o:Repository AND coalesce(o.id = w.repo_id, false)))
  }
DETACH DELETE w`

// MaxBatch bounds the ids one detection page or delete statement carries.
const MaxBatch = 100
