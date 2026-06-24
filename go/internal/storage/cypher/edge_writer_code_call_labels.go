// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// codeCallResolutionMethod reads the resolution method the reducer stamped on a
// code-call row (ADR #2222). A row without a recorded method reads as
// unspecified so a freshly reprojected edge always carries an explicit value
// while legacy un-reprojected edges keep their prior confidence.
func codeCallResolutionMethod(payload map[string]any) codeprovenance.Method {
	if method := payloadString(payload, "resolution_method"); method != "" {
		return method
	}
	return codeprovenance.MethodUnspecified
}

// applyCodeCallProvenance derives the per-edge confidence and reason from the
// row's resolution method and writes all three into rowMap, so every code-call
// edge template persists differentiated provenance instead of a fixed 0.95.
func applyCodeCallProvenance(rowMap map[string]any, payload map[string]any) {
	method := codeCallResolutionMethod(payload)
	rowMap["resolution_method"] = method
	rowMap["confidence"] = codeprovenance.Confidence(method)
	rowMap["reason"] = codeprovenance.Reason(method)
}

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

var codeInstantiatesSourceLabels = map[string]struct{}{
	"Class":    {},
	"File":     {},
	"Function": {},
}

var codeInstantiatesTargetLabels = map[string]struct{}{
	"Class":  {},
	"Enum":   {},
	"Struct": {},
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
		rowMap := map[string]any{
			"source_entity_id":  sourceEntityID,
			"target_entity_id":  targetEntityID,
			"relationship_type": relationshipType,
			"evidence_source":   evidenceSource,
		}
		applyCodeCallProvenance(rowMap, payload)
		return batchCanonicalMetaclassUpsertCypher, rowMap, true
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
	applyCodeCallProvenance(rowMap, payload)

	if payloadString(payload, "relationship_type") == "INSTANTIATES" {
		if isCodeInstantiatesSourceLabel(sourceLabel) && isCodeInstantiatesTargetLabel(targetLabel) {
			return buildLabelScopedInstantiatesCypher(sourceLabel, targetLabel), rowMap, true
		}
		return batchCanonicalInstantiatesUpsertCypher, rowMap, true
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

func isCodeInstantiatesSourceLabel(label string) bool {
	_, ok := codeInstantiatesSourceLabels[label]
	return ok
}

func isCodeInstantiatesTargetLabel(label string) bool {
	_, ok := codeInstantiatesTargetLabels[label]
	return ok
}

func buildLabelScopedCodeCallCypher(sourceLabel string, targetLabel string) string {
	return fmt.Sprintf(`UNWIND $rows AS row
MATCH (source:%s {uid: row.caller_entity_id})
MATCH (target:%s {uid: row.callee_entity_id})
MERGE (source)-[rel:CALLS]->(target)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source,
    rel.call_kind = row.call_kind`, sourceLabel, targetLabel)
}

func buildLabelScopedCodeReferenceCypher(sourceLabel string, targetLabel string) string {
	return fmt.Sprintf(`UNWIND $rows AS row
MATCH (source:%s {uid: row.caller_entity_id})
MATCH (target:%s {uid: row.callee_entity_id})
MERGE (source)-[rel:REFERENCES]->(target)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source,
    rel.call_kind = row.call_kind`, sourceLabel, targetLabel)
}

func buildLabelScopedInstantiatesCypher(sourceLabel string, targetLabel string) string {
	return fmt.Sprintf(`UNWIND $rows AS row
MATCH (source:%s {uid: row.caller_entity_id})
MATCH (target:%s {uid: row.callee_entity_id})
MERGE (source)-[rel:INSTANTIATES]->(target)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
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
	switch domain {
	case "code_calls":
		relationship := codeCallRelationshipSummary(cypher)
		source := edgeEndpointSummary(cypher, "source")
		target := edgeEndpointSummary(cypher, "target")
		return fmt.Sprintf(
			"domain=code_calls relationship=%s source=%s target=%s rows=%d",
			relationship,
			source,
			target,
			rowCount,
		)
	case "inheritance_edges":
		relationship := inheritanceRelationshipSummary(cypher)
		child := edgeEndpointSummary(cypher, "child")
		parent := edgeEndpointSummary(cypher, "parent")
		return fmt.Sprintf(
			"domain=inheritance_edges relationship=%s child=%s parent=%s rows=%d",
			relationship,
			child,
			parent,
			rowCount,
		)
	default:
		return ""
	}
}

func codeCallRelationshipSummary(cypher string) string {
	switch {
	case strings.Contains(cypher, "rel:REFERENCES"):
		return "REFERENCES"
	case strings.Contains(cypher, "rel:USES_METACLASS"):
		return "USES_METACLASS"
	case strings.Contains(cypher, "rel:INSTANTIATES"):
		return "INSTANTIATES"
	default:
		return "CALLS"
	}
}

func inheritanceRelationshipSummary(cypher string) string {
	switch {
	case strings.Contains(cypher, "rel:OVERRIDES"):
		return "OVERRIDES"
	case strings.Contains(cypher, "rel:ALIASES"):
		return "ALIASES"
	case strings.Contains(cypher, "rel:IMPLEMENTS"):
		return "IMPLEMENTS"
	default:
		return "INHERITS"
	}
}

func edgeEndpointSummary(cypher string, endpoint string) string {
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
