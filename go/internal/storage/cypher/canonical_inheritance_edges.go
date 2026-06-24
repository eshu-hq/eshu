// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// Batched UNWIND Cypher for inheritance, trait adaptation, and interface
// implementation edges. The reducer stamps each row with a codeprovenance
// resolution method, and the edge writer derives confidence and reason before
// execution so these templates do not carry per-relationship confidence
// literals.
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
