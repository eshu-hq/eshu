// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_language_imports_grant

// The POST /api/v0/code/imports/investigate half of the live grant proof. The
// seed graph, the scoped/unscoped case runner and the squeeze argument live in
// language_query_grant_nornicdb_live_test.go -- read that file's header first.
//
// Every one of the seven shipped builders behind the route is here, reached
// through the request shape the query_type that uses it produces, so the
// statement under proof is the one the handler sends rather than a rewrite of
// it. importDependencyRequest.access is the field the handler sets from the
// caller's AuthContext, so setting it here is the same binding production uses.
package query

import (
	"context"
	"testing"
	"time"
)

// liveGrantImportRequest is the request every case below starts from: no
// repo_id, so the caller's grant is the ONLY repository restriction in the
// statement, and a limit that importDependencyParams turns into a page of 2 --
// below the six rows the out-of-grant repository can supply.
func liveGrantImportRequest(queryType string, access repositoryAccessFilter) importDependencyRequest {
	return importDependencyRequest{
		QueryType: queryType,
		Language:  liveGrantLanguage,
		Limit:     1,
		access:    access,
	}
}

// liveGrantScanLimit is the in-Go paging bound the builders that page after the
// scan carry. Two, for the same reason the page above is two.
const liveGrantScanLimit = 2

// liveGrantAllFilePaths is every seeded file's path, granted and out-of-grant
// alike. The builders that take $source_paths / $target_paths get all of them,
// so the path predicate cannot be what excludes the out-of-grant rows -- only
// the grant can.
func liveGrantAllFilePaths() []string {
	paths := []string{"/live/" + liveGrantGrantedMarker + "/z-src-0/" + liveGrantGrantedMarker + "-0.py"}
	for index := 0; index < liveGrantOutOfGrantRows; index++ {
		paths = append(paths, liveGrantOtherFilePath(index))
	}
	return paths
}

// liveGrantModuleScopes is the shape importDependencyModuleScopes returns:
// one {repo_id, path} map per file. Passing the out-of-grant repository's own
// scopes in makes the grant the only thing that can drop its rows.
func liveGrantModuleScopes() []map[string]any {
	scopes := make([]map[string]any, 0, liveGrantOutOfGrantRows)
	for index := 0; index < liveGrantOutOfGrantRows; index++ {
		scopes = append(scopes, map[string]any{"repo_id": liveGrantOtherRepo, "path": liveGrantOtherFilePath(index)})
	}
	return scopes
}

// TestLiveNornicDBImportDependencyGrantBindsEveryBuilder covers all seven
// shipped builders across all six query types.
func TestLiveNornicDBImportDependencyGrantBindsEveryBuilder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := openLiveGrantDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveGrantGraph(ctx, t, driver)

	scanParams := map[string]any{"scan_limit": liveGrantScanLimit}
	cycleParams := map[string]any{"scan_limit": liveGrantScanLimit, "cycle_language": liveGrantLanguage}
	pathParams := map[string]any{
		"scan_limit":   liveGrantScanLimit,
		"source_paths": liveGrantAllFilePaths(),
		"target_paths": liveGrantAllFilePaths(),
	}

	runLiveGrantCases(ctx, t, driver, []liveGrantCase{
		{
			// query_type imports_by_file, no source module: the direct edge page.
			name: "directImportRowsCypher/imports_by_file",
			build: func(access repositoryAccessFilter) (string, map[string]any) {
				req := liveGrantImportRequest("imports_by_file", access)
				return directImportRowsCypher(req), importDependencyParams(req)
			},
		},
		{
			// query_type importers: the same builder anchored on the module
			// every seeded file imports, so the out-of-grant repository has six
			// edges competing for the page.
			name: "directImportRowsCypher/importers",
			build: func(access repositoryAccessFilter) (string, map[string]any) {
				req := liveGrantImportRequest("importers", access)
				req.TargetModule = liveGrantImportedModue
				return directImportRowsCypher(req), importDependencyParams(req)
			},
		},
		{
			// query_type package_imports with no source module: the DISTINCT
			// logical-module page. Each out-of-grant file owns a module of its
			// own, so the DISTINCT set is large enough to squeeze.
			name: "packageImportRowsCypher/package_imports",
			build: func(access repositoryAccessFilter) (string, map[string]any) {
				req := liveGrantImportRequest("package_imports", access)
				return packageImportRowsCypher(req, nil), importDependencyParams(req)
			},
		},
		{
			// query_type package_imports with a source module: the scan-bounded
			// shape that pages in Go.
			name:   "packageImportRowsCypher/package_imports scoped",
			params: pathParams,
			build: func(access repositoryAccessFilter) (string, map[string]any) {
				req := liveGrantImportRequest("package_imports", access)
				req.SourceModule = liveGrantSourceModule
				return packageImportRowsCypher(req, liveGrantModuleScopes()), importDependencyParams(req)
			},
		},
		{
			// The source-module membership read that query_type
			// module_dependencies (and any request carrying source_module) runs
			// first.
			name:   "sourceModuleFilesCypher/module_dependencies",
			params: scanParams,
			build: func(access repositoryAccessFilter) (string, map[string]any) {
				req := liveGrantImportRequest("module_dependencies", access)
				req.SourceModule = liveGrantSourceModule
				return sourceModuleFilesCypher(req), importDependencyParams(req)
			},
		},
		{
			// The target-module membership read, which cross_module_calls runs
			// for its callee side.
			name:   "targetModuleFilesCypher/cross_module_calls",
			params: scanParams,
			build: func(access repositoryAccessFilter) (string, map[string]any) {
				req := liveGrantImportRequest("cross_module_calls", access)
				req.TargetModule = liveGrantTargetModule
				return targetModuleFilesCypher(req), importDependencyParams(req)
			},
		},
		{
			// The import-edge read a resolved source module leads to.
			name:   "sourceModuleImportRowsCypher/module_dependencies",
			params: pathParams,
			build: func(access repositoryAccessFilter) (string, map[string]any) {
				req := liveGrantImportRequest("module_dependencies", access)
				req.SourceModule = liveGrantSourceModule
				return sourceModuleImportRowsCypher(req, liveGrantModuleScopes()), importDependencyParams(req)
			},
		},
		{
			// query_type file_import_cycles.
			name:   "fileImportCycleEdgeRowsCypher/file_import_cycles",
			params: cycleParams,
			build: func(access repositoryAccessFilter) (string, map[string]any) {
				req := liveGrantImportRequest("file_import_cycles", access)
				return fileImportCycleEdgeRowsCypher(req), importDependencyParams(req)
			},
		},
		{
			// query_type cross_module_calls, both repository endpoints
			// unanchored. This is the builder that binds the grant TWICE, once
			// per endpoint, so the callee's repository identity cannot leak to a
			// caller granted only the caller's side.
			name:   "crossModuleCallRowsCypher/cross_module_calls",
			params: pathParams,
			build: func(access repositoryAccessFilter) (string, map[string]any) {
				req := liveGrantImportRequest("cross_module_calls", access)
				return crossModuleCallRowsCypher(req, nil, nil), importDependencyParams(req)
			},
		},
		{
			// The same builder with both module scopes resolved, which adds the
			// source_paths and target_paths predicates alongside the two grant
			// conditions.
			name:   "crossModuleCallRowsCypher/cross_module_calls scoped",
			params: pathParams,
			build: func(access repositoryAccessFilter) (string, map[string]any) {
				req := liveGrantImportRequest("cross_module_calls", access)
				req.SourceModule = liveGrantSourceModule
				req.TargetModule = liveGrantTargetModule
				return crossModuleCallRowsCypher(req, liveGrantModuleScopes(), liveGrantModuleScopes()), importDependencyParams(req)
			},
		},
	})
}
