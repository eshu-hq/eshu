// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workload

// The reads below are deliberately single-clause, identity-anchored matches
// with a bound relationship variable. The negative clauses (no instance, a
// scoped twin, only safe relationships) are decided in Go by [Evaluate] over
// these reads, so the readiness check, the dry run, and the pre-delete
// verification share one code path. That also keeps the reads clear of the
// shapes docs/public/reference/nornicdb-pitfalls.md records as unreliable on
// NornicDB (relationship-existence predicates, `EXISTS {}`, `OPTIONAL MATCH`
// with an aggregate); NornicDB was not tested here, by owner direction.

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

// DeleteNeo4jCypher retires the named legacy nodes. It is Neo4j-only (the
// only backend tested; see the evidence note) and runs as one auto-commit
// transaction over at most MaxBatch ids, anchored on the Workload id
// constraint (`w.id IN $ids`). It does not UNWIND the delete.
//
// It takes the write lock on each named node first (a marker property set and
// removed in the same statement, so nothing persists) and only then evaluates
// the retirement clauses, so every clause is checked while this statement
// holds the node's lock. What that guarantees on Neo4j 2026.08.1, from the
// live race proof:
//
//   - A writer that changes the node itself (a property, a label) holds the
//     node's lock until it commits; this statement waits and then reads the
//     committed value, so the node is kept.
//   - A writer that has created an INSTANCE_OF or any other relationship to the
//     node but not yet committed does not hold the node's lock while it runs;
//     both transactions then need the other's lock, and Neo4j aborts one of
//     them with DeadlockDetected. Either outcome is safe: if this statement is
//     aborted nothing is deleted and the transient error is retried, and the
//     retry sees the relationship; if the writer is aborted its relationship
//     never existed.
//   - It does not protect against a writer that touches neither the node nor
//     its relationships: a WorkloadInstance created with only a workload_id
//     property naming the node, or a twin losing its DEFINES edge, can commit
//     while this statement runs and is not seen. Run retirement in a quiet
//     window, against one reducer pod.
//
// Null review: $ids is always a bound list; a null name, repo_id, or twin
// makes its equality null and the node is not matched; the unsafe-relationship
// subquery's predicate is wrapped in coalesce so a null other-end id counts
// as unsafe instead of passing. Label checks use `'L' IN labels(o)`, the form
// the repository's label-predicate gate requires.
const DeleteNeo4jCypher = deleteLockClause + deleteGateClause + `DETACH DELETE w`

// deleteLockClause matches the named nodes and takes their write locks before
// any retirement clause is evaluated.
const deleteLockClause = `MATCH (w:Workload)
WHERE w.id IN $ids
SET w.__eshu_retire_lock = true
REMOVE w.__eshu_retire_lock
WITH w
`

// deleteGateClause is every retirement clause, evaluated after the lock. The
// legacy-shape clause joins byte for byte (a padded id carries its padding,
// so it still matches); only the twin construction and the own-repository
// comparisons trim, exactly as TwinID and safeRelationship do in Go. trim()
// and Go TrimSpace agree on ASCII space, tab, LF, and CR by live probe, and
// the live seed proves space; no-break space is out of claim scope.
const deleteGateClause = `WHERE 'workload:' + w.name = w.id
  AND w.generation_id IS NULL
  AND NOT (w)<-[:INSTANCE_OF]-(:WorkloadInstance)
  AND NOT EXISTS { MATCH (i:WorkloadInstance) WHERE i.workload_id = w.id }
  AND EXISTS {
    MATCH (:Repository {id: trim(w.repo_id)})-[:DEFINES]->(t:Workload {id: 'workload:' + trim(w.repo_id) + ':' + trim(w.name)})
    WHERE t.generation_id IS NOT NULL
  }
  AND NOT EXISTS {
    MATCH (w)-[r]-(o)
    WHERE NOT ((type(r) = 'SAME_NAME' AND 'Workload' IN labels(o)) OR (type(r) = 'DEFINES' AND startNode(r) = o AND 'Repository' IN labels(o) AND coalesce(trim(o.id) = trim(w.repo_id), false)))
  }
`

// MaxBatch bounds the ids one detection page or delete statement carries.
const MaxBatch = 100
