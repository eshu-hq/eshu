// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
)

type containerImageCandidateExplanationState struct {
	ScopeID          string
	ScopeStatus      string
	GenerationID     string
	GenerationStatus string
	WorkStatus       string
	FailureClass     string
	WarningCode      string
	WarningDigest    string
}

// ExplainContainerImageCandidate explains why a deployment image candidate has
// no canonical container image identity without fabricating digest or SBOM
// evidence.
func (s PostgresContainerImageIdentityStore) ExplainContainerImageCandidate(
	ctx context.Context,
	imageRef string,
) (map[string]any, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("container image identity database is required")
	}
	parts, ok := serviceStoryParseImageCandidate(imageRef)
	if !ok {
		return serviceStoryGenericImageCandidateMissingDetail(imageRef, "container_image_identity_missing"), nil
	}
	if parts.Tag == "" && parts.Digest == "" {
		detail, _ := serviceStoryRepoOnlyImageCandidateDetail(imageRef)
		return detail, nil
	}

	rows, err := s.DB.QueryContext(ctx, explainContainerImageCandidateQuery, parts.RepositoryID)
	if err != nil {
		return nil, fmt.Errorf("explain container image candidate: %w", err)
	}
	defer func() { _ = rows.Close() }()

	state := containerImageCandidateExplanationState{}
	if rows.Next() {
		if err := rows.Scan(
			&state.ScopeID,
			&state.ScopeStatus,
			&state.GenerationID,
			&state.GenerationStatus,
			&state.WorkStatus,
			&state.FailureClass,
			&state.WarningCode,
			&state.WarningDigest,
		); err != nil {
			return nil, fmt.Errorf("explain container image candidate: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("explain container image candidate: %w", err)
	}
	return serviceStoryContainerImageCandidateExplanation(parts, state), nil
}

const explainContainerImageCandidateQuery = `
WITH scope_match AS (
    SELECT
        scope.scope_id,
        scope.status AS scope_status,
        generation.generation_id,
        generation.status AS generation_status
    FROM ingestion_scopes AS scope
    LEFT JOIN scope_generations AS generation
      ON generation.scope_id = scope.scope_id
     AND generation.generation_id = scope.active_generation_id
    WHERE scope.scope_id = $1
      AND scope.collector_kind = 'oci_registry'
      AND scope.scope_kind = 'container_registry_repository'
    ORDER BY scope.observed_at DESC
    LIMIT 1
),
work_match AS (
    SELECT work.status, work.last_failure_class
    FROM workflow_work_items AS work
    WHERE work.collector_kind = 'oci_registry'
      AND work.scope_id = $1
    ORDER BY work.updated_at DESC
    LIMIT 1
),
warning_match AS (
    SELECT
        fact.payload->>'warning_code' AS warning_code,
        fact.payload->>'digest' AS warning_digest
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'oci_registry.warning'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND fact.payload->>'repository_id' = $1
    ORDER BY fact.observed_at DESC, fact.fact_id DESC
    LIMIT 1
)
SELECT
    COALESCE((SELECT scope_id FROM scope_match), ''),
    COALESCE((SELECT scope_status FROM scope_match), ''),
    COALESCE((SELECT generation_id FROM scope_match), ''),
    COALESCE((SELECT generation_status FROM scope_match), ''),
    COALESCE((SELECT status FROM work_match), ''),
    COALESCE((SELECT last_failure_class FROM work_match), ''),
    COALESCE((SELECT warning_code FROM warning_match), ''),
    COALESCE((SELECT warning_digest FROM warning_match), '')
`

func serviceStoryContainerImageCandidateExplanation(
	parts serviceStoryImageCandidateParts,
	state containerImageCandidateExplanationState,
) map[string]any {
	reason, collectorScope, action := serviceStoryContainerImageCandidateReason(parts.RepositoryID, state)
	detail := serviceStoryBaseImageCandidateDetail(parts, reason, map[string]any{
		"collector_scope": collectorScope,
		"operator_action": action,
	})
	addNonEmptyString(detail, "oci_scope_id", state.ScopeID)
	addNonEmptyString(detail, "oci_scope_status", state.ScopeStatus)
	addNonEmptyString(detail, "oci_generation_id", state.GenerationID)
	addNonEmptyString(detail, "oci_generation_status", state.GenerationStatus)
	addNonEmptyString(detail, "workflow_status", state.WorkStatus)
	addNonEmptyString(detail, "failure_class", state.FailureClass)
	addNonEmptyString(detail, "collector_warning_code", state.WarningCode)
	addNonEmptyString(detail, "collector_warning_digest", state.WarningDigest)
	return detail
}

func serviceStoryContainerImageCandidateReason(
	repositoryID string,
	state containerImageCandidateExplanationState,
) (string, string, string) {
	if state.ScopeID == "" && state.WorkStatus == "" && state.WarningCode == "" {
		return "oci_registry_target_outside_scope",
			"outside_configured_targets",
			"add an OCI registry collector target for " + repositoryID
	}
	if serviceStoryContainerImageCandidateWorkFailed(state.WorkStatus) {
		return "oci_registry_target_unreadable",
			"configured_unreadable",
			serviceStoryUnreadableOCIRegistryAction(repositoryID, state.FailureClass)
	}
	if state.GenerationStatus == "failed" || state.ScopeStatus == "failed" {
		return "oci_registry_target_unreadable",
			"configured_unreadable",
			serviceStoryUnreadableOCIRegistryAction(repositoryID, state.FailureClass)
	}
	if serviceStoryContainerImageCandidateWorkPending(state.WorkStatus) ||
		state.GenerationStatus == "pending" ||
		state.ScopeStatus == "pending" {
		return "oci_registry_target_collection_pending",
			"configured_pending",
			"wait for or run the configured OCI registry collector target for " + repositoryID
	}
	if state.ScopeID != "" && state.GenerationID == "" && state.WorkStatus == "" && state.WarningCode == "" {
		return "oci_registry_target_collection_pending",
			"configured_pending",
			"wait for or run the configured OCI registry collector target for " + repositoryID
	}
	if state.FailureClass != "" && state.WorkStatus != "completed" {
		return "oci_registry_target_unreadable",
			"configured_unreadable",
			serviceStoryUnreadableOCIRegistryAction(repositoryID, state.FailureClass)
	}
	if state.ScopeID != "" || state.WorkStatus == "completed" || state.WarningCode != "" {
		return "container_image_identity_scanned_missing",
			"configured_scanned",
			"verify the configured OCI registry collector scans the candidate tag or digest for " + repositoryID
	}
	return "container_image_identity_missing",
		"unknown",
		"verify OCI registry collector coverage and reducer image identity facts for this deployment image reference"
}

func serviceStoryContainerImageCandidateWorkFailed(status string) bool {
	switch strings.TrimSpace(status) {
	case "failed_retryable", "failed_terminal":
		return true
	default:
		return false
	}
}

func serviceStoryContainerImageCandidateWorkPending(status string) bool {
	switch strings.TrimSpace(status) {
	case "pending", "claimed", "expired":
		return true
	default:
		return false
	}
}

func serviceStoryUnreadableOCIRegistryAction(repositoryID string, failureClass string) string {
	action := "fix the configured OCI registry collector target for " + repositoryID
	if failureClass = strings.TrimSpace(failureClass); failureClass != "" {
		action += "; current failure class is " + failureClass
	}
	return action
}

func addNonEmptyString(row map[string]any, key string, value string) {
	if value = strings.TrimSpace(value); value != "" {
		row[key] = value
	}
}
