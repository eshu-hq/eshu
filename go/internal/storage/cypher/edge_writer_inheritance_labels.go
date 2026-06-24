// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

var inheritanceEndpointLabels = map[string]struct{}{
	"Class":     {},
	"Enum":      {},
	"Function":  {},
	"Interface": {},
	"Protocol":  {},
	"Struct":    {},
	"Trait":     {},
}

func buildInheritanceRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	childEntityID := payloadString(payload, "child_entity_id")
	parentEntityID := payloadString(payload, "parent_entity_id")
	if childEntityID == "" || parentEntityID == "" {
		return "", nil, false
	}
	relationshipType := payloadString(payload, "relationship_type")
	rowMap := map[string]any{
		"child_entity_id":   childEntityID,
		"parent_entity_id":  parentEntityID,
		"relationship_type": relationshipType,
		"evidence_source":   evidenceSource,
	}
	applyInheritanceProvenance(rowMap, payload)
	childLabel := payloadString(payload, "child_entity_type")
	parentLabel := payloadString(payload, "parent_entity_type")
	if isInheritanceEndpointLabel(childLabel) && isInheritanceEndpointLabel(parentLabel) {
		if cypher, ok := labelScopedInheritanceCypher(relationshipType, childLabel, parentLabel); ok {
			return cypher, rowMap, true
		}
	}

	switch relationshipType {
	case "OVERRIDES":
		return batchCanonicalInheritanceOverrideUpsertCypher, rowMap, true
	case "ALIASES":
		return batchCanonicalInheritanceAliasUpsertCypher, rowMap, true
	case "IMPLEMENTS":
		return batchCanonicalImplementsEdgeUpsertCypher, rowMap, true
	case "", "INHERITS":
		return batchCanonicalInheritanceEdgeUpsertCypher, rowMap, true
	default:
		return "", nil, false
	}
}

func isInheritanceEndpointLabel(label string) bool {
	_, ok := inheritanceEndpointLabels[label]
	return ok
}

func labelScopedInheritanceCypher(
	relationshipType string,
	childLabel string,
	parentLabel string,
) (string, bool) {
	switch relationshipType {
	case "", "INHERITS":
		return buildLabelScopedInheritanceCypher(childLabel, parentLabel, "INHERITS"), true
	case "OVERRIDES":
		return buildLabelScopedInheritanceCypher(childLabel, parentLabel, "OVERRIDES"), true
	case "ALIASES":
		return buildLabelScopedInheritanceCypher(childLabel, parentLabel, "ALIASES"), true
	case "IMPLEMENTS":
		return buildLabelScopedInheritanceCypher(childLabel, parentLabel, "IMPLEMENTS"), true
	default:
		return "", false
	}
}

func inheritanceResolutionMethod(payload map[string]any) codeprovenance.Method {
	if method := payloadString(payload, "resolution_method"); method != "" {
		return method
	}
	return codeprovenance.MethodUnspecified
}

func applyInheritanceProvenance(rowMap map[string]any, payload map[string]any) {
	method := inheritanceResolutionMethod(payload)
	rowMap["resolution_method"] = method
	rowMap["confidence"] = codeprovenance.Confidence(method)
	rowMap["reason"] = codeprovenance.Reason(method)
}

func buildLabelScopedInheritanceCypher(
	childLabel string,
	parentLabel string,
	relationshipType string,
) string {
	return fmt.Sprintf(`UNWIND $rows AS row
MATCH (child:%s {uid: row.child_entity_id})
MATCH (parent:%s {uid: row.parent_entity_id})
MERGE (child)-[rel:%s]->(parent)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`, childLabel, parentLabel, relationshipType)
}
