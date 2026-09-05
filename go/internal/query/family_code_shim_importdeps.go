// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// This file is part of the #6060 lane-A P0 shim split out of
// family_code_shim.go to keep every file under the repo's 500-line
// cap. The deletion protocol in family_code_shim.go's header applies
// to every entry here: delete each entry with its named handler move.

import (
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// importDependencyRequest aliases the leaf-owned investigation request so
// the staying handler, executors, and tests keep their signatures and
// literals unchanged. The request split to codemodel with its methods in
// L1; Validate, EffectiveQueryType, NormalizedLanguage, and QueryLimit are
// the exported methods staying callers use, and Access carries the grant.
// Delete with code_import_dependencies.go's handler move.
type importDependencyRequest = codemodel.ImportDependencyRequest

// errImportDependencyScopeTooBroad aliases the leaf-owned scan sentinel so
// the staying handler and tests keep matching it with errors.Is (same
// error value). Delete with code_import_dependencies.go's handler move.
var errImportDependencyScopeTooBroad = codemodel.ErrImportDependencyScopeTooBroad

// importDependencyResponse forwards to the leaf-owned envelope shaper so
// the staying handler keeps its call site unchanged. Delete with
// code_import_dependencies.go's handler move.
func importDependencyResponse(req importDependencyRequest, rows []map[string]any) map[string]any {
	return codemodel.ImportDependencyResponse(req, rows)
}

// directImportRowsCypher forwards to the leaf-owned edge builder so the
// staying executor and tests keep their call sites unchanged. Delete with
// code_import_dependencies.go's handler move.
func directImportRowsCypher(req importDependencyRequest) string {
	return codemodel.DirectImportRowsCypher(req)
}

// packageImportRowsCypher forwards to the leaf-owned package builder so
// the staying executor and tests keep their call sites unchanged. Delete
// with code_import_dependencies.go's handler move.
func packageImportRowsCypher(req importDependencyRequest, sourceScopes []map[string]any) string {
	return codemodel.PackageImportRowsCypher(req, sourceScopes)
}

// sourceModuleFilesCypher forwards to the leaf-owned membership builder so
// the staying executor and tests keep their call sites unchanged. Delete
// with code_import_dependencies.go's handler move.
func sourceModuleFilesCypher(req importDependencyRequest) string {
	return codemodel.SourceModuleFilesCypher(req)
}

// targetModuleFilesCypher forwards to the leaf-owned membership builder so
// the staying executor and tests keep their call sites unchanged. Delete
// with code_import_dependencies.go's handler move.
func targetModuleFilesCypher(req importDependencyRequest) string {
	return codemodel.TargetModuleFilesCypher(req)
}

// sourceModuleImportRowsCypher forwards to the leaf-owned import builder
// so the staying executor and tests keep their call sites unchanged.
// Delete with code_import_dependencies.go's handler move.
func sourceModuleImportRowsCypher(req importDependencyRequest, sourceScopes []map[string]any) string {
	return codemodel.SourceModuleImportRowsCypher(req, sourceScopes)
}

// fileImportCycleEdgeRowsCypher forwards to the leaf-owned cycle builder so
// the staying executor and tests keep their call sites unchanged. Delete
// with code_import_dependencies.go's handler move.
func fileImportCycleEdgeRowsCypher(req importDependencyRequest) string {
	return codemodel.FileImportCycleEdgeRowsCypher(req)
}

// crossModuleCallRowsCypher forwards to the leaf-owned call-path builder
// so the staying executor and tests keep their call sites unchanged.
// Delete with code_import_dependencies.go's handler move.
func crossModuleCallRowsCypher(
	req importDependencyRequest,
	sourceScopes []map[string]any,
	targetScopes []map[string]any,
) string {
	return codemodel.CrossModuleCallRowsCypher(req, sourceScopes, targetScopes)
}

// buildFileImportCycleRows forwards to the leaf-owned cycle reconstructor
// so the staying executor and tests keep their call sites unchanged.
// Delete with code_import_dependencies.go's handler move.
func buildFileImportCycleRows(
	req importDependencyRequest,
	edgeRows []map[string]any,
) ([]map[string]any, error) {
	return codemodel.BuildFileImportCycleRows(req, edgeRows)
}

// filterCrossModuleCallRows forwards to the leaf-owned scope filter so the
// staying executor and tests keep their call sites unchanged. Delete with
// code_import_dependencies.go's handler move.
func filterCrossModuleCallRows(
	req importDependencyRequest,
	rows []map[string]any,
	sourceScopes []map[string]any,
	targetScopes []map[string]any,
) ([]map[string]any, error) {
	return codemodel.FilterCrossModuleCallRows(req, rows, sourceScopes, targetScopes)
}

// filterImportDependencyScopeRows forwards to the leaf-owned scope filter
// so the staying executor and tests keep their call sites unchanged.
// Delete with code_import_dependencies.go's handler move.
func filterImportDependencyScopeRows(
	rows []map[string]any,
	repoKey string,
	pathKey string,
	scopes []map[string]any,
) []map[string]any {
	return codemodel.FilterImportDependencyScopeRows(rows, repoKey, pathKey, scopes)
}

// uniquePackageImportRows forwards to the leaf-owned module de-duplicator
// so the staying executor and tests keep their call sites unchanged.
// Delete with code_import_dependencies.go's handler move.
func uniquePackageImportRows(rows []map[string]any) []map[string]any {
	return codemodel.UniquePackageImportRows(rows)
}

// stripImportDependencyInternalPaths forwards to the leaf-owned path
// stripper so the staying executor keeps its call site unchanged. Delete
// with code_import_dependencies.go's handler move.
func stripImportDependencyInternalPaths(rows []map[string]any) {
	codemodel.StripImportDependencyInternalPaths(rows)
}

// importDependencyScanBoundError forwards to the leaf-owned scan guard so
// the staying executor keeps its call site unchanged. Delete with
// code_import_dependencies.go's handler move.
func importDependencyScanBoundError(rowCount int) error {
	return codemodel.ImportDependencyScanBoundError(rowCount)
}

// pageImportDependencyRows forwards to the leaf-owned pager so the staying
// executor keeps its call site unchanged. Delete with
// code_import_dependencies.go's handler move.
func pageImportDependencyRows(req importDependencyRequest, rows []map[string]any) []map[string]any {
	return codemodel.PageImportDependencyRows(req, rows)
}

// importDependencyUniqueModules forwards to the leaf-owned module
// de-duplicator so the staying exactness tests keep their call sites
// unchanged. Delete with code_import_dependencies.go's handler move.
func importDependencyUniqueModules(rows []map[string]any) []map[string]any {
	return codemodel.ImportDependencyUniqueModules(rows)
}
