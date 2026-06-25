// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const codeCallPartitionKeyVersion = "code-calls:v1"

type codeCallDeltaFileScope struct {
	filePaths      []string
	partitionPaths []string
}

func buildCodeCallProjectionContexts(envelopes []facts.Envelope, generationID string) map[string]ProjectionContext {
	contextByRepoID := make(map[string]ProjectionContext)
	for _, env := range envelopes {
		if env.FactKind != "repository" {
			continue
		}

		repositoryID := payloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			repositoryID = payloadStr(env.Payload, "graph_id")
		}
		sourceRunID := payloadStr(env.Payload, "source_run_id")
		if repositoryID == "" || sourceRunID == "" {
			continue
		}

		contextByRepoID[repositoryID] = ProjectionContext{
			ScopeID:          env.ScopeID,
			AcceptanceUnitID: repositoryID,
			SourceRunID:      sourceRunID,
			GenerationID:     generationID,
		}
	}
	return contextByRepoID
}

func buildCodeCallSharedIntentRows(
	rows []map[string]any,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
	evidenceSource string,
	deltaScopesByRepoID map[string]codeCallDeltaFileScope,
) []SharedProjectionIntentRow {
	intents := make([]SharedProjectionIntentRow, 0, len(rows))
	deltaPartitionsByRepoID := buildCodeCallDeltaPartitionIndexByRepoID(deltaScopesByRepoID)
	for _, row := range rows {
		repositoryID := anyToString(row["repo_id"])
		if repositoryID == "" {
			continue
		}

		context, ok := contextByRepoID[repositoryID]
		if !ok {
			continue
		}

		payload := copyPayload(row)
		payload["evidence_source"] = evidenceSource
		callerID := anyToString(payload["caller_entity_id"])
		if callerID == "" {
			callerID = anyToString(payload["source_entity_id"])
		}
		calleeID := anyToString(payload["callee_entity_id"])
		if calleeID == "" {
			calleeID = anyToString(payload["target_entity_id"])
		}
		partitionKey := callerID + "->" + calleeID
		if partitionKey == "->" {
			partitionKey = repositoryID
		}
		identityKey := ""
		if deltaPartition, ok := codeCallDeltaPartitionForPayload(
			payload,
			deltaPartitionsByRepoID[repositoryID],
		); ok {
			partitionKey = deltaPartition.partitionKey
			identityKey = codeCallDeltaEdgeIdentityKey(
				partitionKey,
				callerID,
				calleeID,
				repositoryID,
				anyToString(payload["relationship_type"]),
			)
			payload["delta_projection"] = true
			payload["delta_file_paths"] = []string{deltaPartition.filePath}
		}

		intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainCodeCalls,
			PartitionKey:     partitionKey,
			IdentityKey:      identityKey,
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.acceptanceUnitID(repositoryID),
			RepositoryID:     repositoryID,
			SourceRunID:      context.SourceRunID,
			GenerationID:     context.GenerationID,
			Payload:          payload,
			CreatedAt:        createdAt,
		}))
	}

	sort.SliceStable(intents, func(i, j int) bool {
		if intents[i].RepositoryID != intents[j].RepositoryID {
			return intents[i].RepositoryID < intents[j].RepositoryID
		}
		return intents[i].IntentID < intents[j].IntentID
	})

	return deduplicateCodeCallIntentRows(intents)
}

func buildCodeCallRefreshIntentsWithDeltaFileScopes(
	contextByRepoID map[string]ProjectionContext,
	deltaScopesByRepoID map[string]codeCallDeltaFileScope,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	if len(contextByRepoID) == 0 {
		return nil
	}

	repositoryIDs := make([]string, 0, len(contextByRepoID))
	for repositoryID := range contextByRepoID {
		repositoryIDs = append(repositoryIDs, repositoryID)
	}
	sort.Strings(repositoryIDs)

	intents := make([]SharedProjectionIntentRow, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		context := contextByRepoID[repositoryID]
		deltaScope := deltaScopesByRepoID[repositoryID]
		partitionKey, fileScoped := codeCallRefreshPartitionKeyForDeltaScope(
			repositoryID,
			deltaScope.partitionPaths,
		)
		var deltaFilePaths []string
		if fileScoped {
			partitions, ok := buildCodeCallDeltaFilePartitions(repositoryID, deltaScope)
			if !ok {
				partitionKey = codeCallRefreshPartitionKey(repositoryID)
			} else {
				deltaFilePaths = make([]string, 0, len(partitions))
				for _, partition := range partitions {
					deltaFilePaths = append(deltaFilePaths, partition.filePath)
				}
			}
		} else {
			partitionKey = codeCallRefreshPartitionKey(repositoryID)
		}
		intents = append(intents, buildCodeCallRefreshIntent(
			repositoryID,
			partitionKey,
			deltaFilePaths,
			context,
			createdAt,
		))
	}

	return intents
}

func buildCodeCallRefreshIntent(
	repositoryID string,
	partitionKey string,
	deltaFilePaths []string,
	context ProjectionContext,
	createdAt time.Time,
) SharedProjectionIntentRow {
	// action="refresh" and intent_type="repo_refresh" MUST stay co-defined: the DB
	// fences rank by the generated column is_refresh_intent (= action='refresh')
	// while the in-memory fence/selection key off intent_type='repo_refresh'.
	// If these diverge the two fences disagree on which row is the repo refresh
	// and can deadlock (the #3865 class).
	payload := map[string]any{
		"repo_id":         repositoryID,
		"action":          "refresh",
		"intent_type":     "repo_refresh",
		"evidence_source": codeCallRepoRefreshEvidenceSource,
	}
	if len(deltaFilePaths) > 0 {
		payload["delta_projection"] = true
		payload["delta_file_paths"] = append([]string(nil), deltaFilePaths...)
	}

	return BuildSharedProjectionIntent(SharedProjectionIntentInput{
		ProjectionDomain: DomainCodeCalls,
		PartitionKey:     partitionKey,
		ScopeID:          context.ScopeID,
		AcceptanceUnitID: context.acceptanceUnitID(repositoryID),
		RepositoryID:     repositoryID,
		SourceRunID:      context.SourceRunID,
		GenerationID:     context.GenerationID,
		Payload:          payload,
		CreatedAt:        createdAt,
	})
}

func buildCodeCallDeltaFilePathsByRepoID(envelopes []facts.Envelope) map[string][]string {
	scopesByRepoID := buildCodeCallDeltaFileScopesByRepoID(envelopes)
	if len(scopesByRepoID) == 0 {
		return nil
	}

	pathsByRepoID := make(map[string][]string, len(scopesByRepoID))
	for repositoryID, scope := range scopesByRepoID {
		pathsByRepoID[repositoryID] = append([]string(nil), scope.filePaths...)
	}
	return pathsByRepoID
}

func buildCodeCallDeltaFileScopesByRepoID(envelopes []facts.Envelope) map[string]codeCallDeltaFileScope {
	seenByRepoID := make(map[string]map[string]struct{})
	repoPathByRepoID := make(map[string]string)
	unsafeByRepoID := make(map[string]struct{})
	for _, env := range envelopes {
		if env.FactKind != factKindRepository || !codeCallPayloadBool(env.Payload, "delta_generation") {
			continue
		}
		repositoryID := semanticPayloadString(env.Payload, "repo_id")
		if repositoryID == "" {
			repositoryID = semanticPayloadString(env.Payload, "graph_id")
		}
		if repositoryID == "" {
			continue
		}
		repoPath := semanticPayloadString(env.Payload, "path")
		if repoPath == "" {
			repoPath = semanticPayloadString(env.Payload, "local_path")
		}
		if strings.TrimSpace(repoPath) == "" {
			unsafeByRepoID[repositoryID] = struct{}{}
			continue
		}
		for _, relativePath := range codeCallDeltaRelativePaths(env.Payload) {
			cleanedRelativePath, ok := normalizeCodeCallDeltaRelativePath(relativePath)
			if !ok {
				unsafeByRepoID[repositoryID] = struct{}{}
				continue
			}
			seen := seenByRepoID[repositoryID]
			if seen == nil {
				seen = make(map[string]struct{})
				seenByRepoID[repositoryID] = seen
			}
			seen[cleanedRelativePath] = struct{}{}
			repoPathByRepoID[repositoryID] = repoPath
		}
	}
	if len(seenByRepoID) == 0 {
		return nil
	}

	scopesByRepoID := make(map[string]codeCallDeltaFileScope, len(seenByRepoID))
	for repositoryID, seen := range seenByRepoID {
		if _, unsafe := unsafeByRepoID[repositoryID]; unsafe {
			continue
		}
		repoPath := repoPathByRepoID[repositoryID]
		if strings.TrimSpace(repoPath) == "" {
			continue
		}

		partitionPaths := make([]string, 0, len(seen))
		for relativePath := range seen {
			partitionPaths = append(partitionPaths, relativePath)
		}
		sort.Strings(partitionPaths)

		filePaths := make([]string, 0, len(partitionPaths))
		for _, relativePath := range partitionPaths {
			filePaths = append(filePaths, path.Join(repoPath, relativePath))
		}
		scopesByRepoID[repositoryID] = codeCallDeltaFileScope{
			filePaths:      filePaths,
			partitionPaths: partitionPaths,
		}
	}
	return scopesByRepoID
}

func codeCallDeltaRelativePaths(payload map[string]any) []string {
	seen := make(map[string]struct{})
	var paths []string
	for _, key := range []string{"delta_relative_paths", "delta_deleted_relative_paths"} {
		for _, relativePath := range semanticPayloadStringSlice(payload, key) {
			if _, ok := seen[relativePath]; ok {
				continue
			}
			seen[relativePath] = struct{}{}
			paths = append(paths, relativePath)
		}
	}
	return paths
}

func normalizeCodeCallDeltaRelativePath(relativePath string) (string, bool) {
	candidate := strings.TrimSpace(relativePath)
	cleaned := path.Clean(candidate)
	if candidate == "" || cleaned == "" || cleaned == "." || cleaned == ".." ||
		path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "../") || cleaned != candidate {
		return "", false
	}
	return cleaned, true
}

func codeCallPayloadBool(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	value, ok := payload[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

func codeCallRefreshPartitionKey(repositoryID string) string {
	return codeCallWholeScopePartitionKey(repositoryID)
}

func codeCallRefreshPartitionKeyForDelta(repositoryID string, filePaths []string) string {
	partitionKey, _ := codeCallRefreshPartitionKeyForDeltaScope(repositoryID, filePaths)
	return partitionKey
}

func codeCallRefreshPartitionKeyForDeltaScope(repositoryID string, filePaths []string) (string, bool) {
	normalizedPaths, ok := normalizeCodeCallPartitionFilePaths(filePaths)
	if !ok {
		return codeCallWholeScopePartitionKey(repositoryID), false
	}

	hash := sha256.New()
	hash.Write([]byte(strings.TrimSpace(repositoryID)))
	hash.Write([]byte{0})
	for _, filePath := range normalizedPaths {
		hash.Write([]byte(filePath))
		hash.Write([]byte{0})
	}
	digest := hash.Sum(nil)
	return codeCallPartitionKeyVersion + ":files:" +
		strings.TrimSpace(repositoryID) + ":" + hex.EncodeToString(digest), true
}

func normalizeCodeCallPartitionFilePaths(filePaths []string) ([]string, bool) {
	if len(filePaths) == 0 {
		return nil, false
	}

	seen := make(map[string]struct{}, len(filePaths))
	normalized := make([]string, 0, len(filePaths))
	for _, filePath := range filePaths {
		cleaned, ok := normalizeCodeCallDeltaRelativePath(filePath)
		if !ok {
			return nil, false
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		normalized = append(normalized, cleaned)
	}
	if len(normalized) == 0 {
		return nil, false
	}
	sort.Strings(normalized)
	return normalized, true
}

func codeCallWholeScopePartitionKey(repositoryID string) string {
	return codeCallPartitionKeyVersion + ":whole:" + strings.TrimSpace(repositoryID)
}

func deduplicateCodeCallIntentRows(intents []SharedProjectionIntentRow) []SharedProjectionIntentRow {
	if len(intents) < 2 {
		return intents
	}

	deduplicated := intents[:1]
	for _, intent := range intents[1:] {
		if intent.IntentID == deduplicated[len(deduplicated)-1].IntentID {
			continue
		}
		deduplicated = append(deduplicated, intent)
	}

	return deduplicated
}
