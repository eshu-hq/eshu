// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"path"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type semanticDeltaProjectionScope struct {
	Delta     bool
	FilePaths []string
}

func extractSemanticDeltaProjectionScope(
	envelopes []facts.Envelope,
	rows []SemanticEntityRow,
	targetRepoID string,
) semanticDeltaProjectionScope {
	targetRepoID = strings.TrimSpace(targetRepoID)
	seen := make(map[string]struct{})
	scope := semanticDeltaProjectionScope{}

	addFilePath := func(filePath string) {
		if filePath == "" {
			return
		}
		if _, ok := seen[filePath]; ok {
			return
		}
		seen[filePath] = struct{}{}
		scope.FilePaths = append(scope.FilePaths, filePath)
	}

	for _, env := range envelopes {
		if env.FactKind != factKindRepository {
			continue
		}
		repoID := semanticPayloadString(env.Payload, "repo_id")
		if targetRepoID != "" && repoID != targetRepoID {
			continue
		}
		if !semanticDeltaPayloadBool(env.Payload, "delta_generation") {
			continue
		}
		scope.Delta = true
		repoPath := semanticDeltaPayloadString(env.Payload, "path")
		if repoPath == "" {
			repoPath = semanticDeltaPayloadString(env.Payload, "local_path")
		}
		for _, relativePath := range semanticDeltaPayloadStringSlice(env.Payload, "delta_relative_paths") {
			addFilePath(semanticQualifyDeltaPath(repoPath, relativePath))
		}
		for _, relativePath := range semanticDeltaPayloadStringSlice(env.Payload, "delta_deleted_relative_paths") {
			addFilePath(semanticQualifyDeltaPath(repoPath, relativePath))
		}
	}
	if !scope.Delta {
		return semanticDeltaProjectionScope{}
	}

	for _, row := range rows {
		if targetRepoID != "" && row.RepoID != targetRepoID {
			continue
		}
		addFilePath(row.FilePath)
	}
	return scope
}

func semanticQualifyDeltaPath(repoPath string, relativePath string) string {
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

func semanticDeltaPayloadBool(payload map[string]any, key string) bool {
	if len(payload) == 0 {
		return false
	}
	value, ok := payload[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

func semanticDeltaPayloadString(payload map[string]any, key string) string {
	if len(payload) == 0 {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func semanticDeltaPayloadStringSlice(payload map[string]any, key string) []string {
	if len(payload) == 0 {
		return nil
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if ok && text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

// deltaScopeRepositorySet indexes the repositories a delta scope reports as
// being on a delta generation. The four fenced repo-wide-retract domains
// (inheritance, rationale, SQL relationships, shell exec) build it once per
// refresh-intent batch and hand it to applyRepoRefreshDeltaScope per
// repository, so the gate stays O(1) per repository.
func deltaScopeRepositorySet(repositoryIDs []string) map[string]struct{} {
	if len(repositoryIDs) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		set[repositoryID] = struct{}{}
	}
	return set
}

// applyRepoRefreshDeltaScope stamps the delta retract scope onto one
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
func applyRepoRefreshDeltaScope(
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
