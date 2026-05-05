package reducer

import (
	"sort"
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

func buildCodeCallRefreshIntents(
	contextByRepoID map[string]ProjectionContext,
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
