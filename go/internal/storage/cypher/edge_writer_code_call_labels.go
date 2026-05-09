package cypher

import "fmt"

var codeCallEndpointLabels = map[string]struct{}{
	"Class":     {},
	"File":      {},
	"Function":  {},
	"Interface": {},
	"Struct":    {},
	"TypeAlias": {},
}

var codeReferenceEndpointLabels = map[string]struct{}{
	"Class":     {},
	"File":      {},
	"Function":  {},
	"Interface": {},
	"Struct":    {},
	"TypeAlias": {},
}

func buildCodeCallRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	if relationshipType := payloadString(payload, "relationship_type"); relationshipType == "USES_METACLASS" {
		sourceEntityID := payloadString(payload, "source_entity_id")
		targetEntityID := payloadString(payload, "target_entity_id")
		if sourceEntityID == "" || targetEntityID == "" {
			return "", nil, false
		}
		return batchCanonicalMetaclassUpsertCypher, map[string]any{
			"source_entity_id":  sourceEntityID,
			"target_entity_id":  targetEntityID,
			"relationship_type": relationshipType,
			"evidence_source":   evidenceSource,
		}, true
	}

	callerEntityID := payloadString(payload, "caller_entity_id")
	calleeEntityID := payloadString(payload, "callee_entity_id")
	if callerEntityID == "" || calleeEntityID == "" {
		return "", nil, false
	}
	rowMap := map[string]any{
		"caller_entity_id": callerEntityID,
		"callee_entity_id": calleeEntityID,
		"evidence_source":  evidenceSource,
	}
	if callKind := payloadString(payload, "call_kind"); callKind != "" {
		rowMap["call_kind"] = callKind
	}
	sourceLabel := payloadString(payload, "caller_entity_type")
	targetLabel := payloadString(payload, "callee_entity_type")
	if sourceLabel != "" {
		rowMap["caller_entity_type"] = sourceLabel
	}
	if targetLabel != "" {
		rowMap["callee_entity_type"] = targetLabel
	}

	if rowMap["call_kind"] == "jsx_component" || payloadString(payload, "relationship_type") == "REFERENCES" {
		if isCodeReferenceEndpointLabel(sourceLabel) && isCodeReferenceEndpointLabel(targetLabel) {
			return buildLabelScopedCodeReferenceCypher(sourceLabel, targetLabel), rowMap, true
		}
		return batchCanonicalCodeReferenceUpsertCypher, rowMap, true
	}
	if isCodeCallEndpointLabel(sourceLabel) && isCodeCallEndpointLabel(targetLabel) {
		return buildLabelScopedCodeCallCypher(sourceLabel, targetLabel), rowMap, true
	}
	return batchCanonicalCodeCallUpsertCypher, rowMap, true
}

func isCodeCallEndpointLabel(label string) bool {
	_, ok := codeCallEndpointLabels[label]
	return ok
}

func isCodeReferenceEndpointLabel(label string) bool {
	_, ok := codeReferenceEndpointLabels[label]
	return ok
}

func buildLabelScopedCodeCallCypher(sourceLabel string, targetLabel string) string {
	return fmt.Sprintf(`UNWIND $rows AS row
MATCH (source:%s {uid: row.caller_entity_id})
MATCH (target:%s {uid: row.callee_entity_id})
MERGE (source)-[rel:CALLS]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser and symbol analysis resolved a code call edge',
    rel.evidence_source = row.evidence_source,
    rel.call_kind = row.call_kind`, sourceLabel, targetLabel)
}

func buildLabelScopedCodeReferenceCypher(sourceLabel string, targetLabel string) string {
	return fmt.Sprintf(`UNWIND $rows AS row
MATCH (source:%s {uid: row.caller_entity_id})
MATCH (target:%s {uid: row.callee_entity_id})
MERGE (source)-[rel:REFERENCES]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser and symbol analysis resolved a code reference edge',
    rel.evidence_source = row.evidence_source,
    rel.call_kind = row.call_kind`, sourceLabel, targetLabel)
}
