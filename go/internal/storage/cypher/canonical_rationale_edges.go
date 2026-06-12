package cypher

// Batched UNWIND Cypher for rationale EXPLAINS edges (issue #2230).
//
// An EXPLAINS edge links an identity-only Rationale node — built from an intent
// comment (WHY/HACK/NOTE/TODO/FIXME) that precedes a code entity — to the entity
// it explains. The Rationale node carries identity and a bounded excerpt handle
// only; the comment text stays in the Postgres content/fact store (design 430).
// The template MERGEs the Rationale node inline (no separate node-writer phase).
// Rationale comes from repo-scoped code entities, so the retract anchors on
// rationale.repo_id like the inheritance edges.

const batchCanonicalRationaleExplainsEdgeCypher = `UNWIND $rows AS row
MATCH (target:Function|Class|Struct|Interface|TypeAlias|Enum|File {uid: row.target_entity_id})
MERGE (rationale:Rationale {uid: row.rationale_uid})
SET rationale.type = 'rationale',
    rationale.repo_id = row.repo_id,
    rationale.comment_kind = row.comment_kind,
    rationale.excerpt_hash = row.excerpt_hash,
    rationale.evidence_source = row.evidence_source
MERGE (rationale)-[rel:EXPLAINS]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Intent comment explains the code entity it precedes',
    rel.evidence_source = row.evidence_source,
    rel.comment_kind = row.comment_kind`

// retractRationaleEdgesCypher removes a repository's prior-generation EXPLAINS
// edges by rationale repo id and evidence source. Identity-only Rationale nodes
// are re-MERGEd under their stable uid on the next generation; orphan-node
// cleanup is intentionally out of scope.
const retractRationaleEdgesCypher = `MATCH (rationale:Rationale)-[rel:EXPLAINS]->()
WHERE rationale.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`
