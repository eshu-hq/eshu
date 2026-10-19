// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"strings"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// Dedicated [rel:CONTAINS] edge retract for the semantic_entity_containment
// materialized-edge family (#8359, split out of #6228 by the 2026-10-17
// arbiter verdict).
//
// The semantic writer shares its nodes and the CONTAINS type with the
// canonical node writer (canonical_node_cypher.go stamps its CONTAINS edges
// 'projector/canonical'). The old retract DETACH DELETEd the shared nodes,
// taking every foreign edge on them — canonical containment, call edges,
// documentation edges — with it. This retract is the single-writer owner of
// the semantic CONTAINS population instead:
//
//   - It deletes only [rel:CONTAINS] edges between the endpoint labels this
//     writer materializes (File or ImplBlock into the eleven semantic
//     entity labels).
//   - It is fenced by evidence_source, in two statements: one deletes
//     edges carrying this writer's rel stamp (the only fence that reaches
//     Rust ImplBlock ownership edges in canonical-node mode, where the
//     Function node is canonical-owned), and one deletes containment into
//     semantic-owned nodes, which reaps edges written before the rel stamp
//     existed. Canonical-writer edges carry 'projector/canonical' on the
//     edge and target canonical-owned nodes (or endpoint labels outside
//     this pattern entirely), so both statements fence them out. The two
//     statements stay separate — rather than one OR predicate — so each
//     keeps the parameter-bounded shape the grouped-retry classifier
//     accepts as replay-safe (writer.go hasParameterBoundedPredicate).
//   - Node lifecycle follows the edges: a semantic-owned node is reaped
//     only once it carries no edge at all, so a node another writer still
//     references survives until that writer retracts its own edges, and a
//     later semantic write reaps it once edgeless. The reap is a plain
//     DELETE, never DETACH: an edge landing concurrently fails the delete
//     and the work-item retry converges, instead of silently dropping the
//     foreign edge.
//
// Anchor and cardinality: the target label alternation plus the repo_id /
// path scope predicate is the same anchor the old node retract used, so the
// matched node population is unchanged; the edge pattern expands each
// matched node's incoming CONTAINS adjacency (degree 1–2 in practice: its
// File containment, plus ImplBlock ownership for Rust functions).

// SemanticEntityEvidenceSource is the evidence_source stamp this writer puts
// on its nodes and, since #8359, on the CONTAINS edges it MERGEs. Exported
// so the materialized-edge registry can fence this family's asserted edge
// population by the same predicate the retract deletes by.
const SemanticEntityEvidenceSource = semanticEntityEvidenceSource

// semanticEntityLabels alternation shared by every retract variant below.
// Held as one const so the broad, delta, and label-scoped shapes cannot
// drift apart; TestSemanticEntityUpsertLabelsAreAnchorLabels bounds the
// write-side label set this must cover.
const semanticEntityLabelAlternation = "Annotation|Typedef|TypeAlias|TypeAnnotation|Component|Module|ImplBlock|Protocol|ProtocolImplementation|Variable|Function"

// semanticContainmentSourceLabels are the only source labels this writer
// MERGEs CONTAINS from: File for entity containment, ImplBlock for Rust
// impl-block function ownership.
const semanticContainmentSourceLabels = "File|ImplBlock"

// RetractSemanticEntityContainmentEdgesCypher deletes this writer's stale
// stamped CONTAINS edges for whole repositories. It is the production
// retract the materialized-edge registry holds for
// semantic_entity_containment.
const RetractSemanticEntityContainmentEdgesCypher = `MATCH (src:` + semanticContainmentSourceLabels + `)-[rel:CONTAINS]->(n:` + semanticEntityLabelAlternation + `)
WHERE n.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// RetractSemanticEntityContainmentDeltaEdgesCypher is the delta-projection
// variant, scoped to the touched file paths instead of whole repositories.
const RetractSemanticEntityContainmentDeltaEdgesCypher = `MATCH (src:` + semanticContainmentSourceLabels + `)-[rel:CONTAINS]->(n:` + semanticEntityLabelAlternation + `)
WHERE n.path IN $file_paths
  AND rel.evidence_source = $evidence_source
DELETE rel`

// RetractSemanticEntityContainmentNodeScopedEdgesCypher deletes
// containment into semantic-owned nodes regardless of the edge stamp,
// reaping edges written before the rel stamp existed. Endpoint labels
// fence out the canonical writer's other CONTAINS shapes
// (Repository/Directory/Class sources, Directory/File targets). Exported
// so the materialized-edge registry can hold it as a retract variant.
const RetractSemanticEntityContainmentNodeScopedEdgesCypher = `MATCH (src:` + semanticContainmentSourceLabels + `)-[rel:CONTAINS]->(n:` + semanticEntityLabelAlternation + `)
WHERE n.repo_id IN $repo_ids
  AND n.evidence_source = $evidence_source
DELETE rel`

// RetractSemanticEntityContainmentDeltaNodeScopedEdgesCypher is the
// delta-projection variant of the node-scoped retract.
const RetractSemanticEntityContainmentDeltaNodeScopedEdgesCypher = `MATCH (src:` + semanticContainmentSourceLabels + `)-[rel:CONTAINS]->(n:` + semanticEntityLabelAlternation + `)
WHERE n.path IN $file_paths
  AND n.evidence_source = $evidence_source
DELETE rel`

// semanticEntityReapCypher deletes semantic-owned nodes that no longer
// carry any edge, for whole repositories. It runs after the edge retract in
// the same statement group.
const semanticEntityReapCypher = `MATCH (n:` + semanticEntityLabelAlternation + `)
WHERE n.repo_id IN $repo_ids
  AND n.evidence_source = $evidence_source
  AND NOT EXISTS { MATCH (n)-[]-() }
DELETE n`

// semanticEntityDeltaReapCypher is the delta-projection reap variant.
const semanticEntityDeltaReapCypher = `MATCH (n:` + semanticEntityLabelAlternation + `)
WHERE n.path IN $file_paths
  AND n.evidence_source = $evidence_source
  AND NOT EXISTS { MATCH (n)-[]-() }
DELETE n`

// SemanticEntityContainmentSourceLabels returns the source endpoint labels
// of this family's CONTAINS edges, derived from the held retract
// alternation so the materialized-edge registry's endpoint constraint
// cannot drift from the pattern the retract deletes.
func SemanticEntityContainmentSourceLabels() []string {
	return strings.Split(semanticContainmentSourceLabels, "|")
}

// SemanticEntityContainmentTargetLabels returns the target endpoint labels
// of this family's CONTAINS edges, derived from the held retract
// alternation for the same reason.
func SemanticEntityContainmentTargetLabels() []string {
	return strings.Split(semanticEntityLabelAlternation, "|")
}

// SemanticEntityContainmentUpsertCypher is the representative containment
// write template the materialized-edge registry derives this family's
// MERGE identity from. All eleven per-label templates share its shape —
// `MERGE (f)-[rel:CONTAINS]->(n)` on the two endpoint nodes alone, with the
// evidence_source stamp SET after the MERGE — and
// TestSemanticContainmentWritesStampEdgeEvidenceSource holds every one of
// them to it.
const SemanticEntityContainmentUpsertCypher = semanticFunctionUpsertCypher

// Per-label variants name ONE source label and ONE target label. A label
// alternation inside a relationship pattern matches nothing on the pinned
// NornicDB (measured 2026-10-17 against the fix-500 1.3.3 image: single
// labels match, `File|ImplBlock` or `Function|Variable` alternations
// return zero rows), and the label-scoped and canonical-node dispatches
// are the NornicDB-facing modes. The broad alternation consts above stay
// for the Neo4j broad mode, where the same shapes are live-proven.

func semanticEntityLabelEdgeRetractCypher(sourceLabel, label string) string {
	return "MATCH (src:" + sourceLabel + ")-[rel:CONTAINS]->(n:" + label + ")\n" +
		"WHERE n.repo_id IN $repo_ids\n" +
		"  AND rel.evidence_source = $evidence_source\n" +
		"DELETE rel"
}

func semanticEntityLabelNodeScopedRetractCypher(sourceLabel, label string) string {
	return "MATCH (src:" + sourceLabel + ")-[rel:CONTAINS]->(n:" + label + ")\n" +
		"WHERE n.repo_id IN $repo_ids\n" +
		"  AND n.evidence_source = $evidence_source\n" +
		"DELETE rel"
}

func semanticEntityDeltaLabelEdgeRetractCypher(sourceLabel, label string) string {
	return "MATCH (src:" + sourceLabel + ")-[rel:CONTAINS]->(n:" + label + ")\n" +
		"WHERE n.path IN $file_paths\n" +
		"  AND rel.evidence_source = $evidence_source\n" +
		"DELETE rel"
}

func semanticEntityDeltaLabelNodeScopedRetractCypher(sourceLabel, label string) string {
	return "MATCH (src:" + sourceLabel + ")-[rel:CONTAINS]->(n:" + label + ")\n" +
		"WHERE n.path IN $file_paths\n" +
		"  AND n.evidence_source = $evidence_source\n" +
		"DELETE rel"
}

// semanticLabelEdgeRetractStatements builds the per-label edge-retract
// pair (stamped, then node-scoped) for every plan label from File, plus
// the ImplBlock pair into Function for Rust ownership edges. Shared by
// the label-scoped and canonical-node dispatches.
func semanticLabelEdgeRetractStatements(repoIDs []string) []sourcecypher.Statement {
	plans := semanticEntityPlans()
	stmts := make([]sourcecypher.Statement, 0, 2*len(plans)+2)
	for _, plan := range plans {
		stmts = append(stmts,
			semanticEdgeRetractStatement(semanticEntityLabelEdgeRetractCypher("File", plan.label), plan.label, repoIDs),
			semanticEdgeRetractStatement(semanticEntityLabelNodeScopedRetractCypher("File", plan.label), plan.label, repoIDs),
		)
		if plan.label == "Function" {
			stmts = append(stmts,
				semanticEdgeRetractStatement(semanticEntityLabelEdgeRetractCypher("ImplBlock", plan.label), plan.label, repoIDs),
				semanticEdgeRetractStatement(semanticEntityLabelNodeScopedRetractCypher("ImplBlock", plan.label), plan.label, repoIDs),
			)
		}
	}
	return stmts
}

// semanticDeltaLabelEdgeRetractStatements is the delta counterpart.
func semanticDeltaLabelEdgeRetractStatements(filePaths []string) []sourcecypher.Statement {
	plans := semanticEntityPlans()
	stmts := make([]sourcecypher.Statement, 0, 2*len(plans)+2)
	for _, plan := range plans {
		stmts = append(stmts,
			semanticDeltaEdgeRetractStatement(semanticEntityDeltaLabelEdgeRetractCypher("File", plan.label), plan.label, filePaths),
			semanticDeltaEdgeRetractStatement(semanticEntityDeltaLabelNodeScopedRetractCypher("File", plan.label), plan.label, filePaths),
		)
		if plan.label == "Function" {
			stmts = append(stmts,
				semanticDeltaEdgeRetractStatement(semanticEntityDeltaLabelEdgeRetractCypher("ImplBlock", plan.label), plan.label, filePaths),
				semanticDeltaEdgeRetractStatement(semanticEntityDeltaLabelNodeScopedRetractCypher("ImplBlock", plan.label), plan.label, filePaths),
			)
		}
	}
	return stmts
}

func semanticEntityLabelReapCypher(label string) string {
	return "MATCH (n:" + label + ")\n" +
		"WHERE n.repo_id IN $repo_ids\n" +
		"  AND n.evidence_source = $evidence_source\n" +
		"  AND NOT EXISTS { MATCH (n)-[]-() }\n" +
		"DELETE n"
}

func semanticEntityDeltaLabelReapCypher(label string) string {
	return "MATCH (n:" + label + ")\n" +
		"WHERE n.path IN $file_paths\n" +
		"  AND n.evidence_source = $evidence_source\n" +
		"  AND NOT EXISTS { MATCH (n)-[]-() }\n" +
		"DELETE n"
}

// semanticEdgeRetractStatement builds one full-repo retract statement.
func semanticEdgeRetractStatement(cypher, label string, repoIDs []string) sourcecypher.Statement {
	params := map[string]any{
		"repo_ids":                               repoIDs,
		"evidence_source":                        semanticEntityEvidenceSource,
		sourcecypher.StatementMetadataSummaryKey: semanticEntityRetractStatementSummary(label, repoIDs),
	}
	if label != "all" {
		params[sourcecypher.StatementMetadataEntityLabelKey] = label
	}
	return sourcecypher.Statement{Operation: sourcecypher.OperationCanonicalRetract, Cypher: cypher, Parameters: params}
}

// semanticDeltaEdgeRetractStatement builds one delta retract statement.
func semanticDeltaEdgeRetractStatement(cypher, label string, filePaths []string) sourcecypher.Statement {
	params := map[string]any{
		"file_paths":                             filePaths,
		"evidence_source":                        semanticEntityEvidenceSource,
		sourcecypher.StatementMetadataSummaryKey: semanticEntityDeltaRetractStatementSummary(label, filePaths),
	}
	if label != "all" {
		params[sourcecypher.StatementMetadataEntityLabelKey] = label
	}
	return sourcecypher.Statement{Operation: sourcecypher.OperationCanonicalRetract, Cypher: cypher, Parameters: params}
}
