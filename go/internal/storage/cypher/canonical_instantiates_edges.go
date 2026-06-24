// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// batchCanonicalInstantiatesUpsertCypher writes INSTANTIATES edges from a caller
// to the class/struct/enum it constructs (issue #2229). It mirrors the code-call
// edge templates: a batched UNWIND MERGE keyed on uid that carries the per-edge
// resolution provenance and tiered confidence from ADR #2222, so it shares the
// code-call write path and batch semantics.
const batchCanonicalInstantiatesUpsertCypher = `UNWIND $rows AS row
MATCH (source:Function|Class|File {uid: row.caller_entity_id})
MATCH (target:Class|Struct|Enum {uid: row.callee_entity_id})
MERGE (source)-[rel:INSTANTIATES]->(target)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.resolution_method = row.resolution_method,
    rel.evidence_source = row.evidence_source,
    rel.call_kind = row.call_kind`
