// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sharedintent

import "strings"

const (
	// RepoRefreshIntentType is the payload intent_type marking a per-repo
	// refresh intent -- the one row that owns a repo-wide-retract domain's
	// single retract (#2898).
	//
	// It is one constant rather than a literal per emitter because the
	// graph-write side reads it back: storage/cypher's rationale retract
	// collects whole-scope repository ids by matching this intent_type, and if
	// the two sides drifted that predicate would match nothing, the whole-scope
	// retract would silently stop running, and stale edges would persist with
	// no error and no dead letter (#5998).
	RepoRefreshIntentType = "repo_refresh"

	// RepoRefreshAction is the payload action a per-repo refresh intent carries.
	RepoRefreshAction = "refresh"

	// RetractViaRefreshKey marks a per-edge row that was emitted WITH a paired
	// repo refresh intent, so the worker may safely fence it behind that
	// refresh. Per-edge rows without the marker predate the #2898 emission (no
	// paired refresh exists for their source run), so the worker keeps them on
	// the legacy per-partition retract path rather than deferring them forever.
	// Such in-flight rows drain normally and are superseded by the next
	// re-ingest's marked rows.
	RetractViaRefreshKey = "retract_via_refresh"
)

// RepoWideRetractRefreshPartitionKey is the whole-scope partition key the
// per-repo refresh intent is emitted under and that the worker reconstructs to
// fence a per-edge row. A whole-scope key hashes to exactly one partition, so a
// repo's single repo-wide retract is owned by one partition lease and cannot
// race itself. Emission and the fence MUST build the key identically, so they
// share this helper.
func RepoWideRetractRefreshPartitionKey(domain, repoID string) string {
	return domain + ":refresh:v1:whole:" + strings.TrimSpace(repoID)
}

// DeltaScopeRepositorySet indexes the repositories a delta scope reports as
// being on a delta generation. The fenced repo-wide-retract domains
// (inheritance, rationale, SQL relationships, shell exec) build it once per
// refresh-intent batch and hand it to [ApplyRepoRefreshDeltaScope] per
// repository, so the gate stays O(1) per repository.
func DeltaScopeRepositorySet(repositoryIDs []string) map[string]struct{} {
	if len(repositoryIDs) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		set[repositoryID] = struct{}{}
	}
	return set
}

// ApplyRepoRefreshDeltaScope stamps the delta retract scope onto one
// repository's repo-wide refresh payload.
//
// The gate is membership in deltaRepositoryIDs -- the repositories whose
// repository fact carried delta_generation this cycle. It is deliberately NOT
// the scope-wide hasDelta flag, and deliberately NOT "this repository has
// qualified paths". All three readings differ, and two of them are wrong
// (#6216):
//
//   - On a delta generation, ALWAYS delta-scoped, even with an empty path list.
//     The collector replaces the discovered file set with the changed targets
//     alone on a delta sync (resolveNativeSnapshotFileSetForTargets,
//     collector/gitrepo/git_snapshot_native.go), so the generation carries
//     content-entity facts for the CHANGED files only and the per-edge intents
//     re-create only those files' edges. Widening such a repository's retract to
//     the whole repository deletes every UNCHANGED file's edge with nothing left
//     to restore it: silent wrong graph, no error, no dead letter. An empty list
//     instead reaches collectDeltaFilePaths
//     (storage/cypher/edge_writer_retract_scope.go), which rejects it before any
//     statement runs, so the partition fails and the intent dead-letters. That
//     is the intended outcome -- a dead letter an operator can see beats a graph
//     that quietly lost edges. A repository reaches this state when its changed
//     paths cannot be qualified: no local_path on the repository fact, or a
//     symlinked repos root whose targets normalizeSnapshotRelativePaths drops.
//
//   - On a FULL generation, never delta-scoped, even when a delta-generation
//     repository shares the scope. hasDelta is scope-wide, so gating on it would
//     scope a full-generation repository's retract to a sibling's changed paths
//     and leave its removed-file edges behind. Its own generation re-emits every
//     file, so the repo-wide retract is the correct scope for it.
func ApplyRepoRefreshDeltaScope(
	payload map[string]any,
	repoID string,
	deltaRepositoryIDs map[string]struct{},
	filePathsByRepoID map[string][]string,
) {
	if _, onDeltaGeneration := deltaRepositoryIDs[repoID]; !onDeltaGeneration {
		return
	}
	filePaths := filePathsByRepoID[repoID]
	payload["delta_projection"] = true
	payload["delta_file_paths"] = append(make([]string, 0, len(filePaths)), filePaths...)
}
