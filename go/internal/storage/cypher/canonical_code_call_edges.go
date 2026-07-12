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

// NornicDB disjunction status (#5116, refined by measurement): the
// batchCanonical*UpsertCypher templates below anchor their MATCH on a
// node-label disjunction with an inline {uid: ...} property. Probed on the
// pinned v1.1.11 (see docs/public/reference/nornicdb-pitfalls.md and the
// #5141 evidence): this row-driven UNWIND + inline-anchor disjunction shape
// DOES match and write correctly — the USES_METACLASS template is live-proven
// end to end by TestReducerMetaclassEdgeRetractGraphTruth in
// internal/replay/offlinetier. The zero-row disjunction failure applies to
// bare MATCH + WHERE shapes (the retract-side instance, fixed per source label
// by buildCodeCallRetractStatements in canonical_retract.go). For the
// plain-anchor templates (REFERENCES and USES_METACLASS), what remains tracked
// in #5116 is a data concern, not a Cypher-shape one: unresolved-label
// endpoints route here and can reference nodes that do not exist or carry
// labels outside the disjunction, in which case the row still writes nothing.
// The CALLS template is different: its coalesce(row.caller_entity_id,
// row.source_entity_id) anchor is a function-expression variant no probe has
// measured on v1.1.11, so it keeps its own #5116 write-side proof obligation.
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
