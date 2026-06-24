// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// Batched UNWIND Cypher for documentation DOCUMENTS edges (issue #2231).
//
// A DOCUMENTS edge links an identity-only DocumentationSection node to the code
// entity or workload an exact-resolved documentation entity mention points at.
// The section node carries identity and a bounded excerpt handle only; the
// section body stays in the Postgres content/fact store (design 430). Both
// templates MERGE the section node inline (it has no separate node-writer phase)
// and are scoped by section.scope_id so the retract can remove a prior
// generation's edges without a repository anchor (documentation is scope-scoped,
// not repo-scoped).

const batchCanonicalDocumentationEntityEdgeCypher = `UNWIND $rows AS row
MATCH (target:Function|Class|Struct|Interface|TypeAlias|Enum|File {uid: row.target_entity_id})
MERGE (section:DocumentationSection {uid: row.section_uid})
SET section.type = 'documentation_section',
    section.scope_id = row.scope_id,
    section.document_id = row.document_id,
    section.section_id = row.section_id,
    section.heading_text = row.heading_text,
    section.section_anchor = row.section_anchor,
    section.excerpt_hash = row.excerpt_hash,
    section.evidence_source = row.evidence_source
MERGE (section)-[rel:DOCUMENTS]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Exact documentation entity mention resolved to a code entity',
    rel.evidence_source = row.evidence_source,
    rel.mention_kind = row.mention_kind`

const batchCanonicalDocumentationWorkloadEdgeCypher = `UNWIND $rows AS row
MATCH (target:Workload {id: row.target_entity_id})
MERGE (section:DocumentationSection {uid: row.section_uid})
SET section.type = 'documentation_section',
    section.scope_id = row.scope_id,
    section.document_id = row.document_id,
    section.section_id = row.section_id,
    section.heading_text = row.heading_text,
    section.section_anchor = row.section_anchor,
    section.excerpt_hash = row.excerpt_hash,
    section.evidence_source = row.evidence_source
MERGE (section)-[rel:DOCUMENTS]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Exact documentation entity mention resolved to a workload',
    rel.evidence_source = row.evidence_source,
    rel.mention_kind = row.mention_kind`

// retractDocumentationEdgesCypher removes a scope's prior-generation DOCUMENTS
// edges by section scope id and evidence source. Identity-only section nodes are
// re-MERGEd on the next generation under their stable uid, so they do not
// accumulate duplicates; orphan-section cleanup is intentionally out of scope.
const retractDocumentationEdgesCypher = `MATCH (section:DocumentationSection)-[rel:DOCUMENTS]->()
WHERE section.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const retractDocumentationEdgesByDocumentCypher = `MATCH (section:DocumentationSection)-[rel:DOCUMENTS]->()
WHERE section.scope_id IN $scope_ids
  AND section.document_id IN $document_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const retractDocumentationEdgesBySectionCypher = `MATCH (section:DocumentationSection)-[rel:DOCUMENTS]->()
WHERE section.scope_id IN $scope_ids
  AND section.uid IN $section_uids
  AND rel.evidence_source = $evidence_source
DELETE rel`
