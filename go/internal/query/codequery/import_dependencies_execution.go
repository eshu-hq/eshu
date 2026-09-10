// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// importDependencyRequest is the pre-move spelling of
// codemodel.ImportDependencyRequest, kept for the digest-pinned row builders
// below.
type importDependencyRequest = codemodel.ImportDependencyRequest

const (
	// importDependencyInternalScanLimit is the pre-move spelling of
	// querycontract.ImportDependencyInternalScanLimit, kept for the same
	// digest-pinned bodies.
	importDependencyInternalScanLimit = querycontract.ImportDependencyInternalScanLimit
)

// The crossModuleCallRowsCypher, filterCrossModuleCallRows,
// fileImportCycleEdgeRowsCypher, buildFileImportCycleRows,
// targetModuleFilesCypher, sourceModuleFilesCypher, packageImportRowsCypher,
// sourceModuleImportRowsCypher, directImportRowsCypher,
// importDependencyScanBoundError, and filterImportDependencyScopeRows
// forwarders below preserve the pre-move bare names the digest-pinned row
// builders call (Go has no function aliases).
func crossModuleCallRowsCypher(req importDependencyRequest, sourceScopes, targetScopes []map[string]any) string {
	return codemodel.CrossModuleCallRowsCypher(req, sourceScopes, targetScopes)
}

func filterCrossModuleCallRows(req importDependencyRequest, rows, sourceScopes, targetScopes []map[string]any) ([]map[string]any, error) {
	return codemodel.FilterCrossModuleCallRows(req, rows, sourceScopes, targetScopes)
}

func fileImportCycleEdgeRowsCypher(req importDependencyRequest) string {
	return codemodel.FileImportCycleEdgeRowsCypher(req)
}

func buildFileImportCycleRows(req importDependencyRequest, edgeRows []map[string]any) ([]map[string]any, error) {
	return codemodel.BuildFileImportCycleRows(req, edgeRows)
}

func targetModuleFilesCypher(req importDependencyRequest) string {
	return codemodel.TargetModuleFilesCypher(req)
}

func sourceModuleFilesCypher(req importDependencyRequest) string {
	return codemodel.SourceModuleFilesCypher(req)
}

func packageImportRowsCypher(req importDependencyRequest, sourceScopes []map[string]any) string {
	return codemodel.PackageImportRowsCypher(req, sourceScopes)
}

func sourceModuleImportRowsCypher(req importDependencyRequest, sourceScopes []map[string]any) string {
	return codemodel.SourceModuleImportRowsCypher(req, sourceScopes)
}

func directImportRowsCypher(req importDependencyRequest) string {
	return codemodel.DirectImportRowsCypher(req)
}

func importDependencyScanBoundError(rowCount int) error {
	return codemodel.ImportDependencyScanBoundError(rowCount)
}

func filterImportDependencyScopeRows(rows []map[string]any, repoKey, pathKey string, scopes []map[string]any) []map[string]any {
	return codemodel.FilterImportDependencyScopeRows(rows, repoKey, pathKey, scopes)
}

func uniquePackageImportRows(rows []map[string]any) []map[string]any {
	return codemodel.UniquePackageImportRows(rows)
}

func stripImportDependencyInternalPaths(rows []map[string]any) {
	codemodel.StripImportDependencyInternalPaths(rows)
}

func pageImportDependencyRows(req importDependencyRequest, rows []map[string]any) []map[string]any {
	return codemodel.PageImportDependencyRows(req, rows)
}

func (h *CodeHandler) importDependencyRows(
	ctx context.Context,
	req codemodel.ImportDependencyRequest,
) ([]map[string]any, error) {
	switch req.EffectiveQueryType() {
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

	params := ImportDependencyParams(req)
	if len(sourceScopes) > 0 {
		params["source_paths"] = importDependencyScopePaths(sourceScopes)
		params["scan_limit"] = importDependencyInternalScanLimit + 1
	}

	var cypher string
	switch {
	case req.EffectiveQueryType() == "package_imports":
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
	if req.EffectiveQueryType() == "package_imports" {
		rows = uniquePackageImportRows(rows)
	}
	stripImportDependencyInternalPaths(rows)
	return pageImportDependencyRows(req, rows), nil
}

func (h *CodeHandler) fileImportCycleRows(
	ctx context.Context,
	req importDependencyRequest,
) ([]map[string]any, error) {
	params := ImportDependencyParams(req)
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

	params := ImportDependencyParams(req)
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

	params := ImportDependencyParams(req)
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
