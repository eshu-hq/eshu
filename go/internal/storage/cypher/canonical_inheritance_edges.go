// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// Batched UNWIND Cypher for inheritance, trait adaptation, and interface
// implementation edges. The reducer stamps each row with a codeprovenance
// resolution method, and the edge writer derives confidence and reason before
// execution so these templates do not carry per-relationship confidence
// literals.
// inheritanceMaterializedEdgeTypes is the single source of truth for the
// relationship types the inheritance domain's write path can materialize,
// mapping each to the template that writes it. ifa.MaterializedEdgeDomainEdgeTypes
// asserts exactly this set for the inheritance_edges family, so a type reachable
// from the write path but absent here is a live edge the baseline gate ignores.
//
// Four types, not one. Three are written by templates in this file and IMPLEMENTS
// by canonical_implements_edges.go; the retract in canonical_inheritance_retract.go
// deletes the same four as INHERITS|OVERRIDES|ALIASES|IMPLEMENTS, and
// inheritanceRelationshipSummary enumerates them for telemetry. Reading only the
// first MERGE below yields INHERITS alone and undercounts the family fourfold —
// TestInheritanceRegistryMatchesRetractDisjunction pins the lists together.
var inheritanceMaterializedEdgeTypes = map[string]string{
	"INHERITS":   "class/type inheritance (batchCanonicalInheritanceEdgeUpsertCypher)",
	"OVERRIDES":  "member override (batchCanonicalInheritanceOverrideUpsertCypher)",
	"ALIASES":    "trait or type alias adaptation (batchCanonicalInheritanceAliasUpsertCypher)",
	"IMPLEMENTS": "interface implementation (batchCanonicalImplementsEdgeUpsertCypher)",
}

// InheritanceMaterializedEdgeTypes returns the inheritance domain's materialized
// relationship types keyed by type, each mapped to a short reason naming the
// template that writes it.
//
// It returns a copy so a caller cannot mutate the package's source of truth. The
// Ifá live baseline gate reads this to scope `assert-edges -domain
// inheritance_edges` to the family's own edges.
func InheritanceMaterializedEdgeTypes() map[string]string {
	out := make(map[string]string, len(inheritanceMaterializedEdgeTypes))
	for edgeType, reason := range inheritanceMaterializedEdgeTypes {
		out[edgeType] = reason
	}
	return out
}

const batchCanonicalInheritanceEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (child:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.child_entity_id})
MATCH (parent:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.parent_entity_id})
MERGE (child)-[rel:INHERITS]->(parent)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`

const batchCanonicalInheritanceOverrideUpsertCypher = `UNWIND $rows AS row
MATCH (child:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.child_entity_id})
MATCH (parent:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.parent_entity_id})
MERGE (child)-[rel:OVERRIDES]->(parent)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`

const batchCanonicalInheritanceAliasUpsertCypher = `UNWIND $rows AS row
MATCH (child:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.child_entity_id})
MATCH (parent:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.parent_entity_id})
MERGE (child)-[rel:ALIASES]->(parent)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`
