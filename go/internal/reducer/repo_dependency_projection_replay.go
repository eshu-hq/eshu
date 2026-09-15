// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

type workloadMaterializationReplayRequest struct {
	scopeID      string
	generationID string
	entityKey    string
}

// RepoDependencyReadinessFencePayloadKey stores the RUNS_ON input-set fence
// carried by a workload-materialization replay intent.
const RepoDependencyReadinessFencePayloadKey = "repo_dependency_readiness_fence"

// RepoDependencyReadinessRepoIDPayloadKey stores the repository acceptance
// unit whose token-scoped workload phase must be published.
const RepoDependencyReadinessRepoIDPayloadKey = "repo_dependency_readiness_repo_id"

const repoDependencyReadinessFenceSourceRunPrefix = "repo-dependency:"

type workloadMaterializationFenceRequest struct {
	workloadMaterializationReplayRequest
	repoID       string
	fence        string
	readinessKey GraphProjectionPhaseKey
}

// WorkloadMaterializationFenceReplayer schedules a workload replay carrying a
// deterministic RUNS_ON input fence.
type WorkloadMaterializationFenceReplayer interface {
	ReplayWorkloadMaterializationForFence(
		ctx context.Context,
		scopeID string,
		generationID string,
		entityKey string,
		repoID string,
		fence string,
	) (bool, error)
}

// RepoDependencyReadinessFenceSourceRunID returns the exact source-run
// identity used by token-scoped workload readiness publications.
func RepoDependencyReadinessFenceSourceRunID(fence string) string {
	return repoDependencyReadinessFenceSourceRunPrefix + strings.TrimSpace(fence)
}

func (r *RepoDependencyProjectionRunner) replayWorkloadMaterialization(
	ctx context.Context,
	rows []SharedProjectionIntentRow,
) (int, error) {
	requestCount := 0
	for _, request := range repoDependencyReplayRequests(rows) {
		requestCount++
		replayed, err := r.WorkloadMaterializationReplayer.ReplayWorkloadMaterialization(
			ctx,
			request.scopeID,
			request.generationID,
			request.entityKey,
		)
		if err != nil {
			return requestCount, err
		}
		if !replayed {
			return requestCount, fmt.Errorf(
				"workload materialization replay was not scheduled for scope %q generation %q entity %q",
				request.scopeID,
				request.generationID,
				request.entityKey,
			)
		}
	}
	return requestCount, nil
}

func (r *RepoDependencyProjectionRunner) replayWorkloadMaterializationForFence(
	ctx context.Context,
	requests []workloadMaterializationFenceRequest,
) (int, error) {
	replayer, ok := r.WorkloadMaterializationReplayer.(WorkloadMaterializationFenceReplayer)
	if !ok {
		return 0, fmt.Errorf("repo dependency RUNS_ON workload replayer does not support readiness fences")
	}
	for i, request := range requests {
		replayed, err := replayer.ReplayWorkloadMaterializationForFence(
			ctx,
			request.scopeID,
			request.generationID,
			request.entityKey,
			request.repoID,
			request.fence,
		)
		if err != nil {
			return i + 1, err
		}
		if !replayed {
			return i + 1, fmt.Errorf(
				"workload materialization fenced replay was not scheduled for scope %q generation %q entity %q",
				request.scopeID,
				request.generationID,
				request.entityKey,
			)
		}
	}
	return len(requests), nil
}

func repoDependencyRunsOnFenceRequests(
	rows []SharedProjectionIntentRow,
) ([]workloadMaterializationFenceRequest, error) {
	type fenceGroup struct {
		request     workloadMaterializationReplayRequest
		repoID      string
		fenceInputs []string
	}
	groups := make(map[string]*fenceGroup, len(rows))
	for _, row := range rows {
		repoID := strings.TrimSpace(row.RepositoryID)
		if repoID == "" {
			repoID = strings.TrimSpace(repoDependencyPayloadString(row, "repo_id"))
		}
		request := workloadMaterializationReplayRequest{
			scopeID:      strings.TrimSpace(row.ScopeID),
			generationID: strings.TrimSpace(row.GenerationID),
			entityKey:    repoDependencyReplayEntityKey(row),
		}
		groupKey := request.scopeID + "\x00" + request.generationID + "\x00" + request.entityKey + "\x00" + repoID
		group := groups[groupKey]
		if group == nil {
			group = &fenceGroup{request: request, repoID: repoID}
			groups[groupKey] = group
		}
		intentID := strings.TrimSpace(row.IntentID)
		if intentID == "" || row.CreatedAt.IsZero() {
			return nil, fmt.Errorf("repo dependency RUNS_ON workload readiness fence requires intent id and created_at")
		}
		group.fenceInputs = append(
			group.fenceInputs,
			intentID+"\x00"+row.CreatedAt.UTC().Format(time.RFC3339Nano),
		)
	}

	groupKeys := make([]string, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)
	requests := make([]workloadMaterializationFenceRequest, 0, len(groupKeys))
	for _, groupKey := range groupKeys {
		group := groups[groupKey]
		sort.Strings(group.fenceInputs)
		digest := sha256.Sum256([]byte(strings.Join(group.fenceInputs, "\x00")))
		fence := fmt.Sprintf("%x", digest[:])
		key := workloadMaterializationRepoReadinessKey(
			group.request.scopeID,
			group.repoID,
			group.request.generationID,
		)
		key.SourceRunID = RepoDependencyReadinessFenceSourceRunID(fence)
		if err := key.Validate(); err != nil {
			return nil, fmt.Errorf("build repo dependency RUNS_ON workload readiness fence: %w", err)
		}
		requests = append(requests, workloadMaterializationFenceRequest{
			workloadMaterializationReplayRequest: group.request,
			repoID:                               group.repoID,
			fence:                                fence,
			readinessKey:                         key,
		})
	}
	return requests, nil
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

// repoDependencyRunsOnRows returns the active RUNS_ON rows whose graph write
// requires a committed WorkloadInstance prerequisite.
func repoDependencyRunsOnRows(rows []SharedProjectionIntentRow) []SharedProjectionIntentRow {
	runsOn := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		if strings.EqualFold(
			strings.TrimSpace(repoDependencyPayloadString(row, "relationship_type")),
			"RUNS_ON",
		) {
			runsOn = append(runsOn, row)
		}
	}
	return runsOn
}

func (r *RepoDependencyProjectionRunner) ensureRunsOnWorkloadReadiness(
	ctx context.Context,
	rows []SharedProjectionIntentRow,
) (bool, int, time.Duration, error) {
	runsOn := repoDependencyRunsOnRows(rows)
	if len(runsOn) == 0 {
		return true, 0, 0, nil
	}
	if r.WorkloadReadinessPrefetch == nil {
		return false, 0, 0, fmt.Errorf("repo dependency RUNS_ON workload readiness prefetch is required")
	}
	requests, err := repoDependencyRunsOnFenceRequests(runsOn)
	if err != nil {
		return false, 0, 0, err
	}
	keys := make([]GraphProjectionPhaseKey, 0, len(requests))
	for _, request := range requests {
		keys = append(keys, request.readinessKey)
	}
	lookup, err := r.WorkloadReadinessPrefetch(
		ctx,
		keys,
		GraphProjectionPhaseWorkloadMaterialization,
	)
	if err != nil {
		return false, 0, 0, fmt.Errorf("prefetch repo dependency RUNS_ON workload readiness: %w", err)
	}
	if lookup == nil {
		return false, 0, 0, fmt.Errorf("prefetch repo dependency RUNS_ON workload readiness returned nil lookup")
	}
	missing := make([]workloadMaterializationFenceRequest, 0, len(requests))
	for _, request := range requests {
		ready, found := lookup(request.readinessKey, GraphProjectionPhaseWorkloadMaterialization)
		if ready && found {
			continue
		}
		missing = append(missing, request)
	}
	if len(missing) == 0 {
		return true, 0, 0, nil
	}
	if r.WorkloadMaterializationReplayer == nil {
		return false, 0, 0, fmt.Errorf("repo dependency RUNS_ON workload readiness requires workload replayer")
	}
	replayStart := time.Now()
	replayed, err := r.replayWorkloadMaterializationForFence(ctx, missing)
	replayDuration := time.Since(replayStart)
	if err != nil {
		return false, replayed, replayDuration, fmt.Errorf("replay workload materialization for RUNS_ON readiness: %w", err)
	}
	return false, replayed, replayDuration, nil
}
