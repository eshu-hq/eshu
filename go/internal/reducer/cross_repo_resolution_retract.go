// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func buildResolvedEdgeRetractionIntentRows(
	scopeID string,
	evidenceFacts []relationships.EvidenceFact,
	resolved []relationships.ResolvedRelationship,
	sourceRunID string,
	generationID string,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	sourceRepoIDs := retractionSourceRepoIDs(scopeID, evidenceFacts)
	if len(sourceRepoIDs) == 0 {
		return nil
	}

	resolvedSources := make(map[string]struct{}, len(resolved))
	for _, relationship := range resolved {
		repoID := normalizeReducerRepositoryID(relationship.SourceRepoID)
		if repoID == "" {
			continue
		}
		resolvedSources[repoID] = struct{}{}
	}

	rows := make([]SharedProjectionIntentRow, 0, len(sourceRepoIDs))
	for _, repoID := range sourceRepoIDs {
		if _, ok := resolvedSources[repoID]; ok {
			continue
		}
		rows = append(rows, buildResolvedEdgeRetractionIntentRow(
			repoID,
			scopeID,
			sourceRunID,
			generationID,
			createdAt,
		))
	}
	return rows
}

func buildResolvedEdgeRetractionIntentRow(
	repoID string,
	scopeID string,
	sourceRunID string,
	generationID string,
	createdAt time.Time,
) SharedProjectionIntentRow {
	payload := map[string]any{
		"repo_id":         repoID,
		"action":          "retract",
		"evidence_source": crossRepoEvidenceSource,
		"generation_id":   generationID,
	}

	return BuildSharedProjectionIntent(SharedProjectionIntentInput{
		ProjectionDomain: DomainRepoDependency,
		PartitionKey:     fmt.Sprintf("retract:repo:%s", repoID),
		ScopeID:          scopeID,
		AcceptanceUnitID: repoID,
		RepositoryID:     repoID,
		SourceRunID:      strings.TrimSpace(sourceRunID),
		GenerationID:     generationID,
		Payload:          payload,
		CreatedAt:        createdAt,
	})
}

func retractionSourceRepoIDs(
	scopeID string,
	evidenceFacts []relationships.EvidenceFact,
) []string {
	seen := make(map[string]struct{})
	for _, fact := range evidenceFacts {
		repoID := normalizeReducerRepositoryID(fact.SourceRepoID)
		if repoID == "" {
			continue
		}
		seen[repoID] = struct{}{}
	}
	if len(seen) == 0 {
		if repoID := repoIDFromRelationshipScope(scopeID); repoID != "" {
			seen[repoID] = struct{}{}
		}
	}

	repoIDs := make([]string, 0, len(seen))
	for repoID := range seen {
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)
	return repoIDs
}

func repoIDFromRelationshipScope(scopeID string) string {
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" {
		return ""
	}
	if strings.HasPrefix(scopeID, "git-repository-scope:") {
		return normalizeReducerRepositoryID(scopeID)
	}
	if strings.HasPrefix(scopeID, "repository:") {
		return scopeID
	}
	return ""
}
