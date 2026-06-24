// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

type workloadMaterializationReplayRequest struct {
	scopeID      string
	generationID string
	entityKey    string
}

func (r *RepoDependencyProjectionRunner) replayWorkloadMaterialization(
	ctx context.Context,
	rows []SharedProjectionIntentRow,
) (int, error) {
	requestCount := 0
	for _, request := range repoDependencyReplayRequests(rows) {
		requestCount++
		if _, err := r.WorkloadMaterializationReplayer.ReplayWorkloadMaterialization(
			ctx,
			request.scopeID,
			request.generationID,
			request.entityKey,
		); err != nil {
			return requestCount, err
		}
	}
	return requestCount, nil
}

func repoDependencyAcceptanceUnitID(row SharedProjectionIntentRow) (string, bool) {
	if value := strings.TrimSpace(row.AcceptanceUnitID); value != "" {
		return value, true
	}
	if key, ok := row.AcceptanceKey(); ok && strings.TrimSpace(key.AcceptanceUnitID) != "" {
		return strings.TrimSpace(key.AcceptanceUnitID), true
	}
	if value := strings.TrimSpace(row.RepositoryID); value != "" {
		return value, true
	}
	return "", false
}

func repoDependencyReplayRequests(rows []SharedProjectionIntentRow) []workloadMaterializationReplayRequest {
	seen := make(map[string]struct{}, len(rows))
	requests := make([]workloadMaterializationReplayRequest, 0, len(rows))
	for _, row := range rows {
		scopeID := strings.TrimSpace(row.ScopeID)
		generationID := strings.TrimSpace(row.GenerationID)
		entityKey := repoDependencyReplayEntityKey(row)
		if scopeID == "" || generationID == "" || entityKey == "" {
			continue
		}
		key := scopeID + "|" + generationID + "|" + entityKey
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		requests = append(requests, workloadMaterializationReplayRequest{
			scopeID:      scopeID,
			generationID: generationID,
			entityKey:    entityKey,
		})
	}
	sort.Slice(requests, func(i, j int) bool {
		if requests[i].scopeID != requests[j].scopeID {
			return requests[i].scopeID < requests[j].scopeID
		}
		if requests[i].generationID != requests[j].generationID {
			return requests[i].generationID < requests[j].generationID
		}
		return requests[i].entityKey < requests[j].entityKey
	})
	return requests
}

func repoDependencyReplayEntityKey(row SharedProjectionIntentRow) string {
	if strings.EqualFold(
		strings.TrimSpace(repoDependencyPayloadString(row, "relationship_type")),
		"PROVISIONS_DEPENDENCY_FOR",
	) {
		if targetRepoID := strings.TrimSpace(repoDependencyPayloadString(row, "target_repo_id")); targetRepoID != "" {
			return repoDependencyReplayRepoKey(targetRepoID)
		}
	}

	repoID := strings.TrimSpace(row.RepositoryID)
	if repoID == "" {
		repoID = strings.TrimSpace(repoDependencyPayloadString(row, "repo_id"))
	}
	if repoID == "" {
		repoID = strings.TrimSpace(row.AcceptanceUnitID)
	}
	if repoID == "" {
		return ""
	}
	return repoDependencyReplayRepoKey(repoID)
}

// repoDependencyReplayRepoKey normalizes repository identifiers to the
// workload-materialization entity-key form used by reducer intents.
func repoDependencyReplayRepoKey(repoID string) string {
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(repoID), "repo:") {
		return repoID
	}
	if alias := normalizedEntityKey(repoID); alias != "" {
		return "repo:" + alias
	}
	return "repo:" + repoID
}

func buildRepoDependencyRetractRows(repositoryIDs []string) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		rows = append(rows, SharedProjectionIntentRow{
			RepositoryID: repositoryID,
			Payload:      map[string]any{"repo_id": repositoryID},
		})
	}
	return rows
}

func groupRepoDependencyUpsertRows(rows []SharedProjectionIntentRow) map[string][]SharedProjectionIntentRow {
	groups := make(map[string][]SharedProjectionIntentRow)
	for _, row := range rows {
		if !isRepoDependencyUpsertRow(row) {
			continue
		}
		source := repoDependencyRowEvidenceSource(row)
		groups[source] = append(groups[source], row)
	}
	return groups
}

func isRepoDependencyUpsertRow(row SharedProjectionIntentRow) bool {
	if row.Payload == nil {
		return false
	}
	action := strings.TrimSpace(repoDependencyPayloadString(row, "action"))
	if action == "delete" || action == "retract" {
		return false
	}

	repoID := strings.TrimSpace(repoDependencyPayloadString(row, "repo_id"))
	if repoID == "" {
		repoID = strings.TrimSpace(row.RepositoryID)
	}
	if repoID == "" {
		return false
	}
	if relationshipType := strings.TrimSpace(repoDependencyPayloadString(row, "relationship_type")); relationshipType == string(edgetype.RunsOn) {
		return strings.TrimSpace(repoDependencyPayloadString(row, "platform_id")) != ""
	}
	return strings.TrimSpace(repoDependencyPayloadString(row, "target_repo_id")) != ""
}

func repoDependencyEvidenceSources(rows []SharedProjectionIntentRow) []string {
	seen := make(map[string]struct{}, len(rows))
	sources := make([]string, 0, len(rows))
	for _, row := range rows {
		source := repoDependencyRowEvidenceSource(row)
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return sources
}

func repoDependencyRowEvidenceSource(row SharedProjectionIntentRow) string {
	if source := strings.TrimSpace(repoDependencyPayloadString(row, "evidence_source")); source != "" {
		return source
	}
	return defaultEvidenceSource
}

func repoDependencyPayloadString(row SharedProjectionIntentRow, key string) string {
	if row.Payload == nil {
		return ""
	}
	value, ok := row.Payload[key]
	if !ok {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(str)
}

func uniqueGenerationIDs(rows []SharedProjectionIntentRow) []string {
	seen := make(map[string]struct{}, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		generationID := strings.TrimSpace(row.GenerationID)
		if generationID == "" {
			continue
		}
		if _, ok := seen[generationID]; ok {
			continue
		}
		seen[generationID] = struct{}{}
		ids = append(ids, generationID)
	}
	sort.Strings(ids)
	return ids
}
