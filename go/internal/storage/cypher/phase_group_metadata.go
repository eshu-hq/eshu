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
	// CanonicalPhaseDirectories identifies depth-ordered directory-node writes.
	CanonicalPhaseDirectories = "directories"
	// CanonicalPhaseFiles identifies canonical file-node writes.
	CanonicalPhaseFiles = "files"
	// PhaseGroupModeExecuteOnly tells executors to run a statement outside the
	// default grouped-write path while preserving phase ordering.
	PhaseGroupModeExecuteOnly = "execute_only"
	// PhaseGroupModeGroupedSingleton keeps singleton Cypher shape while allowing
	// the backend executor to batch the statement with same-label entity writes.
	PhaseGroupModeGroupedSingleton = "grouped_singleton"
)
