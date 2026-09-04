// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

// The column helpers below name the shape of one relational read model. A test
// queues a ContentReaderQueryResult with the matching helper's columns to say
// which read it is answering, and the fake answers the same read with the same
// helper when no result was queued. Keeping both sides on one function is the
// point: a hand-copied column list in a test drifts from the reader's SELECT
// silently, and the mismatch then surfaces as a scan error inside the handler
// rather than as a harness defect.

// ContentReaderRelationshipReadModelColumns returns the columns the repository
// relationship read model selects, in order.
func ContentReaderRelationshipReadModelColumns() []string {
	return []string{
		"direction", "relationship_type", "source_repo_id", "source_name",
		"target_repo_id", "target_name", "resolved_id", "generation_id",
		"confidence", "evidence_count", "rationale", "resolution_source", "details",
	}
}

// ContentReaderDeploymentEvidenceColumns returns the columns the repository
// deployment-evidence read selects, in order.
func ContentReaderDeploymentEvidenceColumns() []string {
	return []string{
		"direction", "resolved_id", "generation_id", "source_repo_id", "source_name",
		"source_remote_url", "source_scope_id", "target_repo_id", "target_name",
		"target_remote_url", "target_scope_id", "relationship_type", "confidence", "details",
	}
}

// ContentReaderRelationshipEvidenceColumns returns the columns the relationship
// evidence read selects, in order.
func ContentReaderRelationshipEvidenceColumns() []string {
	return []string{
		"resolved_id", "generation_id", "source_repo_id", "source_name",
		"source_entity_id", "target_repo_id", "target_name", "target_entity_id",
		"relationship_type", "confidence", "evidence_count", "rationale",
		"resolution_source", "details", "generation_scope", "generation_run_id",
		"generation_status",
	}
}

// ContentReaderDeadCodeCandidateColumns returns the columns the dead-code
// candidate scan selects, in order.
func ContentReaderDeadCodeCandidateColumns() []string {
	return []string{
		"entity_id", "entity_name", "entity_type", "repo_id", "relative_path",
		"language", "start_line", "end_line", "metadata",
	}
}
