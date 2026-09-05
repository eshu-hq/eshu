// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inheritance

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// PartitionKeyVersion namespaces every inheritance_edges partition
// key so a future key-shape change can run alongside the old one without
// colliding. It mirrors codeCallPartitionKeyVersion (#2867).
const PartitionKeyVersion = "inheritance-edges:v1"

// inheritanceFilePartitionKey returns the file-scoped partition key for a single
// inheritance edge. It is unique per edge, not merely per file: the generic
// ProcessPartitionOnce selection deduplicates rows by (acceptance key, partition
// key) via LatestIntentsByRepoAndPartition, so two edges that shared one partition
// key would collapse and one edge would be silently dropped. The key therefore
// hashes the repo, the child's repo-qualified file path, and the edge identity
// (child->parent:relationship_type). The child_path is still in the hash so a
// re-ingest of the same edge keeps a stable key and so the value reads as
// file-scoped; the edge identity makes it collision-free. Hashing spreads a repo's
// edges across the partition ring so distinct edges project concurrently, and the
// repo is mixed in first so two repos never collide (#2867).
func inheritanceFilePartitionKey(repoID, childPath, edgeIdentity string) string {
	repoID = strings.TrimSpace(repoID)
	hash := sha256.New()
	hash.Write([]byte(repoID))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.TrimSpace(childPath)))
	hash.Write([]byte{0})
	hash.Write([]byte(strings.TrimSpace(edgeIdentity)))
	digest := hash.Sum(nil)
	return PartitionKeyVersion + ":files:" + repoID + ":" + hex.EncodeToString(digest)
}

// WholeScopePartitionKey returns the whole-scope partition key the
// per-repo refresh intent is emitted under. It MUST equal the key the #2898
// refresh fence reconstructs (repoWideRetractRefreshPartitionKey), because the
// fence reads a per-edge row's repo and rebuilds this exact key to check whether
// the owning refresh has committed. Emitting the refresh under any other key
// would make the fence miss it and defer every cross-partition edge forever, so
// this delegates to the shared helper rather than minting an inheritance-only key
// (#2867/#2898). A whole-scope key hashes to exactly one partition, so the repo's
// single retract is owned by one partition lease and cannot race itself.
func WholeScopePartitionKey(repoID string) string {
	return sharedintent.RepoWideRetractRefreshPartitionKey(reducercontract.DomainInheritanceEdges, repoID)
}

// BuildSharedIntentRows promotes extracted inheritance edge rows to
// durable shared-projection intents with file-scoped partition keys, reusing the
// #2898 refresh-fence mechanism (#2867).
//
// For each repository that has a projection context it emits exactly one
// whole-scope refresh intent. That refresh owns the domain's single retract:
// repo-wide on child.repo_id by default, or file-scoped on child.path when the
// generation is a delta (the refresh then carries delta_projection and the
// repo's changed delta_file_paths, matching the edge writer's delta dispatch).
//
// For each edge row it emits a write-only per-edge intent placed under the
// file-scoped partition key for that edge's child_path, marked
// retract_via_refresh so the worker fences the write behind the paired refresh.
// Because many edges in one file share a partition key, the per-edge intent uses
// an IdentityKey (child->parent:relationship_type) so each intent ID stays
// distinct. Rows whose repo has no projection context are skipped: without an
// acceptance identity they cannot be fenced or freshness-gated.
func BuildSharedIntentRows(
	edgeRows []map[string]any,
	deltaScope DeltaScope,
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
		childPath := payloadcore.AnyToString(row["child_path"])
		edgeIdentity := inheritanceEdgeIdentityKey(row)
		payload := payloadcore.CopyPayload(row)
		payload["action"] = "upsert"
		payload[sharedintent.RetractViaRefreshKey] = true

		intents = append(intents, sharedintent.Build(sharedintent.Input{
			ProjectionDomain: reducercontract.DomainInheritanceEdges,
			PartitionKey:     inheritanceFilePartitionKey(repoID, childPath, edgeIdentity),
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

// BuildRefreshIntents emits one whole-scope refresh intent per
// repository that has a projection context. A repository on a DELTA generation
// carries the delta scope so the worker issues the file-scoped retract; one on
// a full generation carries none, so the worker issues the repo-wide retract. Repos are sorted so
// emission is deterministic (#2867/#2898).
func BuildRefreshIntents(
	deltaScope DeltaScope,
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
			"evidence_source": EvidenceSource,
		}
		// Delta scoping is per repository and fails closed on an unusable
		// delta; applyRepoRefreshDeltaScope (shared_payload_delta_compat.go)
		// carries the full rule and why the two obvious alternatives lose
		// edges (#6216).
		sharedintent.ApplyRepoRefreshDeltaScope(payload, repoID, deltaRepositoryIDs, deltaScope.FilePathsByRepoID)
		intents = append(intents, sharedintent.Build(sharedintent.Input{
			ProjectionDomain: reducercontract.DomainInheritanceEdges,
			PartitionKey:     WholeScopePartitionKey(repoID),
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

// inheritanceEdgeIdentityKey is the deterministic per-edge identity used for the
// intent ID when many edges share one file-scoped partition key. It matches the
// edge key the canonical inheritance edge writer uses (child->parent plus the
// relationship type), so two distinct relationship types between the same pair
// stay separate intents (#2867).
func inheritanceEdgeIdentityKey(row map[string]any) string {
	return payloadcore.AnyToString(row["child_entity_id"]) + "->" +
		payloadcore.AnyToString(row["parent_entity_id"]) + ":" +
		payloadcore.AnyToString(row["relationship_type"])
}
