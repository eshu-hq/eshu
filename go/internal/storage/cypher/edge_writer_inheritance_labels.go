package cypher

import "fmt"

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
		return buildLabelScopedInheritanceCypher(
			childLabel,
			parentLabel,
			"INHERITS",
			"Parser entity bases metadata resolved an inheritance edge",
		), true
	case "OVERRIDES":
		return buildLabelScopedInheritanceCypher(
			childLabel,
			parentLabel,
			"OVERRIDES",
			"Parser trait adaptation metadata resolved an override edge",
		), true
	case "ALIASES":
		return buildLabelScopedInheritanceCypher(
			childLabel,
			parentLabel,
			"ALIASES",
			"Parser trait adaptation metadata resolved an alias edge",
		), true
	default:
		return "", false
	}
}

func buildLabelScopedInheritanceCypher(
	childLabel string,
	parentLabel string,
	relationshipType string,
	reason string,
) string {
	return fmt.Sprintf(`UNWIND $rows AS row
MATCH (child:%s {uid: row.child_entity_id})
MATCH (parent:%s {uid: row.parent_entity_id})
MERGE (child)-[rel:%s]->(parent)
SET rel.confidence = 0.95,
    rel.reason = '%s',
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`, childLabel, parentLabel, relationshipType, reason)
}
