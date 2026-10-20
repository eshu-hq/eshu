// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package node

// INVOKES_CLOUD_ACTION edge templates (#2723).
//
// A Function-[:INVOKES_CLOUD_ACTION]->CloudAction edge records that a Go AWS SDK
// call site in the Function provably invokes a cloud action whose identity is in
// the closed CAN_PERFORM catalog, so the privilege graph becomes joinable: a
// Function that invokes "s3:putobject" lines up with a principal that
// CAN_PERFORM "s3:putobject". The Function node is keyed by uid (committed at
// canonical-nodes); the CloudAction node is keyed by id and created inline by
// the same MERGE, so there is no cross-acceptance-unit MATCH dependency. The
// reducer only emits an intent for a row whose (service, method) maps to a
// catalog action, so the MERGE never attaches an edge to a guessed action.

const BatchCanonicalInvokesCloudActionUpsertCypher = `UNWIND $rows AS row
MATCH (func:Function {uid: row.function_id})
MERGE (action:CloudAction {id: row.action_id})
  ON CREATE SET action.action = row.action
MERGE (func)-[rel:INVOKES_CLOUD_ACTION]->(action)
SET rel.action = row.action,
    rel.confidence = row.confidence,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source`

// RetractInvokesCloudActionEdgesCypher removes the INVOKES_CLOUD_ACTION edges
// this evidence source owns for a set of repositories before they are
// re-projected, so a removed or re-resolved SDK call site does not leave a stale
// edge. It matches on the source Function's repo_id and the edge evidence_source,
// mirroring the retract-before-write contract the other shared-projection
// domains use. The CloudAction node is intentionally left in place: it is shared,
// id-keyed, and identical across repos, so retracting the edge alone is correct.
const RetractInvokesCloudActionEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (func:Function {repo_id: repo_id})-[rel:INVOKES_CLOUD_ACTION]->(:CloudAction)
WHERE rel.evidence_source = $evidence_source
DELETE rel`
