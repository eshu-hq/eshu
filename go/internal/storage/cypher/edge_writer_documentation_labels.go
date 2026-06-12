package cypher

// buildDocumentationRowMap routes a documentation DOCUMENTS edge row (issue
// #2231) to the template for its resolved target. An exact entity mention
// resolves to a code entity (matched by uid) or a workload (matched by id); a
// mention whose candidate is a service is dropped because no Service graph node
// exists yet — provenance is never allowed to fabricate a node.
func buildDocumentationRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	sectionUID := payloadString(payload, "section_uid")
	targetEntityID := payloadString(payload, "target_entity_id")
	if sectionUID == "" || targetEntityID == "" {
		return "", nil, false
	}

	rowMap := map[string]any{
		"section_uid":      sectionUID,
		"target_entity_id": targetEntityID,
		"scope_id":         payloadString(payload, "scope_id"),
		"document_id":      payloadString(payload, "document_id"),
		"section_id":       payloadString(payload, "section_id"),
		"heading_text":     payloadString(payload, "heading_text"),
		"section_anchor":   payloadString(payload, "section_anchor"),
		"excerpt_hash":     payloadString(payload, "excerpt_hash"),
		"mention_kind":     payloadString(payload, "mention_kind"),
		"evidence_source":  evidenceSource,
	}

	switch payloadString(payload, "target_kind") {
	case "workload":
		return batchCanonicalDocumentationWorkloadEdgeCypher, rowMap, true
	case "service":
		// No Service graph node exists; never fabricate one.
		return "", nil, false
	default:
		return batchCanonicalDocumentationEntityEdgeCypher, rowMap, true
	}
}

// BuildRetractDocumentationEdges builds a scope-scoped DOCUMENTS edge retraction
// statement. The id list carries documentation scope ids (not repository ids):
// documentation is scope-scoped, so the retract anchors on section.scope_id.
func BuildRetractDocumentationEdges(scopeIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractDocumentationEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":       scopeIDs,
			"evidence_source": evidenceSource,
		},
	}
}
