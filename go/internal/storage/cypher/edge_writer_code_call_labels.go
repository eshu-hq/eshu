package cypher

import (
	"fmt"
	"strings"
)

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

func annotateEdgeStatementSummaries(domain string, cypher string, stmts []Statement) {
	for index := range stmts {
		rowCount, _ := statementRowsCount(stmts[index])
		summary := edgeStatementSummary(domain, cypher, rowCount)
		if summary == "" {
			continue
		}
		if stmts[index].Parameters == nil {
			stmts[index].Parameters = make(map[string]any)
		}
		stmts[index].Parameters[StatementMetadataSummaryKey] = summary
	}
}

func edgeStatementSummary(domain string, cypher string, rowCount int) string {
	if domain != "code_calls" {
		return ""
	}
	relationship := codeCallRelationshipSummary(cypher)
	source := codeCallEndpointSummary(cypher, "source")
	target := codeCallEndpointSummary(cypher, "target")
	return fmt.Sprintf(
		"domain=code_calls relationship=%s source=%s target=%s rows=%d",
		relationship,
		source,
		target,
		rowCount,
	)
}

func codeCallRelationshipSummary(cypher string) string {
	switch {
	case strings.Contains(cypher, "rel:REFERENCES"):
		return "REFERENCES"
	case strings.Contains(cypher, "rel:USES_METACLASS"):
		return "USES_METACLASS"
	default:
		return "CALLS"
	}
}

func codeCallEndpointSummary(cypher string, endpoint string) string {
	marker := "MATCH (" + endpoint + ":"
	start := strings.Index(cypher, marker)
	if start < 0 {
		return "unknown"
	}
	start += len(marker)
	remainder := cypher[start:]
	end := strings.Index(remainder, " ")
	if end < 0 {
		end = strings.Index(remainder, "{")
	}
	if end < 0 {
		return "unknown"
	}
	return remainder[:end]
}
