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
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

const codeCallPartitionKeyVersion = "code-calls:v1"

type codeCallDeltaFileScope struct {
	filePaths      []string
	partitionPaths []string
}

// buildCodeCallProjectionContexts decodes each "repository" fact's outer
// envelope through the codegraph contracts seam (decodeCodegraphRepository)
// to recover its join identity (RepoID) and optional SourceRunID before
// building one ProjectionContext per repository. A repository fact whose
// payload is missing a required identity field is skipped for context
// building — dropped by returning early from decodeCodegraphRepository's
// error, matching this function's pre-existing "skip and continue" shape for
// an absent identity; batch-wide quarantine visibility for this read site is
// provided by extractCodeCallRowsWithIndex's file-fact quarantine, which is
// the accuracy hole issue #4749 targets (repo_id/relative_path used to
// silently collapse to an empty-string graph identity on "file" facts).
// SourceRunID stays optional: not every repository fact carries one, and an
// absent source run id is a legitimate reason to skip context building here,
// not a malformed payload.
func buildCodeCallProjectionContexts(envelopes []facts.Envelope, generationID string) map[string]ProjectionContext {
	contextByRepoID := make(map[string]ProjectionContext)
	for _, env := range envelopes {
		if env.FactKind != "repository" {
			continue
		}

		repository, err := decodeCodegraphRepository(env)
		if err != nil {
			continue
		}

		// TrimSpace preserves the pre-Contract-System payloadStr behavior: a
		// whitespace-only repo_id must not become a non-canonical
		// AcceptanceUnitID/contextByRepoID key. The real collector never emits
		// a whitespace repo id, so this is behavior-equivalence, not new logic.
		repositoryID := strings.TrimSpace(repository.RepoID)
		if repositoryID == "" {
			continue
		}
		var sourceRunID string
		if repository.SourceRunID != nil {
			sourceRunID = strings.TrimSpace(*repository.SourceRunID)
		}
		if sourceRunID == "" {
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

// buildCodeCallDeltaFileScopesByRepoID decodes each delta-generation
// "repository" fact's outer envelope through the codegraph contracts seam
// (decodeCodegraphRepository) to recover its join identity (RepoID) and the
// delta path slices before collecting the delta file scope. The cheap
// delta_generation gate check runs on the raw payload before decode so a
// non-delta repository fact (the overwhelming majority) never pays the
// decode cost. A delta-generation repository fact whose payload is missing a
// required identity field is skipped, matching this function's pre-existing
// "skip and continue" shape for an absent identity.
//
// The repository checkout path is resolved from the raw "path" key first, then
// the typed LocalPath — preserving the exact pre-Contract-System precedence.
// "path" is NOT a typed Repository field: repositoryFactEnvelope never writes
// it to the payload (it routes the checkout path to SourceRef.SourceURI), so in
// production this raw read is always absent and LocalPath is used. It is read
// raw here only to preserve behavior for callers (and tests) that carry the
// checkout path under "path".
func buildCodeCallDeltaFileScopesByRepoID(envelopes []facts.Envelope) map[string]codeCallDeltaFileScope {
	seenByRepoID := make(map[string]map[string]struct{})
	repoPathByRepoID := make(map[string]string)
	unsafeByRepoID := make(map[string]struct{})
	for _, env := range envelopes {
		if env.FactKind != factKindRepository || !codeCallPayloadBool(env.Payload, "delta_generation") {
			continue
		}
		repository, err := decodeCodegraphRepository(env)
		if err != nil {
			continue
		}
		// TrimSpace + skip-empty preserves the pre-Contract-System
		// semanticPayloadString behavior: a whitespace-only repo_id must not
		// create an unsafeByRepoID/seenByRepoID entry under a non-canonical key.
		repositoryID := strings.TrimSpace(repository.RepoID)
		if repositoryID == "" {
			continue
		}

		repoPath := semanticPayloadString(env.Payload, "path")
		if repoPath == "" && repository.LocalPath != nil {
			repoPath = *repository.LocalPath
		}
		if strings.TrimSpace(repoPath) == "" {
			unsafeByRepoID[repositoryID] = struct{}{}
			continue
		}
		for _, relativePath := range codeCallDeltaRelativePathsFromRepository(repository) {
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

// codeCallDeltaRelativePathsFromRepository returns the deduplicated union of
// a decoded codegraphv1.Repository's DeltaRelativePaths and
// DeltaDeletedRelativePaths — the changed and deleted file paths a delta
// generation carries. It is the typed-decode replacement for the pre-Contract-
// System raw payload["delta_relative_paths"]/["delta_deleted_relative_paths"]
// reads.
func codeCallDeltaRelativePathsFromRepository(repository codegraphv1.Repository) []string {
	seen := make(map[string]struct{})
	var paths []string
	for _, relativePath := range repository.DeltaRelativePaths {
		if _, ok := seen[relativePath]; ok {
			continue
		}
		seen[relativePath] = struct{}{}
		paths = append(paths, relativePath)
	}
	for _, relativePath := range repository.DeltaDeletedRelativePaths {
		if _, ok := seen[relativePath]; ok {
			continue
		}
		seen[relativePath] = struct{}{}
		paths = append(paths, relativePath)
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
