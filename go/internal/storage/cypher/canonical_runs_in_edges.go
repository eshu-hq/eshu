// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// RUNS_IN edge templates (#2722).
//
// A Function-[:RUNS_IN]->Workload edge binds a parser-identified route-handler
// Function to the deployed runtime it runs in. The Function node is keyed by uid
// (committed at canonical-nodes); the target is resolved through the Repository
// the handler belongs to: the edge fans out to every Workload that Repository
// DEFINES (committed at workload-materialization). The reducer only emits an
// intent for an exact, unambiguous handler resolution, so the MERGE never
// attaches an edge to a guessed entrypoint; if either MATCH finds no node the
// MERGE is a no-op rather than an error.
//
// rel.ambiguous is "represented, not collapsed": the reducer cannot count a
// repo's materialized Workloads at intent-build time, so it marks every edge
// ambiguous=true — a candidate member of the repo's workload set, never an
// asserted single-workload binding. A repo that DEFINES exactly one Workload
// still yields exactly one edge, and a consumer derives exactness by counting the
// fan-out at query time.

const batchCanonicalRunsInEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (func:Function {uid: row.function_id})
MATCH (repo:Repository {id: row.repo_id})-[:DEFINES]->(workload:Workload)
MERGE (func)-[rel:RUNS_IN]->(workload)
SET rel.confidence = row.confidence,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source,
    rel.ambiguous = row.ambiguous`

// retractRunsInEdgesCypher removes the RUNS_IN edges this evidence source owns for
// a set of repositories before they are re-projected, so a removed or re-resolved
// handler binding does not leave a stale edge. It matches on the source Function's
// repo_id and the edge evidence_source, mirroring the retract-before-write
// contract the other shared-projection domains use.
const retractRunsInEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (f:Function {repo_id: repo_id})-[rel:RUNS_IN]->(:Workload)
WHERE rel.evidence_source = $evidence_source
DELETE rel`

// buildRunsInRowMap converts a runs_in intent payload into the flat UNWIND
// parameter map for the RUNS_IN upsert. It skips the row (ok=false) when any
// MATCH key — function_id or repo_id — is empty so an unresolvable edge is never
// written. The ambiguous flag rides through verbatim: the reducer marks every
// edge ambiguous=true because it cannot count the repo's materialized Workloads
// at intent-build time, so the edge is always a candidate-set member.
func buildRunsInRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	functionID := payloadString(payload, "function_id")
	repoID := payloadString(payload, "repo_id")
	if functionID == "" || repoID == "" {
		return "", nil, false
	}
	return batchCanonicalRunsInEdgeUpsertCypher, map[string]any{
		"function_id":       functionID,
		"repo_id":           repoID,
		"resolution_method": payloadString(payload, "resolution_method"),
		"confidence":        payloadFloat(payload, "confidence"),
		"ambiguous":         payloadBool(payload, "ambiguous"),
		"evidence_source":   evidenceSource,
	}, true
}
