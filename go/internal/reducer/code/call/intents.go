// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// PartitionKeyVersion prefixes the code-call refresh partition keys
// (WholeScopePartitionKey and the file-scoped refresh keys); an unscoped
// per-edge intent is keyed by its caller->callee pair instead. Bump it when the
// refresh key derivation changes so old and new keys never collide.
const PartitionKeyVersion = "code-calls:v1"

// RepoRefreshEvidenceSource is the evidence_source stamped on every per-repo
// code-call refresh intent (action "refresh", intent_type "repo_refresh"). The
// refresh intent owns the repo-wide CALLS retract that the per-edge code-call
// intents are fenced behind. The fences key on the action and intent_type
// fields, not on this string; root runner tests stamp it into fixtures.
const RepoRefreshEvidenceSource = "reducer/code-call-refresh"

// EvidenceSource is the evidence_source stamped on per-edge code-call intents
// built from parser call rows. The root handler passes it to
// [BuildSharedIntentRows], and the root projection runner reads it back from
// persisted payloads to group the rows it writes (its retract always covers
// this source), so changing it is a data migration.
const EvidenceSource = "parser/code-calls"

// PythonMetaclassEvidenceSource is the evidence_source stamped on per-edge
// intents built from Python metaclass rows (see
// [github.com/eshu-hq/eshu/go/internal/reducer/code/call/python.ExtractMetaclassRows]).
// It is persisted like [EvidenceSource] and read back by the root runner.
const PythonMetaclassEvidenceSource = "parser/python-metaclass"

// DeltaFileScope bounds a code-call refresh or per-edge intent to a specific
// set of repository-relative files, either from a delta generation or a
// full-refresh scope build. Its fields are unexported, so it is opaque to
// other packages: callers thread it through the exported builders rather than
// constructing or inspecting one directly.
type DeltaFileScope struct {
	filePaths      []string
	partitionPaths []string
}

// BuildSharedIntentRows promotes extracted code-call/metaclass edge rows to
// sharedintent.Row values ready for the projection worker. It stamps
// evidenceSource onto every row, derives a caller/callee partition key (or a
// file-scoped delta partition key when deltaScopesByRepoID covers the edge's
// repository), sorts the result by repository then intent ID, and
// deduplicates rows that land on the same intent ID. A row whose repository
// has no matching entry in contextByRepoID is dropped.
func BuildSharedIntentRows(
	rows []map[string]any,
	contextByRepoID map[string]sharedintent.ProjectionContext,
	createdAt time.Time,
	evidenceSource string,
	deltaScopesByRepoID map[string]DeltaFileScope,
) []sharedintent.Row {
	intents := make([]sharedintent.Row, 0, len(rows))
	deltaPartitionsByRepoID := buildCodeCallDeltaPartitionIndexByRepoID(deltaScopesByRepoID)
	for _, row := range rows {
		repositoryID := payloadcore.AnyToString(row["repo_id"])
		if repositoryID == "" {
			continue
		}

		context, ok := contextByRepoID[repositoryID]
		if !ok {
			continue
		}

		payload := payloadcore.CopyPayload(row)
		payload["evidence_source"] = evidenceSource
		callerID := payloadcore.AnyToString(payload["caller_entity_id"])
		if callerID == "" {
			callerID = payloadcore.AnyToString(payload["source_entity_id"])
		}
		calleeID := payloadcore.AnyToString(payload["callee_entity_id"])
		if calleeID == "" {
			calleeID = payloadcore.AnyToString(payload["target_entity_id"])
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
				payloadcore.AnyToString(payload["relationship_type"]),
			)
			payload["delta_projection"] = true
			payload["delta_file_paths"] = []string{deltaPartition.filePath}
		}

		intents = append(intents, sharedintent.Build(sharedintent.Input{
			ProjectionDomain: reducercontract.DomainCodeCalls,
			PartitionKey:     partitionKey,
			IdentityKey:      identityKey,
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.ResolveAcceptanceUnitID(repositoryID),
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

// BuildRefreshIntentsWithDeltaFileScopes builds one repo-wide refresh intent
// per repository in contextByRepoID, using a file-scoped partition key when
// deltaScopesByRepoID has a usable scope for that repository and falling back
// to the whole-scope partition key (RefreshPartitionKey) otherwise. It returns
// nil when contextByRepoID is empty. Each repository's refresh intent owns the
// repo-wide CALLS retract that the corresponding per-edge intents from
// BuildSharedIntentRows are fenced behind.
//
// The whole-scope fallback is correct only for a full-generation repository.
// A delta-generation repository with no usable scope must never reach this
// builder: callers check [FileScopeBuildResult].UnusableDeltaScopes first and
// fail closed (#10023), because a whole-scope retract with a delta's rows
// deletes every unchanged file's CALLS edge.
func BuildRefreshIntentsWithDeltaFileScopes(
	contextByRepoID map[string]sharedintent.ProjectionContext,
	deltaScopesByRepoID map[string]DeltaFileScope,
	createdAt time.Time,
) []sharedintent.Row {
	if len(contextByRepoID) == 0 {
		return nil
	}

	repositoryIDs := make([]string, 0, len(contextByRepoID))
	for repositoryID := range contextByRepoID {
		repositoryIDs = append(repositoryIDs, repositoryID)
	}
	sort.Strings(repositoryIDs)

	intents := make([]sharedintent.Row, 0, len(repositoryIDs))
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
				partitionKey = RefreshPartitionKey(repositoryID)
			} else {
				deltaFilePaths = make([]string, 0, len(partitions))
				for _, partition := range partitions {
					deltaFilePaths = append(deltaFilePaths, partition.filePath)
				}
			}
		} else {
			partitionKey = RefreshPartitionKey(repositoryID)
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
	context sharedintent.ProjectionContext,
	createdAt time.Time,
) sharedintent.Row {
	// action="refresh" and intent_type="repo_refresh" MUST stay co-defined: the DB
	// fences rank by the generated column is_refresh_intent (= action='refresh')
	// while the in-memory fence/selection key off intent_type='repo_refresh'.
	// If these diverge the two fences disagree on which row is the repo refresh
	// and can deadlock (the #3865 class).
	payload := map[string]any{
		"repo_id":         repositoryID,
		"action":          "refresh",
		"intent_type":     "repo_refresh",
		"evidence_source": RepoRefreshEvidenceSource,
	}
	if len(deltaFilePaths) > 0 {
		payload["delta_projection"] = true
		payload["delta_file_paths"] = append([]string(nil), deltaFilePaths...)
	}

	return sharedintent.Build(sharedintent.Input{
		ProjectionDomain: reducercontract.DomainCodeCalls,
		PartitionKey:     partitionKey,
		ScopeID:          context.ScopeID,
		AcceptanceUnitID: context.ResolveAcceptanceUnitID(repositoryID),
		RepositoryID:     repositoryID,
		SourceRunID:      context.SourceRunID,
		GenerationID:     context.GenerationID,
		Payload:          payload,
		CreatedAt:        createdAt,
	})
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

// PayloadBool reports whether payload[key] is a real bool value set to true.
// A missing key, a nil payload, or a non-bool value (including the strings
// "true"/"false") returns false.
func PayloadBool(payload map[string]any, key string) bool {
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

// RefreshPartitionKey returns the whole-scope refresh partition key for
// repositoryID. It is the fallback partition key used whenever a repository's
// refresh cannot be safely scoped to a set of files.
func RefreshPartitionKey(repositoryID string) string {
	return WholeScopePartitionKey(repositoryID)
}

// RefreshPartitionKeyForDelta returns the file-scoped refresh partition key
// derived by hashing repositoryID and the normalized, deduplicated filePaths.
// When filePaths is empty or contains a path that cannot be normalized to a
// safe relative form, it falls back to RefreshPartitionKey(repositoryID)
// instead of a file-scoped key.
func RefreshPartitionKeyForDelta(repositoryID string, filePaths []string) string {
	partitionKey, _ := codeCallRefreshPartitionKeyForDeltaScope(repositoryID, filePaths)
	return partitionKey
}

func codeCallRefreshPartitionKeyForDeltaScope(repositoryID string, filePaths []string) (string, bool) {
	normalizedPaths, ok := normalizeCodeCallPartitionFilePaths(filePaths)
	if !ok {
		return WholeScopePartitionKey(repositoryID), false
	}

	hash := sha256.New()
	hash.Write([]byte(strings.TrimSpace(repositoryID)))
	hash.Write([]byte{0})
	for _, filePath := range normalizedPaths {
		hash.Write([]byte(filePath))
		hash.Write([]byte{0})
	}
	digest := hash.Sum(nil)
	return PartitionKeyVersion + ":files:" +
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

// WholeScopePartitionKey returns the whole-scope (unfiltered) refresh
// partition key for repositoryID: PartitionKeyVersion plus a "whole" tag and
// the trimmed repository ID. It is used whenever the refresh cannot be scoped
// to a specific set of files.
func WholeScopePartitionKey(repositoryID string) string {
	return PartitionKeyVersion + ":whole:" + strings.TrimSpace(repositoryID)
}

func deduplicateCodeCallIntentRows(intents []sharedintent.Row) []sharedintent.Row {
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
