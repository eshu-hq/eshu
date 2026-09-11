// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shell

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/reducer/sqlrelationship"
)

const partitionKeyVersion = "shell-exec:v1"

func filePartitionKey(repoID, sourcePath, edgeIdentity string) string {
	repoID = strings.TrimSpace(repoID)
	hash := sha256.New()
	hash.Write([]byte(repoID))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.TrimSpace(sourcePath)))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.TrimSpace(edgeIdentity)))
	return partitionKeyVersion + ":files:" + repoID + ":" + hex.EncodeToString(hash.Sum(nil))
}

func wholeScopePartitionKey(repoID string) string {
	return sharedintent.RepoWideRetractRefreshPartitionKey(reducercontract.DomainShellExec, repoID)
}

// BuildSharedIntentRows builds the durable shared-projection intent rows for
// one shell-exec materialization pass: one per-repo refresh intent per
// repository, plus one per-edge intent per extracted row.
func BuildSharedIntentRows(
	edgeRows []map[string]any,
	deltaScope sqlrelationship.DeltaScope,
	repoIDs []string,
	contextByRepoID map[string]sharedintent.ProjectionContext,
	createdAt time.Time,
) []sharedintent.Row {
	if len(repoIDs) == 0 {
		return nil
	}

	intents := make([]sharedintent.Row, 0, len(repoIDs)+len(edgeRows))
	intents = append(intents, BuildRefreshIntents(deltaScope, repoIDs, contextByRepoID, createdAt)...)

	for _, row := range edgeRows {
		repoID := payloadcore.AnyToString(row["repo_id"])
		context, ok := contextByRepoID[repoID]
		if !ok {
			continue
		}
		sourcePath := payloadcore.AnyToString(row["source_path"])
		edgeIdentity := edgeIdentityKey(row)
		payload := payloadcore.CopyPayload(row)
		payload["action"] = "upsert"
		payload[sharedintent.RetractViaRefreshKey] = true

		intents = append(intents, sharedintent.Build(sharedintent.Input{
			ProjectionDomain: reducercontract.DomainShellExec,
			PartitionKey:     filePartitionKey(repoID, sourcePath, edgeIdentity),
			IdentityKey:      edgeIdentity,
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.ResolveAcceptanceUnitID(repoID),
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

// BuildRefreshIntents builds the per-repo refresh intents that own each
// repository's whole-scope shell-exec retract.
func BuildRefreshIntents(
	deltaScope sqlrelationship.DeltaScope,
	repoIDs []string,
	contextByRepoID map[string]sharedintent.ProjectionContext,
	createdAt time.Time,
) []sharedintent.Row {
	sorted := append([]string(nil), repoIDs...)
	sort.Strings(sorted)

	deltaRepositoryIDs := sharedintent.DeltaScopeRepositorySet(deltaScope.RepositoryIDs)
	intents := make([]sharedintent.Row, 0, len(sorted))
	for _, repoID := range sorted {
		context, ok := contextByRepoID[repoID]
		if !ok {
			continue
		}
		payload := map[string]any{
			"repo_id":         repoID,
			"intent_type":     sharedintent.RepoRefreshIntentType,
			"action":          sharedintent.RepoRefreshAction,
			"evidence_source": evidenceSource,
		}
		// Delta scoping is per repository and fails closed on an unusable
		// delta; sharedintent.ApplyRepoRefreshDeltaScope (the reducer root's
		// compat_decode.go forwards to the same function) carries the full
		// rule and why the two obvious alternatives lose edges (#6216).
		sharedintent.ApplyRepoRefreshDeltaScope(payload, repoID, deltaRepositoryIDs, deltaScope.FilePathsByRepoID)
		intents = append(intents, sharedintent.Build(sharedintent.Input{
			ProjectionDomain: reducercontract.DomainShellExec,
			PartitionKey:     wholeScopePartitionKey(repoID),
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.ResolveAcceptanceUnitID(repoID),
			RepositoryID:     repoID,
			SourceRunID:      context.SourceRunID,
			GenerationID:     context.GenerationID,
			Payload:          payload,
			CreatedAt:        createdAt,
		}))
	}
	return intents
}

func edgeIdentityKey(row map[string]any) string {
	return payloadcore.AnyToString(row["source_entity_id"]) + "->" +
		payloadcore.AnyToString(row["target_entity_id"]) + ":" +
		payloadcore.AnyToString(row["relationship_type"])
}
