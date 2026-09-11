// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"path"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// AcceptanceScanLimit bounds how many pending code-call intents the runner may
// scan or load for one authoritative acceptance unit. The runner must see the
// complete unit before retracting and rewriting repo-wide CALLS edges; this
// guard prevents silent partial graph truth while allowing large real
// repositories to exceed the normal per-cycle batch size. Full-refresh file
// scoping uses the same bound so it never scopes more files than the runner
// can load for the unit.
const AcceptanceScanLimit = 250_000

// FileScopeBuildResult is the outcome of [BuildFileScopesByRepoID]: the
// per-repository file scopes to use for projection, plus counts describing how
// many repositories were scoped or fell back to a whole-repository refresh.
type FileScopeBuildResult struct {
	// ScopesByRepoID maps repository ID to the DeltaFileScope that bounds the
	// files it should be projected against, merging delta-generation scopes
	// with full-refresh scopes (delta wins on conflict). A repository absent
	// from this map has no file scope and must be treated as whole-repository.
	ScopesByRepoID map[string]DeltaFileScope
	// FullRefreshScopedRepos counts repositories that built a full-refresh
	// file scope (no delta scope, but a set of file facts bounded enough to
	// scope from).
	FullRefreshScopedRepos int
	// FullRefreshFallbackRepos counts non-delta repositories with a repository
	// fact whose full-refresh scope could not be built safely (missing or mixed
	// repo path, an unnormalizable relative path, no file facts, or more files
	// than AcceptanceScanLimit), so they fall back to an unscoped refresh. A
	// delta-generation repository whose delta scope is unusable is not counted.
	FullRefreshFallbackRepos int
}

type codeCallFullRefreshFileScopeState struct {
	repoPath string
	paths    map[string]struct{}
	unsafe   bool
}

// BuildFileScopesByRepoID builds the per-repository file scopes used to bound
// code-call projection: it merges delta-generation file scopes with
// full-refresh file scopes derived from "file" facts, preferring the delta
// scope when a repository has both. A repository with no usable scope is left
// out of ScopesByRepoID, meaning its projection is not file-scoped; see
// FullRefreshFallbackRepos for which of those it counts.
func BuildFileScopesByRepoID(envelopes []facts.Envelope) FileScopeBuildResult {
	deltaScopesByRepoID := buildCodeCallDeltaFileScopesByRepoID(envelopes)
	fullScopesByRepoID, fallbackRepos := buildCodeCallFullRefreshFileScopesByRepoID(
		envelopes,
		deltaScopesByRepoID,
	)
	if len(deltaScopesByRepoID) == 0 && len(fullScopesByRepoID) == 0 {
		return FileScopeBuildResult{FullRefreshFallbackRepos: fallbackRepos}
	}

	scopesByRepoID := make(map[string]DeltaFileScope, len(deltaScopesByRepoID)+len(fullScopesByRepoID))
	for repositoryID, scope := range deltaScopesByRepoID {
		scopesByRepoID[repositoryID] = scope
	}
	for repositoryID, scope := range fullScopesByRepoID {
		if _, hasDeltaScope := scopesByRepoID[repositoryID]; hasDeltaScope {
			continue
		}
		scopesByRepoID[repositoryID] = scope
	}

	return FileScopeBuildResult{
		ScopesByRepoID:           scopesByRepoID,
		FullRefreshScopedRepos:   len(fullScopesByRepoID),
		FullRefreshFallbackRepos: fallbackRepos,
	}
}

func buildCodeCallFullRefreshFileScopesByRepoID(
	envelopes []facts.Envelope,
	deltaScopesByRepoID map[string]DeltaFileScope,
) (map[string]DeltaFileScope, int) {
	return buildCodeCallFullRefreshFileScopesByRepoIDWithLimit(
		envelopes,
		deltaScopesByRepoID,
		AcceptanceScanLimit,
	)
}

func buildCodeCallFullRefreshFileScopesByRepoIDWithLimit(
	envelopes []facts.Envelope,
	deltaScopesByRepoID map[string]DeltaFileScope,
	fileLimit int,
) (map[string]DeltaFileScope, int) {
	stateByRepoID, deltaRepoIDs := collectCodeCallFullRefreshScopeRepositories(envelopes)
	if len(stateByRepoID) == 0 {
		return nil, 0
	}

	for repositoryID := range deltaScopesByRepoID {
		deltaRepoIDs[repositoryID] = struct{}{}
	}
	for _, env := range envelopes {
		if env.IsTombstone || env.FactKind != factload.FactKindFile {
			continue
		}
		repositoryID := payloadcore.SemanticPayloadString(env.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}
		if _, isDelta := deltaRepoIDs[repositoryID]; isDelta {
			continue
		}
		state := stateByRepoID[repositoryID]
		if state == nil || state.unsafe {
			continue
		}
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		relativePath := codeCallFullRefreshRelativePath(env.Payload, fileData)
		cleanedRelativePath, ok := normalizeCodeCallDeltaRelativePath(relativePath)
		if !ok {
			state.unsafe = true
			continue
		}
		state.paths[cleanedRelativePath] = struct{}{}
		if fileLimit <= 0 || len(state.paths) > fileLimit {
			state.unsafe = true
		}
	}

	scopesByRepoID := make(map[string]DeltaFileScope, len(stateByRepoID))
	fallbackRepos := 0
	for repositoryID, state := range stateByRepoID {
		if _, isDelta := deltaRepoIDs[repositoryID]; isDelta {
			continue
		}
		scope, ok := buildCodeCallFullRefreshFileScope(state)
		if !ok {
			fallbackRepos++
			continue
		}
		scopesByRepoID[repositoryID] = scope
	}
	if len(scopesByRepoID) == 0 {
		return nil, fallbackRepos
	}
	return scopesByRepoID, fallbackRepos
}

func collectCodeCallFullRefreshScopeRepositories(
	envelopes []facts.Envelope,
) (map[string]*codeCallFullRefreshFileScopeState, map[string]struct{}) {
	stateByRepoID := make(map[string]*codeCallFullRefreshFileScopeState)
	deltaRepoIDs := make(map[string]struct{})
	for _, env := range envelopes {
		if env.IsTombstone || env.FactKind != factload.FactKindRepository {
			continue
		}
		repositoryID := payloadcore.SemanticPayloadString(env.Payload, "repo_id")
		if repositoryID == "" {
			repositoryID = payloadcore.SemanticPayloadString(env.Payload, "graph_id")
		}
		if repositoryID == "" {
			continue
		}
		if PayloadBool(env.Payload, "delta_generation") {
			deltaRepoIDs[repositoryID] = struct{}{}
			continue
		}
		repoPath := payloadcore.SemanticPayloadString(env.Payload, "path")
		if repoPath == "" {
			repoPath = payloadcore.SemanticPayloadString(env.Payload, "local_path")
		}
		cleanedRepoPath := strings.TrimSpace(repoPath)
		state := stateByRepoID[repositoryID]
		if state == nil {
			state = &codeCallFullRefreshFileScopeState{
				paths: make(map[string]struct{}),
			}
			stateByRepoID[repositoryID] = state
		}
		switch {
		case cleanedRepoPath == "":
			state.unsafe = true
		case state.repoPath == "":
			state.repoPath = cleanedRepoPath
		case state.repoPath != cleanedRepoPath:
			state.unsafe = true
		}
	}
	return stateByRepoID, deltaRepoIDs
}

func buildCodeCallFullRefreshFileScope(
	state *codeCallFullRefreshFileScopeState,
) (DeltaFileScope, bool) {
	if state == nil || state.unsafe || strings.TrimSpace(state.repoPath) == "" || len(state.paths) == 0 {
		return DeltaFileScope{}, false
	}

	partitionPaths := make([]string, 0, len(state.paths))
	for relativePath := range state.paths {
		partitionPaths = append(partitionPaths, relativePath)
	}
	sort.Strings(partitionPaths)

	filePaths := make([]string, 0, len(partitionPaths))
	for _, relativePath := range partitionPaths {
		filePaths = append(filePaths, path.Join(state.repoPath, relativePath))
	}
	return DeltaFileScope{
		filePaths:      filePaths,
		partitionPaths: partitionPaths,
	}, true
}

func codeCallFullRefreshRelativePath(payload map[string]any, fileData map[string]any) string {
	if relativePath := payloadcore.SemanticPayloadString(payload, "relative_path"); relativePath != "" {
		return relativePath
	}
	return strings.TrimSpace(payloadcore.AnyToString(fileData["path"]))
}
