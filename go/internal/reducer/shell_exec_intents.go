package reducer

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

const shellExecPartitionKeyVersion = "shell-exec:v1"

func shellExecFilePartitionKey(repoID, sourcePath, edgeIdentity string) string {
	repoID = strings.TrimSpace(repoID)
	hash := sha256.New()
	hash.Write([]byte(repoID))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.TrimSpace(sourcePath)))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.TrimSpace(edgeIdentity)))
	return shellExecPartitionKeyVersion + ":files:" + repoID + ":" + hex.EncodeToString(hash.Sum(nil))
}

func shellExecWholeScopePartitionKey(repoID string) string {
	return repoWideRetractRefreshPartitionKey(DomainShellExec, repoID)
}

func buildShellExecSharedIntentRows(
	edgeRows []map[string]any,
	deltaScope sqlRelationshipDeltaScope,
	repoIDs []string,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	if len(repoIDs) == 0 {
		return nil
	}

	intents := make([]SharedProjectionIntentRow, 0, len(repoIDs)+len(edgeRows))
	intents = append(intents, buildShellExecRefreshIntents(deltaScope, repoIDs, contextByRepoID, createdAt)...)

	for _, row := range edgeRows {
		repoID := anyToString(row["repo_id"])
		context, ok := contextByRepoID[repoID]
		if !ok {
			continue
		}
		sourcePath := anyToString(row["source_path"])
		edgeIdentity := shellExecEdgeIdentityKey(row)
		payload := copyPayload(row)
		payload["action"] = "upsert"
		payload[retractViaRefreshKey] = true

		intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainShellExec,
			PartitionKey:     shellExecFilePartitionKey(repoID, sourcePath, edgeIdentity),
			IdentityKey:      edgeIdentity,
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.acceptanceUnitID(repoID),
			RepositoryID:     repoID,
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
	return intents
}

func buildShellExecRefreshIntents(
	deltaScope sqlRelationshipDeltaScope,
	repoIDs []string,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	sorted := append([]string(nil), repoIDs...)
	sort.Strings(sorted)

	intents := make([]SharedProjectionIntentRow, 0, len(sorted))
	for _, repoID := range sorted {
		context, ok := contextByRepoID[repoID]
		if !ok {
			continue
		}
		payload := map[string]any{
			"repo_id":         repoID,
			"intent_type":     repoRefreshIntentType,
			"action":          repoRefreshAction,
			"evidence_source": shellExecEvidenceSource,
		}
		if deltaScope.hasDelta {
			payload["delta_projection"] = true
			payload["delta_file_paths"] = append([]string(nil), deltaScope.filePathsByRepoID[repoID]...)
		}
		intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainShellExec,
			PartitionKey:     shellExecWholeScopePartitionKey(repoID),
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.acceptanceUnitID(repoID),
			RepositoryID:     repoID,
			SourceRunID:      context.SourceRunID,
			GenerationID:     context.GenerationID,
			Payload:          payload,
			CreatedAt:        createdAt,
		}))
	}
	return intents
}

func shellExecEdgeIdentityKey(row map[string]any) string {
	return anyToString(row["source_entity_id"]) + "->" +
		anyToString(row["target_entity_id"]) + ":" +
		anyToString(row["relationship_type"])
}
