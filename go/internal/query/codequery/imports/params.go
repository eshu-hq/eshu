// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imports

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Params builds the parameter map the import-dependency Cypher builders
// send: paging, repository, language, grant, and module file bindings
// from one request.
func Params(req codemodel.ImportDependencyRequest) map[string]any {
	params := map[string]any{
		"limit":  req.QueryLimit(),
		"offset": req.Offset,
	}
	if repoID := strings.TrimSpace(req.RepoID); repoID != "" {
		params["repo_id"] = repoID
	}
	if language := req.NormalizedLanguage(); language != "" {
		params["language"] = language
	}
	params = req.Access.GraphParams(params)
	if sourceFile := strings.TrimSpace(req.SourceFile); sourceFile != "" {
		params["source_file"] = sourceFile
	}
	if targetFile := strings.TrimSpace(req.TargetFile); targetFile != "" {
		params["target_file"] = targetFile
	}
	if sourceModule := strings.TrimSpace(req.SourceModule); sourceModule != "" {
		params["source_module"] = sourceModule
	}
	if targetModule := strings.TrimSpace(req.TargetModule); targetModule != "" {
		params["target_module"] = targetModule
	}
	return params
}

// UniqueScopes dedupes module file membership rows to distinct
// repository/path scopes in a stable order.
func UniqueScopes(rows []map[string]any, pathKey string) []map[string]any {
	seen := make(map[string]struct{}, len(rows))
	scopes := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		repoID := strings.TrimSpace(querycontract.StringVal(row, "repo_id"))
		path := strings.TrimSpace(querycontract.StringVal(row, pathKey))
		if repoID == "" || path == "" {
			continue
		}
		key := repoID + "\x00" + path
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		scopes = append(scopes, map[string]any{"repo_id": repoID, "path": path})
	}
	sort.Slice(scopes, func(i, j int) bool {
		leftRepo, rightRepo := querycontract.StringVal(scopes[i], "repo_id"), querycontract.StringVal(scopes[j], "repo_id")
		if leftRepo != rightRepo {
			return leftRepo < rightRepo
		}
		return querycontract.StringVal(scopes[i], "path") < querycontract.StringVal(scopes[j], "path")
	})
	return scopes
}

// ScopePaths lists the distinct sorted paths across module scopes.
func ScopePaths(scopes []map[string]any) []string {
	seen := make(map[string]struct{}, len(scopes))
	paths := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		path := querycontract.StringVal(scope, "path")
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
