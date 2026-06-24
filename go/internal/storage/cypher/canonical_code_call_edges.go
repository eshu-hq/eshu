// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// Batched UNWIND Cypher for code call, reference, and Python metaclass edges.
//
// Per ADR #2222 the confidence, reason, and resolution_method are row
// parameters rather than literals: the reducer stamps a resolution method on
// each row (issue #2223) and the edge writer derives the tiered confidence and
// reason from it (issue #2224). The MERGE and UNWIND batching shape is unchanged
// from the previous hard-coded form, so the graph-write hot path keeps its batch
// semantics; only the SET clause now reads per-edge provenance.

const batchCanonicalCodeCallUpsertCypher = `UNWIND $rows AS row
MATCH (source:Function|Class|File {uid: coalesce(row.caller_entity_id, row.source_entity_id)})
MATCH (target:Function|Class|File {uid: coalesce(row.callee_entity_id, row.target_entity_id)})
MERGE (source)-[rel:CALLS]->(target)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source,
    rel.call_kind = row.call_kind`

const batchCanonicalCodeReferenceUpsertCypher = `UNWIND $rows AS row
MATCH (source:Function|Class|Struct|Interface|TypeAlias|File {uid: row.caller_entity_id})
MATCH (target:Function|Class|Struct|Interface|TypeAlias|File {uid: row.callee_entity_id})
MERGE (source)-[rel:REFERENCES]->(target)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source,
    rel.call_kind = row.call_kind`

const batchCanonicalMetaclassUpsertCypher = `UNWIND $rows AS row
MATCH (source:Function|Class|File {uid: row.source_entity_id})
MATCH (target:Function|Class|File {uid: row.target_entity_id})
MERGE (source)-[rel:USES_METACLASS]->(target)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`
