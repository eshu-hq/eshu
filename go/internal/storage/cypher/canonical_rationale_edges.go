// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "strings"

// Batched UNWIND Cypher for rationale EXPLAINS edges (issue #2230).
//
// An EXPLAINS edge links an identity-only Rationale node — built from an intent
// comment (WHY/HACK/NOTE/TODO/FIXME) that precedes a code entity — to the entity
// it explains. The Rationale node carries identity and a bounded excerpt handle
// only; the comment text stays in the Postgres content/fact store (design 430).
// The template MERGEs the Rationale node inline (no separate node-writer phase).
// Rationale comes from repo-scoped code entities. Full-refresh retracts anchor
// on rationale.repo_id; delta-generation retracts anchor on target.path so a
// changed file cannot delete other files' EXPLAINS truth.

// rationaleExplainsTargetLabels is the single source of truth for the code
// entity labels an EXPLAINS edge can target. The write template's target
// disjunction and the per-label delta retract statements are both built from
// it, so a label added to one side cannot silently miss the other.
var rationaleExplainsTargetLabels = []string{
	"Function", "Class", "Struct", "Interface", "TypeAlias", "Enum", "File",
}

// batchCanonicalRationaleExplainsEdgeCypher targets its MATCH with a label
// disjunction plus an inline {uid: ...} anchor. Probed on NornicDB v1.1.11:
// this UNWIND + disjunction + inline-property shape matches and creates every
// edge, unlike a bare MATCH whose disjunction-labeled node is filtered by a
// WHERE predicate, which matches zero rows (#5116 — the reason the delta
// retract fans out per target label instead).
var batchCanonicalRationaleExplainsEdgeCypher = `UNWIND $rows AS row
MATCH (target:` + strings.Join(rationaleExplainsTargetLabels, "|") + ` {uid: row.target_entity_id})
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

// retractCanonicalRationaleEdgesCypher removes both the canonical rationale
// provenance and the bounded legacy provenance written before #5998 corrected
// the runner mapping. Custom rationale writers continue to use the exact-source
// statement above.
const retractCanonicalRationaleEdgesCypher = `MATCH (rationale:Rationale)-[rel:EXPLAINS]->()
WHERE rationale.repo_id IN $repo_ids
  AND rel.evidence_source IN $evidence_sources
DELETE rel`

// probeRationaleEdgesCypher mirrors retractRationaleEdgesCypher's MATCH/WHERE
// exactly, replacing DELETE rel with a bounded read, so RetractEdges can probe
// whether the paired retract would remove anything before paying for it
// (#5998: on the pinned NornicDB build the DELETE shape above costs ~18s per
// STATEMENT regardless of rows deleted, because cost tracks store size, and one
// statement binds every repository in the RetractEdges batch
// (ledger:5998-zero-row-explains-delete-large-store) -- do not multiply that
// figure by a repository count. The identical MATCH run as a read stays around
// 21ms (ledger:5998-explains-existence-probe-read), though that row timed the
// pre-change RETURN rel shape and bounds the shipped RETURN true LIMIT 1 from
// above rather than measuring it). Any drift between this MATCH/WHERE and the retract's
// makes the probe answer a different question than the delete it guards.
//
// RETURN true LIMIT 1, not RETURN rel LIMIT 1 (review F11): the probe only
// needs to answer "does at least one row match", the same existence question
// canonicalCodeTargetExistsCypher (canonical_check.go) already asks with this
// exact shape. Returning the matched relationship would additionally depend
// on Bolt's relationship-value serialization for a value the Go side (which
// only inspects QueryCypherExists' bool) never reads, for no benefit.
const probeRationaleEdgesCypher = `MATCH (rationale:Rationale)-[rel:EXPLAINS]->()
WHERE rationale.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
RETURN true LIMIT 1`

// probeCanonicalRationaleEdgesCypher mirrors retractCanonicalRationaleEdgesCypher's
// MATCH/WHERE exactly (the combined canonical-plus-legacy-source shape); see
// probeRationaleEdgesCypher for why the shapes and the RETURN true LIMIT 1
// terminal clause must stay identical.
const probeCanonicalRationaleEdgesCypher = `MATCH (rationale:Rationale)-[rel:EXPLAINS]->()
WHERE rationale.repo_id IN $repo_ids
  AND rel.evidence_source IN $evidence_sources
RETURN true LIMIT 1`

// The delta (by-file) EXPLAINS retract is built per target label by
// buildRationaleDeltaRetractStatements in edge_writer_rationale_labels.go, not
// as a single constant: on NornicDB v1.1.11 a bare MATCH whose target carries
// a node-label disjunction matches zero rows (probed — the disjunction retract
// deleted nothing while the same per-label statements deleted every edge), so
// a single combined statement silently left the changed file's stale EXPLAINS
// edges behind (#5116 sibling).
