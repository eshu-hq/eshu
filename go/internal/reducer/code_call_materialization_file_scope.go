// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"path"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type codeCallFileScopeBuildResult struct {
	scopesByRepoID           map[string]codeCallDeltaFileScope
	fullRefreshScopedRepos   int
	fullRefreshFallbackRepos int
}

type codeCallFullRefreshFileScopeState struct {
	repoPath string
	paths    map[string]struct{}
	unsafe   bool
}

func buildCodeCallFileScopesByRepoID(envelopes []facts.Envelope) codeCallFileScopeBuildResult {
	deltaScopesByRepoID := buildCodeCallDeltaFileScopesByRepoID(envelopes)
	fullScopesByRepoID, fallbackRepos := buildCodeCallFullRefreshFileScopesByRepoID(
		envelopes,
		deltaScopesByRepoID,
	)
	if len(deltaScopesByRepoID) == 0 && len(fullScopesByRepoID) == 0 {
		return codeCallFileScopeBuildResult{fullRefreshFallbackRepos: fallbackRepos}
	}

	scopesByRepoID := make(map[string]codeCallDeltaFileScope, len(deltaScopesByRepoID)+len(fullScopesByRepoID))
	for repositoryID, scope := range deltaScopesByRepoID {
		scopesByRepoID[repositoryID] = scope
	}
	for repositoryID, scope := range fullScopesByRepoID {
		if _, hasDeltaScope := scopesByRepoID[repositoryID]; hasDeltaScope {
			continue
		}
		scopesByRepoID[repositoryID] = scope
	}

	return codeCallFileScopeBuildResult{
		scopesByRepoID:           scopesByRepoID,
		fullRefreshScopedRepos:   len(fullScopesByRepoID),
		fullRefreshFallbackRepos: fallbackRepos,
	}
}

func buildCodeCallFullRefreshFileScopesByRepoID(
	envelopes []facts.Envelope,
	deltaScopesByRepoID map[string]codeCallDeltaFileScope,
) (map[string]codeCallDeltaFileScope, int) {
	return buildCodeCallFullRefreshFileScopesByRepoIDWithLimit(
		envelopes,
		deltaScopesByRepoID,
		DefaultCodeCallAcceptanceScanLimit,
	)
}

func buildCodeCallFullRefreshFileScopesByRepoIDWithLimit(
	envelopes []facts.Envelope,
	deltaScopesByRepoID map[string]codeCallDeltaFileScope,
	fileLimit int,
) (map[string]codeCallDeltaFileScope, int) {
	stateByRepoID, deltaRepoIDs := collectCodeCallFullRefreshScopeRepositories(envelopes)
	if len(stateByRepoID) == 0 {
		return nil, 0
	}

	for repositoryID := range deltaScopesByRepoID {
		deltaRepoIDs[repositoryID] = struct{}{}
	}
	for _, env := range envelopes {
		if env.IsTombstone || env.FactKind != factKindFile {
			continue
		}
		repositoryID := semanticPayloadString(env.Payload, "repo_id")
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

	scopesByRepoID := make(map[string]codeCallDeltaFileScope, len(stateByRepoID))
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
		if env.IsTombstone || env.FactKind != factKindRepository {
			continue
		}
		repositoryID := semanticPayloadString(env.Payload, "repo_id")
		if repositoryID == "" {
			repositoryID = semanticPayloadString(env.Payload, "graph_id")
		}
		if repositoryID == "" {
			continue
		}
		if codeCallPayloadBool(env.Payload, "delta_generation") {
			deltaRepoIDs[repositoryID] = struct{}{}
			continue
		}
		repoPath := semanticPayloadString(env.Payload, "path")
		if repoPath == "" {
			repoPath = semanticPayloadString(env.Payload, "local_path")
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
) (codeCallDeltaFileScope, bool) {
	if state == nil || state.unsafe || strings.TrimSpace(state.repoPath) == "" || len(state.paths) == 0 {
		return codeCallDeltaFileScope{}, false
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
	return codeCallDeltaFileScope{
		filePaths:      filePaths,
		partitionPaths: partitionPaths,
	}, true
}

func codeCallFullRefreshRelativePath(payload map[string]any, fileData map[string]any) string {
	if relativePath := semanticPayloadString(payload, "relative_path"); relativePath != "" {
		return relativePath
	}
	return strings.TrimSpace(anyToString(fileData["path"]))
}
