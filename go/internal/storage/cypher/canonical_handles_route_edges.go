// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// HANDLES_ROUTE edge templates (#2721).
//
// A Function-[:HANDLES_ROUTE]->Endpoint edge records that a parser-identified
// route handler function serves a materialized API endpoint. The Function node
// is keyed by uid (committed at canonical-nodes) and the Endpoint node is keyed
// by (repo_id, path) (committed at workload-materialization). The reducer only
// emits an intent for an exact, unambiguous handler resolution, so the MERGE
// never attaches an edge to a guessed handler; if either MATCH finds no node
// the MERGE is a no-op rather than an error.

const batchCanonicalHandlesRouteEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (f:Function {uid: row.function_entity_id})
MATCH (e:Endpoint {repo_id: row.repo_id, path: row.path})
MERGE (f)-[rel:HANDLES_ROUTE]->(e)
SET rel.http_method = row.http_method,
    rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source`

// retractHandlesRouteEdgesCypher removes the HANDLES_ROUTE edges this evidence
// source owns for a set of repositories before they are re-projected, so a
// removed or re-resolved handler binding does not leave a stale edge. It matches
// on the Endpoint's repo_id and the edge evidence_source, mirroring the
// retract-before-write contract the other shared-projection domains use.
const retractHandlesRouteEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (:Function)-[rel:HANDLES_ROUTE]->(e:Endpoint {repo_id: repo_id})
WHERE rel.evidence_source = $evidence_source
DELETE rel`
