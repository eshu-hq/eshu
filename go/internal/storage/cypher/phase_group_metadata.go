// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

const (
	// StatementMetadataPhaseKey tags a canonical-write statement with the
	// writer phase that produced it so narrower executors can preserve phase
	// ordering and diagnostics without parsing Cypher.
	StatementMetadataPhaseKey = "_eshu_phase"
	// StatementMetadataEntityLabelKey tags canonical entity statements with the
	// concrete entity label they are writing so backend-specific executors can
	// tune grouped execution without parsing summaries or Cypher text.
	StatementMetadataEntityLabelKey = "_eshu_entity_label"
	// StatementMetadataPhaseGroupModeKey tags a canonical-write statement with
	// group-execution handling hints such as execute-only singleton fallback.
	StatementMetadataPhaseGroupModeKey = "_eshu_phase_group_mode"
	// StatementMetadataSummaryKey carries a human-readable first-statement
	// summary used only for logging and error wrapping.
	StatementMetadataSummaryKey = "_eshu_statement_summary"
	// StatementMetadataScopeIDKey carries the source-local scope for backend
	// diagnostics and is stripped before Cypher execution.
	StatementMetadataScopeIDKey = "_eshu_scope_id"
	// StatementMetadataGenerationIDKey carries the source-local generation for
	// backend diagnostics and is stripped before Cypher execution.
	StatementMetadataGenerationIDKey = "_eshu_generation_id"

	// CanonicalPhaseEntities identifies the canonical entity-node write phase.
	CanonicalPhaseEntities = "entities"
	// CanonicalPhaseEntityContainment identifies file-to-entity containment
	// writes that may need backend-specific grouping limits.
	CanonicalPhaseEntityContainment = "entity_containment"
	// CanonicalPhaseDirectories identifies directory-node writes (MERGE by path,
	// no parent MATCH).
	CanonicalPhaseDirectories = "directories"
	// CanonicalPhaseDirectoryEdges identifies the directory parent CONTAINS edge
	// writes. It runs after CanonicalPhaseDirectories commits so each parent
	// (Repository or parent Directory) is already visible to its MATCH — required
	// on NornicDB, which hides same-transaction MERGEs from a later MATCH.
	CanonicalPhaseDirectoryEdges = "directory_edges"
	// CanonicalPhaseFiles identifies canonical file-node writes.
	CanonicalPhaseFiles = "files"
	// CanonicalPhaseStructuralEdges identifies canonical structural-edge writes:
	// IMPORTS, HAS_PARAMETER, class/nested containment, and the Atlantis, Flux,
	// GitLab, and Helm family edges. Its statements are row-batched, so its
	// transaction size is governed by a narrow statement budget rather than the
	// broad phase-group default (issue #6070).
	CanonicalPhaseStructuralEdges = "structural_edges"
	// PhaseGroupModeExecuteOnly tells executors to run a statement outside the
	// default grouped-write path while preserving phase ordering.
	PhaseGroupModeExecuteOnly = "execute_only"
	// PhaseGroupModeGroupedSingleton keeps singleton Cypher shape while allowing
	// the backend executor to batch the statement with same-label entity writes.
	PhaseGroupModeGroupedSingleton = "grouped_singleton"
)
