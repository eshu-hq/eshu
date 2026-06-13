package reducer

import (
	"path"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

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
) []SharedProjectionIntentRow {
	intents := make([]SharedProjectionIntentRow, 0, len(rows))
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

		intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainCodeCalls,
			PartitionKey:     partitionKey,
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

func buildCodeCallRefreshIntentsWithDeltaScope(
	contextByRepoID map[string]ProjectionContext,
	deltaFilePathsByRepoID map[string][]string,
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
		payload := map[string]any{
			"repo_id":         repositoryID,
			"action":          "refresh",
			"intent_type":     "repo_refresh",
			"evidence_source": codeCallRepoRefreshEvidenceSource,
		}
		if filePaths := deltaFilePathsByRepoID[repositoryID]; len(filePaths) > 0 {
			payload["delta_projection"] = true
			payload["delta_file_paths"] = append([]string(nil), filePaths...)
		}

		intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainCodeCalls,
			PartitionKey:     codeCallRefreshPartitionKey(repositoryID),
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.acceptanceUnitID(repositoryID),
			RepositoryID:     repositoryID,
			SourceRunID:      context.SourceRunID,
			GenerationID:     context.GenerationID,
			Payload:          payload,
			CreatedAt:        createdAt,
		}))
	}

	return intents
}

func buildCodeCallDeltaFilePathsByRepoID(envelopes []facts.Envelope) map[string][]string {
	seenByRepoID := make(map[string]map[string]struct{})
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
		for _, relativePath := range codeCallDeltaRelativePaths(env.Payload) {
			filePath := qualifyCodeCallDeltaFilePath(repoPath, relativePath)
			if filePath == "" {
				continue
			}
			seen := seenByRepoID[repositoryID]
			if seen == nil {
				seen = make(map[string]struct{})
				seenByRepoID[repositoryID] = seen
			}
			seen[filePath] = struct{}{}
		}
	}
	if len(seenByRepoID) == 0 {
		return nil
	}

	pathsByRepoID := make(map[string][]string, len(seenByRepoID))
	for repositoryID, seen := range seenByRepoID {
		filePaths := make([]string, 0, len(seen))
		for filePath := range seen {
			filePaths = append(filePaths, filePath)
		}
		sort.Strings(filePaths)
		pathsByRepoID[repositoryID] = filePaths
	}
	return pathsByRepoID
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

func qualifyCodeCallDeltaFilePath(repoPath string, relativePath string) string {
	if repoPath == "" || relativePath == "" {
		return ""
	}
	cleaned := path.Clean(relativePath)
	if cleaned == "" || cleaned == "." || cleaned == ".." ||
		path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return path.Join(repoPath, cleaned)
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
	return "repo-refresh:" + repositoryID
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
