package cypher

const batchCanonicalCodeCallUpsertCypher = `UNWIND $rows AS row
MATCH (source:Function|Class|File {uid: coalesce(row.caller_entity_id, row.source_entity_id)})
MATCH (target:Function|Class|File {uid: coalesce(row.callee_entity_id, row.target_entity_id)})
MERGE (source)-[rel:CALLS]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser and symbol analysis resolved a code call edge',
    rel.evidence_source = row.evidence_source,
    rel.call_kind = row.call_kind`

const batchCanonicalCodeReferenceUpsertCypher = `UNWIND $rows AS row
MATCH (source:Function|Class|Struct|Interface|TypeAlias|File {uid: row.caller_entity_id})
MATCH (target:Function|Class|Struct|Interface|TypeAlias|File {uid: row.callee_entity_id})
MERGE (source)-[rel:REFERENCES]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser and symbol analysis resolved a code reference edge',
    rel.evidence_source = row.evidence_source,
    rel.call_kind = row.call_kind`

const batchCanonicalCodeInstantiationUpsertCypher = `UNWIND $rows AS row
MATCH (source:Function|Class|Struct|File {uid: row.caller_entity_id})
MATCH (target:Class|Struct {uid: row.callee_entity_id})
MERGE (source)-[rel:INSTANTIATES]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser constructor-call metadata resolved a code instantiation edge',
    rel.evidence_source = row.evidence_source,
    rel.call_kind = row.call_kind`

const batchCanonicalMetaclassUpsertCypher = `UNWIND $rows AS row
MATCH (source:Function|Class|File {uid: row.source_entity_id})
MATCH (target:Function|Class|File {uid: row.target_entity_id})
MERGE (source)-[rel:USES_METACLASS]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser and symbol analysis resolved a Python metaclass edge',
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`

const batchCanonicalInheritanceEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (child:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.child_entity_id})
MATCH (parent:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.parent_entity_id})
MERGE (child)-[rel:INHERITS]->(parent)
SET rel.confidence = 0.95,
    rel.reason = 'Parser entity bases metadata resolved an inheritance edge',
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`

const batchCanonicalImplementsEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (child:Class|Struct {uid: row.child_entity_id})
MATCH (parent:Interface {uid: row.parent_entity_id})
MERGE (child)-[rel:IMPLEMENTS]->(parent)
SET rel.confidence = 0.95,
    rel.reason = 'Parser implemented-interface metadata resolved an implementation edge',
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`

const batchCanonicalInheritanceOverrideUpsertCypher = `UNWIND $rows AS row
MATCH (child:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.child_entity_id})
MATCH (parent:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.parent_entity_id})
MERGE (child)-[rel:OVERRIDES]->(parent)
SET rel.confidence = 0.95,
    rel.reason = 'Parser trait adaptation metadata resolved an override edge',
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`

const batchCanonicalInheritanceAliasUpsertCypher = `UNWIND $rows AS row
MATCH (child:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.child_entity_id})
MATCH (parent:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.parent_entity_id})
MERGE (child)-[rel:ALIASES]->(parent)
SET rel.confidence = 0.95,
    rel.reason = 'Parser trait adaptation metadata resolved an alias edge',
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`
