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

// KNOWN-BROKEN ON NORNICDB (#5116): the three batchCanonical*UpsertCypher
// fallback templates below anchor their MATCH on a node-label disjunction
// (source:Function|Class|File|...). On the pinned NornicDB a node-label
// disjunction in a MATCH returns zero rows even when the node exists, so these
// fallbacks silently write NO edge. They are reached only when an endpoint's
// label is unresolved: buildCodeCallRowMap routes resolved endpoints to the
// exact-label templates in edge_writer_code_call_labels.go, which NornicDB
// matches, so the common path is unaffected. The retract-side instance of this
// same disjunction bug is fixed per source label by buildCodeCallRetractStatements
// in canonical_retract.go; the write-side silent-drop for unresolved-label
// endpoints (and every USES_METACLASS write, whose only template is the
// disjunction below) is tracked in #5116 and needs its own write-side live proof
// before it is fixed here.
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
