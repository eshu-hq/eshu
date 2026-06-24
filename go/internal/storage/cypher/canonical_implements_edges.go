// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// batchCanonicalImplementsEdgeUpsertCypher writes IMPLEMENTS edges from a
// class/struct/enum to an interface it declares it implements (issue #2229).
// It mirrors the inheritance edge templates: a batched UNWIND MERGE keyed on
// uid, so it shares the inheritance-domain write path and batch semantics.
const batchCanonicalImplementsEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (child:Class|Struct|Enum|Interface|Protocol {uid: row.child_entity_id})
MATCH (parent:Interface|Protocol {uid: row.parent_entity_id})
MERGE (child)-[rel:IMPLEMENTS]->(parent)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`
