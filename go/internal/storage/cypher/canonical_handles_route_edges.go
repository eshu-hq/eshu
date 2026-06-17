package cypher

// batchCanonicalHandlesRouteUpsertCypher writes Function-[:HANDLES_ROUTE]->Endpoint
// edges from resolved framework route-to-handler bindings (issue #2721 Stage 2).
//
// The Endpoint is matched by path anchored through the Repository's
// EXPOSES_ENDPOINT relationship rather than a global Endpoint scan, so the match
// stays bounded to the repository that owns the handler and the same path served
// by N workloads fans out to the N Endpoint nodes that already exist. The
// confidence, reason, and resolution_method are row parameters derived from the
// reducer-stamped resolution method (ADR #2222), mirroring the CALLS edge so the
// provenance contract is identical.
const batchCanonicalHandlesRouteUpsertCypher = `UNWIND $rows AS row
MATCH (func:Function {uid: row.function_id})
MATCH (repo:Repository {id: row.repo_id})-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint {path: row.path})
MERGE (func)-[rel:HANDLES_ROUTE]->(endpoint)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source,
    rel.method = row.method`

// buildHandlesRouteRowMap converts a HANDLES_ROUTE intent payload into the flat
// UNWIND parameter map. It returns false when a required MATCH or MERGE field is
// empty so an ambiguous or malformed row is dropped instead of writing a
// dangling edge.
func buildHandlesRouteRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	repoID := payloadString(payload, "repo_id")
	functionID := payloadString(payload, "function_id")
	routePath := payloadString(payload, "path")
	if repoID == "" || functionID == "" || routePath == "" {
		return "", nil, false
	}
	rowMap := map[string]any{
		"repo_id":         repoID,
		"function_id":     functionID,
		"path":            routePath,
		"method":          payloadString(payload, "method"),
		"evidence_source": evidenceSource,
	}
	applyCodeCallProvenance(rowMap, payload)
	return batchCanonicalHandlesRouteUpsertCypher, rowMap, true
}

// retractHandlesRouteEdgesCypher deletes HANDLES_ROUTE edges whose source
// Function belongs to one of the given repositories and whose evidence_source
// matches, so a reprojection cleanly removes stale handler bindings before the
// upsert reasserts the current ones.
const retractHandlesRouteEdgesCypher = `MATCH (func:Function)-[rel:HANDLES_ROUTE]->(:Endpoint)
WHERE func.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// BuildRetractHandlesRouteEdges builds a HANDLES_ROUTE edge retraction statement
// for all handler Functions owned by the given repositories.
func BuildRetractHandlesRouteEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractHandlesRouteEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}
