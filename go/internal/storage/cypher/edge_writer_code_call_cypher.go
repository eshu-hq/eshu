package cypher

const canonicalCodeCallUpsertCypher = `MATCH (source {id: $caller_entity_id})
MATCH (target {id: $callee_entity_id})
MERGE (source)-[rel:CALLS]->(target)
SET rel.confidence = $confidence,
    rel.reason = $reason,
    rel.resolution_method = $resolution_method,
    rel.evidence_source = $evidence_source,
    rel.call_kind = $call_kind`

const canonicalJSXComponentReferenceUpsertCypher = `MATCH (source {id: $caller_entity_id})
MATCH (target {id: $callee_entity_id})
MERGE (source)-[rel:REFERENCES]->(target)
SET rel.confidence = $confidence,
    rel.reason = $reason,
    rel.resolution_method = $resolution_method,
    rel.evidence_source = $evidence_source,
    rel.call_kind = $call_kind`

const canonicalMetaclassUpsertCypher = `MATCH (source {id: $caller_entity_id})
MATCH (target {id: $callee_entity_id})
MERGE (source)-[rel:USES_METACLASS]->(target)
SET rel.confidence = $confidence,
    rel.reason = $reason,
    rel.resolution_method = $resolution_method,
    rel.evidence_source = $evidence_source,
    rel.relationship_type = $relationship_type`

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
