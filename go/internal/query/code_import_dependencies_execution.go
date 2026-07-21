// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func (h *CodeHandler) importDependencyRows(
	ctx context.Context,
	req importDependencyRequest,
) ([]map[string]any, error) {
	switch req.queryType() {
	case "file_import_cycles":
		return h.fileImportCycleRows(ctx, req)
	case "cross_module_calls":
		return h.crossModuleCallRows(ctx, req)
	default:
		return h.importRows(ctx, req)
	}
}

func (h *CodeHandler) importRows(
	ctx context.Context,
	req importDependencyRequest,
) ([]map[string]any, error) {
	sourceScopes, err := h.importDependencyModuleScopes(ctx, req, true)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.SourceModule) != "" && len(sourceScopes) == 0 {
		return []map[string]any{}, nil
	}

	params := importDependencyParams(req)
	if len(sourceScopes) > 0 {
		params["source_paths"] = importDependencyScopePaths(sourceScopes)
		params["scan_limit"] = importDependencyInternalScanLimit + 1
	}

	var cypher string
	switch {
	case req.queryType() == "package_imports":
		cypher = packageImportRowsCypher(req, sourceScopes)
	case len(sourceScopes) > 0:
		cypher = sourceModuleImportRowsCypher(req, sourceScopes)
		params["scan_limit"] = importDependencyInternalScanLimit + 1
	default:
		cypher = directImportRowsCypher(req)
	}

	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, fmt.Errorf("query import dependency rows: %w", err)
	}
	if len(sourceScopes) == 0 {
		return rows, nil
	}
	if err := importDependencyScanBoundError(len(rows)); err != nil {
		return nil, err
	}
	rows = filterImportDependencyScopeRows(rows, "repo_id", "source_path", sourceScopes)
	for _, row := range rows {
		row["source_module"] = strings.TrimSpace(req.SourceModule)
	}
	if req.queryType() == "package_imports" {
		rows = uniquePackageImportRows(rows)
	}
	stripImportDependencyInternalPaths(rows)
	return pageImportDependencyRows(req, rows), nil
}

func (h *CodeHandler) fileImportCycleRows(
	ctx context.Context,
	req importDependencyRequest,
) ([]map[string]any, error) {
	params := importDependencyParams(req)
	params["cycle_language"] = "python"
	params["scan_limit"] = importDependencyInternalScanLimit + 1
	rows, err := h.Neo4j.Run(ctx, fileImportCycleEdgeRowsCypher(req), params)
	if err != nil {
		return nil, fmt.Errorf("query file import cycle edges: %w", err)
	}
	return buildFileImportCycleRows(req, rows)
}

func (h *CodeHandler) crossModuleCallRows(
	ctx context.Context,
	req importDependencyRequest,
) ([]map[string]any, error) {
	sourceScopes, err := h.importDependencyModuleScopes(ctx, req, true)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.SourceModule) != "" && len(sourceScopes) == 0 {
		return []map[string]any{}, nil
	}
	targetScopes, err := h.importDependencyModuleScopes(ctx, req, false)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.TargetModule) != "" && len(targetScopes) == 0 {
		return []map[string]any{}, nil
	}

	params := importDependencyParams(req)
	params["scan_limit"] = importDependencyInternalScanLimit + 1
	if len(sourceScopes) > 0 {
		params["source_paths"] = importDependencyScopePaths(sourceScopes)
	}
	if len(targetScopes) > 0 {
		params["target_paths"] = importDependencyScopePaths(targetScopes)
	}
	rows, err := h.Neo4j.Run(ctx, crossModuleCallRowsCypher(req, sourceScopes, targetScopes), params)
	if err != nil {
		return nil, fmt.Errorf("query cross-module call rows: %w", err)
	}
	return filterCrossModuleCallRows(req, rows, sourceScopes, targetScopes)
}

func (h *CodeHandler) importDependencyModuleScopes(
	ctx context.Context,
	req importDependencyRequest,
	source bool,
) ([]map[string]any, error) {
	module := strings.TrimSpace(req.TargetModule)
	cypher := targetModuleFilesCypher(req)
	pathKey := "target_path"
	if source {
		module = strings.TrimSpace(req.SourceModule)
		cypher = sourceModuleFilesCypher(req)
		pathKey = "source_path"
	}
	if module == "" {
		return nil, nil
	}

	params := importDependencyParams(req)
	params["scan_limit"] = importDependencyInternalScanLimit + 1
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, fmt.Errorf("query module file membership: %w", err)
	}
	if err := importDependencyScanBoundError(len(rows)); err != nil {
		return nil, err
	}
	return uniqueImportDependencyScopes(rows, pathKey), nil
}

func uniqueImportDependencyScopes(rows []map[string]any, pathKey string) []map[string]any {
	seen := make(map[string]struct{}, len(rows))
	scopes := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		repoID := strings.TrimSpace(StringVal(row, "repo_id"))
		path := strings.TrimSpace(StringVal(row, pathKey))
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
		leftRepo, rightRepo := StringVal(scopes[i], "repo_id"), StringVal(scopes[j], "repo_id")
		if leftRepo != rightRepo {
			return leftRepo < rightRepo
		}
		return StringVal(scopes[i], "path") < StringVal(scopes[j], "path")
	})
	return scopes
}

func importDependencyScopePaths(scopes []map[string]any) []string {
	seen := make(map[string]struct{}, len(scopes))
	paths := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		path := StringVal(scope, "path")
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
