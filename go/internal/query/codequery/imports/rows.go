// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imports

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Rows dispatches one import-dependency read by the request's effective
// query type: file import cycles, cross-module calls, or the default
// import rows. The second return is the cycle enumeration state. It is
// meaningful only for file_import_cycles and is the zero value for every other
// query type; a caller shaping a cycle response must pass it on, so a capped or
// budget-stopped enumeration reports truncated:true with its stop reason and is
// never a silently partial list.
func Rows(
	ctx context.Context,
	graph querycontract.GraphQuery,
	req codemodel.ImportDependencyRequest,
) ([]map[string]any, codemodel.CycleEnumeration, error) {
	switch req.EffectiveQueryType() {
	case "file_import_cycles":
		return CycleRows(ctx, graph, req)
	case "cross_module_calls":
		rows, err := CrossModuleCalls(ctx, graph, req)
		return rows, codemodel.CycleEnumeration{}, err
	default:
		rows, err := ImportRows(ctx, graph, req)
		return rows, codemodel.CycleEnumeration{}, err
	}
}

// ImportRows reads the import rows for the request: scoped module reads
// when a source module is set, package imports, or the direct import
// read. Scope rows bound the corpus before the read; the scan bound
// rejects an over-broad page after it.
func ImportRows(
	ctx context.Context,
	graph querycontract.GraphQuery,
	req codemodel.ImportDependencyRequest,
) ([]map[string]any, error) {
	sourceScopes, err := ModuleScopes(ctx, graph, req, true)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.SourceModule) != "" && len(sourceScopes) == 0 {
		return []map[string]any{}, nil
	}

	params := Params(req)
	if len(sourceScopes) > 0 {
		params["source_paths"] = ScopePaths(sourceScopes)
		params["scan_limit"] = querycontract.ImportDependencyInternalScanLimit + 1
	}

	var cypher string
	switch {
	case req.EffectiveQueryType() == "package_imports":
		cypher = codemodel.PackageImportRowsCypher(req, sourceScopes)
	case len(sourceScopes) > 0:
		cypher = codemodel.SourceModuleImportRowsCypher(req, sourceScopes)
		params["scan_limit"] = querycontract.ImportDependencyInternalScanLimit + 1
	default:
		cypher = codemodel.DirectImportRowsCypher(req)
	}

	rows, err := graph.Run(ctx, cypher, params)
	if err != nil {
		return nil, fmt.Errorf("query import dependency rows: %w", err)
	}
	if len(sourceScopes) == 0 {
		return rows, nil
	}
	if err := codemodel.ImportDependencyScanBoundError(len(rows)); err != nil {
		return nil, err
	}
	rows = codemodel.FilterImportDependencyScopeRows(rows, "repo_id", "source_path", sourceScopes)
	for _, row := range rows {
		row["source_module"] = strings.TrimSpace(req.SourceModule)
	}
	if req.EffectiveQueryType() == "package_imports" {
		rows = codemodel.UniquePackageImportRows(rows)
	}
	codemodel.StripImportDependencyInternalPaths(rows)
	return codemodel.PageImportDependencyRows(req, rows), nil
}

// CycleRows reads the file import cycle edges for the request and shapes
// them into bounded simple-cycle rows. The second return reports how
// enumeration ended (cap, step budget, or neither); callers shaping the
// response must carry it so a partial list says truncated:true with its
// stop reason.
func CycleRows(
	ctx context.Context,
	graph querycontract.GraphQuery,
	req codemodel.ImportDependencyRequest,
) ([]map[string]any, codemodel.CycleEnumeration, error) {
	params := Params(req)
	params["cycle_language"] = "python"
	params["scan_limit"] = querycontract.ImportDependencyInternalScanLimit + 1
	rows, err := graph.Run(ctx, codemodel.FileImportCycleEdgeRowsCypher(req), params)
	if err != nil {
		return nil, codemodel.CycleEnumeration{}, fmt.Errorf("query file import cycle edges: %w", err)
	}
	return codemodel.BuildFileImportCycleRows(req, rows)
}

// CrossModuleCalls reads the cross-module CALLS rows between the
// request's source and target module scopes.
func CrossModuleCalls(
	ctx context.Context,
	graph querycontract.GraphQuery,
	req codemodel.ImportDependencyRequest,
) ([]map[string]any, error) {
	sourceScopes, err := ModuleScopes(ctx, graph, req, true)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.SourceModule) != "" && len(sourceScopes) == 0 {
		return []map[string]any{}, nil
	}
	targetScopes, err := ModuleScopes(ctx, graph, req, false)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.TargetModule) != "" && len(targetScopes) == 0 {
		return []map[string]any{}, nil
	}

	params := Params(req)
	params["scan_limit"] = querycontract.ImportDependencyInternalScanLimit + 1
	if len(sourceScopes) > 0 {
		params["source_paths"] = ScopePaths(sourceScopes)
	}
	if len(targetScopes) > 0 {
		params["target_paths"] = ScopePaths(targetScopes)
	}
	rows, err := graph.Run(ctx, codemodel.CrossModuleCallRowsCypher(req, sourceScopes, targetScopes), params)
	if err != nil {
		return nil, fmt.Errorf("query cross-module call rows: %w", err)
	}
	return codemodel.FilterCrossModuleCallRows(req, rows, sourceScopes, targetScopes)
}

// ModuleScopes reads the module file membership for the request's source
// (source true) or target module: an empty module resolves to no scopes
// without touching the graph.
func ModuleScopes(
	ctx context.Context,
	graph querycontract.GraphQuery,
	req codemodel.ImportDependencyRequest,
	source bool,
) ([]map[string]any, error) {
	module := strings.TrimSpace(req.TargetModule)
	cypher := codemodel.TargetModuleFilesCypher(req)
	pathKey := "target_path"
	if source {
		module = strings.TrimSpace(req.SourceModule)
		cypher = codemodel.SourceModuleFilesCypher(req)
		pathKey = "source_path"
	}
	if module == "" {
		return nil, nil
	}

	params := Params(req)
	params["scan_limit"] = querycontract.ImportDependencyInternalScanLimit + 1
	rows, err := graph.Run(ctx, cypher, params)
	if err != nil {
		return nil, fmt.Errorf("query module file membership: %w", err)
	}
	if err := codemodel.ImportDependencyScanBoundError(len(rows)); err != nil {
		return nil, err
	}
	return UniqueScopes(rows, pathKey), nil
}
