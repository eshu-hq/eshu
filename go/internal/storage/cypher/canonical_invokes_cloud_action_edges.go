// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

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

const batchCanonicalInvokesCloudActionUpsertCypher = `UNWIND $rows AS row
MATCH (func:Function {uid: row.function_id})
MERGE (action:CloudAction {id: row.action_id})
  ON CREATE SET action.action = row.action
MERGE (func)-[rel:INVOKES_CLOUD_ACTION]->(action)
SET rel.action = row.action,
    rel.confidence = row.confidence,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source`

// retractInvokesCloudActionEdgesCypher removes the INVOKES_CLOUD_ACTION edges
// this evidence source owns for a set of repositories before they are
// re-projected, so a removed or re-resolved SDK call site does not leave a stale
// edge. It matches on the source Function's repo_id and the edge evidence_source,
// mirroring the retract-before-write contract the other shared-projection
// domains use. The CloudAction node is intentionally left in place: it is shared,
// id-keyed, and identical across repos, so retracting the edge alone is correct.
const retractInvokesCloudActionEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (func:Function {repo_id: repo_id})-[rel:INVOKES_CLOUD_ACTION]->(:CloudAction)
WHERE rel.evidence_source = $evidence_source
DELETE rel`

// buildInvokesCloudActionRowMap converts an invokes_cloud_action intent payload
// into the flat UNWIND parameter map for the INVOKES_CLOUD_ACTION upsert. It
// skips the row (ok=false) when any MATCH/MERGE key — function_id, action, or
// action_id — is empty so an unresolvable or actionless edge is never written.
// Provenance fields are passed through from the reducer, which derives them from
// the import-proven SDK binding.
func buildInvokesCloudActionRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	functionID := payloadString(payload, "function_id")
	// The cloud action is carried under "cloud_action" (not "action"), so it does
	// not collide with the shared-projection upsert/refresh discriminator that
	// filterUpsertRows reads from payload["action"]. The Cypher param stays
	// "action" because it sets the CloudAction node + edge action property.
	action := payloadString(payload, "cloud_action")
	actionID := payloadString(payload, "action_id")
	if functionID == "" || action == "" || actionID == "" {
		return "", nil, false
	}
	return batchCanonicalInvokesCloudActionUpsertCypher, map[string]any{
		"function_id":       functionID,
		"action":            action,
		"action_id":         actionID,
		"resolution_method": payloadString(payload, "resolution_method"),
		"confidence":        payloadFloat(payload, "confidence"),
		"evidence_source":   evidenceSource,
	}, true
}
