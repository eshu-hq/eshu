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

// rationalePartitionKeyVersion namespaces every rationale_edges partition key so
// a future key-shape change can run alongside the old one without colliding. It
// mirrors inheritance.PartitionKeyVersion and sqlrelationship.PartitionKeyVersion
// (#2869).
const rationalePartitionKeyVersion = "rationale-edges:v1"

// rationaleFilePartitionKey returns the file-scoped partition key for a single
// rationale EXPLAINS edge. It is unique per edge, not merely per file: the generic
// ProcessPartitionOnce selection deduplicates rows by (acceptance key, partition
// key) via LatestIntentsByRepoAndPartition, so two edges that shared one partition
// key would collapse and one edge would be silently dropped. The key therefore
// hashes the repo, the target entity's repo-relative file path, and the edge
// identity (rationale_uid->target_entity_id). The target_path is the entity's
// repo-relative partition anchor, so it is hashed to keep the value visibly
// file-scoped while the edge identity makes it collision-free. Delta retraction
// does not reuse this value: it matches repository-qualified delta_file_paths
// against canonical target.path values.
// Hashing spreads a repo's edges across the partition ring so distinct edges
// project concurrently, and the repo is mixed in first so two repos never collide
// (#2869).
func rationaleFilePartitionKey(repoID, targetPath, edgeIdentity string) string {
	repoID = strings.TrimSpace(repoID)
	hash := sha256.New()
	hash.Write([]byte(repoID))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.TrimSpace(targetPath)))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.TrimSpace(edgeIdentity)))
	digest := hash.Sum(nil)
	return rationalePartitionKeyVersion + ":files:" + repoID + ":" + hex.EncodeToString(digest)
}

// rationaleWholeScopePartitionKey returns the whole-scope partition key the
// per-repo refresh intent is emitted under. It MUST equal the key the #2898
// refresh fence reconstructs (repoWideRetractRefreshPartitionKey), because the
// fence reads a per-edge row's repo and rebuilds this exact key to check whether
// the owning refresh has committed. Emitting the refresh under any other key would
// make the fence miss it and defer every cross-partition edge forever, so this
// delegates to the shared helper rather than minting a rationale-only key
// (#2869/#2898). A whole-scope key hashes to exactly one partition, so the repo's
// single retract is owned by one partition lease and cannot race itself.
func rationaleWholeScopePartitionKey(repoID string) string {
	return repoWideRetractRefreshPartitionKey(DomainRationaleEdges, repoID)
}

// buildRationaleSharedIntentRows promotes extracted rationale EXPLAINS edge rows
// to durable shared-projection intents with file-scoped partition keys, reusing
// the #2898 refresh-fence mechanism (#2869).
//
// For each repository that has a projection context it emits exactly one
// whole-scope refresh intent. That refresh owns the domain's single retract:
// repo-wide on rationale.repo_id by default, or file-scoped on target.path when
// the generation is a delta (the refresh then carries delta_projection and the
// repo's changed delta_file_paths, matching the edge writer's delta dispatch).
//
// For each edge row it emits a write-only per-edge intent placed under the
// file-scoped partition key for that edge's target_path, marked
// retract_via_refresh so the worker fences the write behind the paired refresh.
// The partition key already mixes the edge identity, so same-file edges occupy
// distinct durable partitions. IdentityKey repeats that edge identity for
// deterministic intent-ID construction rather than relying on the encoded
// partition-key string. Rows whose repo has no projection context are skipped:
// without an acceptance identity they cannot be fenced or freshness-gated.
func buildRationaleSharedIntentRows(
	edgeRows []map[string]any,
	deltaScope rationaleDeltaScope,
	repoIDs []string,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	if len(repoIDs) == 0 {
		return nil
	}

	intents := make([]SharedProjectionIntentRow, 0, len(repoIDs)+len(edgeRows))
	intents = append(intents, buildRationaleRefreshIntents(deltaScope, repoIDs, contextByRepoID, createdAt)...)

	for _, row := range edgeRows {
		repoID := anyToString(row["repo_id"])
		context, ok := contextByRepoID[repoID]
		if !ok {
			continue
		}
		targetPath := anyToString(row["target_path"])
		edgeIdentity := rationaleEdgeIdentityKey(row)
		payload := copyPayload(row)
		payload["action"] = "upsert"
		payload[retractViaRefreshKey] = true

		intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainRationaleEdges,
			PartitionKey:     rationaleFilePartitionKey(repoID, targetPath, edgeIdentity),
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

// buildRationaleRefreshIntents emits one whole-scope refresh intent per repository
// that has a projection context. A repository on a DELTA generation carries the
// delta scope so the worker issues the file-scoped retract; one on a full
// generation carries none, so the worker issues the repo-wide retract. Repos are
// sorted so emission is deterministic (#2869/#2898).
func buildRationaleRefreshIntents(
	deltaScope rationaleDeltaScope,
	repoIDs []string,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	sorted := append([]string(nil), repoIDs...)
	sort.Strings(sorted)

	deltaRepositoryIDs := deltaScopeRepositorySet(deltaScope.repositoryIDs)
	intents := make([]SharedProjectionIntentRow, 0, len(sorted))
	for _, repoID := range sorted {
		context, ok := contextByRepoID[repoID]
		if !ok {
			continue
		}
		payload := map[string]any{
			"repo_id":         repoID,
			"intent_type":     RepoRefreshIntentType,
			"action":          repoRefreshAction,
			"evidence_source": rationaleEvidenceSource,
		}
		// Delta scoping is per repository and fails closed on an unusable
		// delta; applyRepoRefreshDeltaScope (shared_payload_delta_compat.go)
		// carries the full rule and why the two obvious alternatives lose
		// edges (#6216).
		applyRepoRefreshDeltaScope(payload, repoID, deltaRepositoryIDs, deltaScope.filePathsByRepoID)
		intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainRationaleEdges,
			PartitionKey:     rationaleWholeScopePartitionKey(repoID),
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

// rationaleEdgeIdentityKey is the deterministic per-edge identity mixed into
// both the file-scoped partition key and the intent ID. It matches the edge key
// the canonical rationale edge writer uses: the identity-only Rationale node's
// uid to the target code entity it EXPLAINS. Two distinct rationale comments
// (different kind or excerpt) on the same entity carry different rationale_uids,
// so they stay separate partitions and intents (#2869).
func rationaleEdgeIdentityKey(row map[string]any) string {
	return anyToString(row["rationale_uid"]) + "->" +
		anyToString(row["target_entity_id"])
}
