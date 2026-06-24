// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// sqlRelationshipPartitionKeyVersion namespaces every sql_relationships partition
// key so a future key-shape change can run alongside the old one without
// colliding. It mirrors inheritancePartitionKeyVersion (#2868).
const sqlRelationshipPartitionKeyVersion = "sql-relationships:v1"

// sqlRelationshipFilePartitionKey returns the file-scoped partition key for a
// single SQL relationship edge. It is unique per edge, not merely per file: the
// generic ProcessPartitionOnce selection deduplicates rows by (acceptance key,
// partition key) via LatestIntentsByRepoAndPartition, so two edges that shared
// one partition key would collapse and one edge would be silently dropped. The
// key therefore hashes the repo, the edge's source-file path, and the edge
// identity (source->target:relationship_type). The source_path is the durable
// anchor (the per-label delta retract keys on source.path), so it is hashed so
// the value reads as file-scoped while the edge identity makes it collision-free.
// Hashing spreads a repo's edges across the partition ring so distinct edges
// project concurrently, and the repo is mixed in first so two repos never
// collide (#2868).
func sqlRelationshipFilePartitionKey(repoID, sourcePath, edgeIdentity string) string {
	repoID = strings.TrimSpace(repoID)
	hash := sha256.New()
	hash.Write([]byte(repoID))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.TrimSpace(sourcePath)))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.TrimSpace(edgeIdentity)))
	digest := hash.Sum(nil)
	return sqlRelationshipPartitionKeyVersion + ":files:" + repoID + ":" + hex.EncodeToString(digest)
}

// sqlRelationshipWholeScopePartitionKey returns the whole-scope partition key the
// per-repo refresh intent is emitted under. It MUST equal the key the #2898
// refresh fence reconstructs (repoWideRetractRefreshPartitionKey), because the
// fence reads a per-edge row's repo and rebuilds this exact key to check whether
// the owning refresh has committed. Emitting the refresh under any other key
// would make the fence miss it and defer every cross-partition edge forever, so
// this delegates to the shared helper rather than minting an SQL-only key
// (#2868/#2898). A whole-scope key hashes to exactly one partition, so the repo's
// single retract is owned by one partition lease and cannot race itself.
func sqlRelationshipWholeScopePartitionKey(repoID string) string {
	return repoWideRetractRefreshPartitionKey(DomainSQLRelationships, repoID)
}

// buildSQLRelationshipSharedIntentRows promotes extracted SQL relationship edge
// rows to durable shared-projection intents with file-scoped partition keys,
// reusing the #2898 refresh-fence mechanism (#2868).
//
// For each repository that has a projection context it emits exactly one
// whole-scope refresh intent. That refresh owns the domain's single retract:
// repo-wide on source.repo_id by default, or file-scoped on source.path when the
// generation is a delta (the refresh then carries delta_projection and the
// repo's changed delta_file_paths, matching the edge writer's delta dispatch).
//
// For each edge row it emits a write-only per-edge intent placed under the
// file-scoped partition key for that edge's source_path, marked
// retract_via_refresh so the worker fences the write behind the paired refresh.
// Because many edges in one file share a partition key, the per-edge intent uses
// an IdentityKey (source->target:relationship_type) so each intent ID stays
// distinct and same-file edges (including the EXECUTES trigger->routine edge) do
// not collapse. Rows whose repo has no projection context are skipped: without an
// acceptance identity they cannot be fenced or freshness-gated.
func buildSQLRelationshipSharedIntentRows(
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
	intents = append(intents, buildSQLRelationshipRefreshIntents(deltaScope, repoIDs, contextByRepoID, createdAt)...)

	for _, row := range edgeRows {
		repoID := anyToString(row["repo_id"])
		context, ok := contextByRepoID[repoID]
		if !ok {
			continue
		}
		sourcePath := anyToString(row["source_path"])
		edgeIdentity := sqlRelationshipEdgeIdentityKey(row)
		payload := copyPayload(row)
		payload["action"] = "upsert"
		payload[retractViaRefreshKey] = true

		intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainSQLRelationships,
			PartitionKey:     sqlRelationshipFilePartitionKey(repoID, sourcePath, edgeIdentity),
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

// buildSQLRelationshipRefreshIntents emits one whole-scope refresh intent per
// repository that has a projection context. The refresh carries the delta scope
// (when present) so the worker issues the file-scoped retract for exactly the
// changed files; otherwise it issues the repo-wide retract. Repos are sorted so
// emission is deterministic (#2868/#2898).
func buildSQLRelationshipRefreshIntents(
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
			"evidence_source": sqlRelationshipEvidenceSource,
		}
		if deltaScope.hasDelta {
			payload["delta_projection"] = true
			payload["delta_file_paths"] = append([]string(nil), deltaScope.filePathsByRepoID[repoID]...)
		}
		intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainSQLRelationships,
			PartitionKey:     sqlRelationshipWholeScopePartitionKey(repoID),
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

// sqlRelationshipEdgeIdentityKey is the deterministic per-edge identity used for
// the intent ID when many edges share one file-scoped partition key. It matches
// the edge key the canonical SQL edge writer uses (source->target plus the
// relationship type), so two distinct relationship types between the same pair
// (for example a SqlTrigger that both TRIGGERS a table and EXECUTES a routine)
// stay separate intents (#2868).
func sqlRelationshipEdgeIdentityKey(row map[string]any) string {
	return anyToString(row["source_entity_id"]) + "->" +
		anyToString(row["target_entity_id"]) + ":" +
		anyToString(row["relationship_type"])
}
