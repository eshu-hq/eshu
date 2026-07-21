// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var errImportDependencyScopeTooBroad = errors.New("import dependency scope exceeds internal scan limit")

type importCycleEdge struct {
	repoID       string
	repoName     string
	sourcePath   string
	sourceFile   string
	sourceModule string
	language     string
	targetModule string
	lineNumber   int
}

type importCycleDirection struct {
	repoID       string
	sourceModule string
	targetModule string
}

type importDependencyScopeKey struct {
	repoID string
	path   string
}

// buildFileImportCycleRows reconstructs reciprocal Python import edges after a
// bounded candidate scan. Duplicate directed edges retain their earliest
// positive source line.
func buildFileImportCycleRows(
	req importDependencyRequest,
	edgeRows []map[string]any,
) ([]map[string]any, error) {
	if err := importDependencyScanBoundError(len(edgeRows)); err != nil {
		return nil, err
	}

	directedEdges := deduplicateImportCycleEdges(req, edgeRows)
	byDirection := make(map[importCycleDirection][]importCycleEdge, len(directedEdges))
	for _, edge := range directedEdges {
		direction := importCycleDirection{
			repoID:       edge.repoID,
			sourceModule: edge.sourceModule,
			targetModule: edge.targetModule,
		}
		byDirection[direction] = append(byDirection[direction], edge)
	}

	cycleRows := make([]map[string]any, 0)
	seenCycles := make(map[string]struct{})
	for _, sourceEdge := range directedEdges {
		reverse := importCycleDirection{
			repoID:       sourceEdge.repoID,
			sourceModule: sourceEdge.targetModule,
			targetModule: sourceEdge.sourceModule,
		}
		for _, backEdge := range byDirection[reverse] {
			if sourceEdge.sourceFile >= backEdge.sourceFile {
				continue
			}
			row := importCycleRow(sourceEdge, backEdge)
			if !importCycleRowMatches(req, row) {
				continue
			}
			key := importCycleRowKey(row)
			if _, exists := seenCycles[key]; exists {
				continue
			}
			seenCycles[key] = struct{}{}
			cycleRows = append(cycleRows, row)
			if len(cycleRows) > importDependencyInternalScanLimit {
				return nil, fmt.Errorf(
					"%w: reciprocal cycle candidates exceed %d",
					errImportDependencyScopeTooBroad,
					importDependencyInternalScanLimit,
				)
			}
		}
	}

	sort.Slice(cycleRows, func(i, j int) bool {
		return compareImportCycleRows(cycleRows[i], cycleRows[j]) < 0
	})
	return pageImportDependencyRows(req, cycleRows), nil
}

// filterCrossModuleCallRows removes cross-repository candidates before stable
// ordering and paging. Repository identity is normalized only after filtering.
func filterCrossModuleCallRows(
	req importDependencyRequest,
	rows []map[string]any,
	sourceScopes []map[string]any,
	targetScopes []map[string]any,
) ([]map[string]any, error) {
	if err := importDependencyScanBoundError(len(rows)); err != nil {
		return nil, err
	}

	sourceScopeIndex := newImportDependencyScopeIndex(sourceScopes)
	targetScopeIndex := newImportDependencyScopeIndex(targetScopes)
	filtered := make([]map[string]any, 0, len(rows))
	for _, candidate := range rows {
		if !crossModuleCallRowMatches(req, candidate) ||
			!importDependencyRowMatchesScope(candidate, "source_repo_id", "source_path", sourceScopeIndex) ||
			!importDependencyRowMatchesScope(candidate, "target_repo_id", "target_path", targetScopeIndex) {
			continue
		}
		filtered = append(filtered, cloneQueryAnyMap(candidate))
	}
	sort.Slice(filtered, func(i, j int) bool {
		return compareCrossModuleCallRows(filtered[i], filtered[j]) < 0
	})

	page := pageImportDependencyRows(req, filtered)
	for _, row := range page {
		row["repo_id"] = StringVal(row, "source_repo_id")
		delete(row, "source_repo_id")
		delete(row, "target_repo_id")
		delete(row, "source_language")
		delete(row, "target_language")
		delete(row, "source_path")
		delete(row, "target_path")
	}
	return page, nil
}

func filterImportDependencyScopeRows(
	rows []map[string]any,
	repoKey string,
	pathKey string,
	scopes []map[string]any,
) []map[string]any {
	scopeIndex := newImportDependencyScopeIndex(scopes)
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if importDependencyRowMatchesScope(row, repoKey, pathKey, scopeIndex) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func importDependencyRowMatchesScope(
	row map[string]any,
	repoKey string,
	pathKey string,
	scopeIndex map[importDependencyScopeKey]struct{},
) bool {
	if len(scopeIndex) == 0 {
		return true
	}
	_, exists := scopeIndex[importDependencyScopeKey{
		repoID: StringVal(row, repoKey),
		path:   StringVal(row, pathKey),
	}]
	return exists
}

func newImportDependencyScopeIndex(scopes []map[string]any) map[importDependencyScopeKey]struct{} {
	index := make(map[importDependencyScopeKey]struct{}, len(scopes))
	for _, scope := range scopes {
		key := importDependencyScopeKey{
			repoID: StringVal(scope, "repo_id"),
			path:   StringVal(scope, "path"),
		}
		if key.repoID == "" || key.path == "" {
			continue
		}
		index[key] = struct{}{}
	}
	return index
}

func uniquePackageImportRows(rows []map[string]any) []map[string]any {
	seen := make(map[string]struct{}, len(rows))
	unique := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		key := strings.Join([]string{
			StringVal(row, "repo_id"),
			StringVal(row, "target_module"),
			StringVal(row, "language"),
		}, "\x00")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, row)
	}
	sort.Slice(unique, func(i, j int) bool {
		return compareRowStrings(unique[i], unique[j], []string{
			"repo_id", "target_module", "language",
		}) < 0
	})
	return unique
}

func stripImportDependencyInternalPaths(rows []map[string]any) {
	for _, row := range rows {
		delete(row, "source_path")
		delete(row, "target_path")
	}
}

func importDependencyScanBoundError(rowCount int) error {
	if rowCount <= importDependencyInternalScanLimit {
		return nil
	}
	return fmt.Errorf(
		"%w: received %d candidates, maximum is %d",
		errImportDependencyScopeTooBroad,
		rowCount,
		importDependencyInternalScanLimit,
	)
}

func deduplicateImportCycleEdges(
	req importDependencyRequest,
	rows []map[string]any,
) []importCycleEdge {
	deduplicated := make(map[string]importCycleEdge, len(rows))
	for _, row := range rows {
		edge, ok := importCycleEdgeFromRow(req, row)
		if !ok {
			continue
		}
		key := strings.Join([]string{
			edge.repoID,
			edge.sourcePath,
			edge.sourceFile,
			edge.targetModule,
		}, "\x00")
		current, exists := deduplicated[key]
		if !exists || earlierPositiveLine(edge.lineNumber, current.lineNumber) {
			deduplicated[key] = edge
		}
	}

	edges := make([]importCycleEdge, 0, len(deduplicated))
	for _, edge := range deduplicated {
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool {
		return compareImportCycleEdges(edges[i], edges[j]) < 0
	})
	return edges
}

func importCycleEdgeFromRow(
	req importDependencyRequest,
	row map[string]any,
) (importCycleEdge, bool) {
	edge := importCycleEdge{
		repoID:       StringVal(row, "repo_id"),
		repoName:     StringVal(row, "repo_name"),
		sourcePath:   StringVal(row, "source_path"),
		sourceFile:   StringVal(row, "source_file"),
		sourceModule: pythonSourceModule(StringVal(row, "source_name")),
		language:     strings.ToLower(strings.TrimSpace(StringVal(row, "language"))),
		targetModule: strings.TrimSpace(StringVal(row, "target_module")),
		lineNumber:   IntVal(row, "line_number"),
	}
	if edge.repoID == "" || edge.sourceFile == "" || edge.sourceModule == "" || edge.targetModule == "" {
		return importCycleEdge{}, false
	}
	if edge.language != "python" {
		return importCycleEdge{}, false
	}
	if repoID := strings.TrimSpace(req.RepoID); repoID != "" && edge.repoID != repoID {
		return importCycleEdge{}, false
	}
	if language := req.normalizedLanguage(); language != "" && edge.language != language {
		return importCycleEdge{}, false
	}
	return edge, true
}

func importCycleRow(sourceEdge, backEdge importCycleEdge) map[string]any {
	return map[string]any{
		"repo_id":               sourceEdge.repoID,
		"repo_name":             sourceEdge.repoName,
		"source_file":           sourceEdge.sourceFile,
		"target_file":           backEdge.sourceFile,
		"source_module":         backEdge.targetModule,
		"target_module":         sourceEdge.targetModule,
		"source_line_number":    sourceEdge.lineNumber,
		"back_edge_line_number": backEdge.lineNumber,
	}
}

func importCycleRowMatches(req importDependencyRequest, row map[string]any) bool {
	return matchesExactRequestValue(req.RepoID, StringVal(row, "repo_id")) &&
		matchesExactRequestValue(req.SourceFile, StringVal(row, "source_file")) &&
		matchesExactRequestValue(req.TargetFile, StringVal(row, "target_file")) &&
		matchesExactRequestValue(req.SourceModule, StringVal(row, "source_module")) &&
		matchesExactRequestValue(req.TargetModule, StringVal(row, "target_module"))
}

func crossModuleCallRowMatches(req importDependencyRequest, row map[string]any) bool {
	sourceRepoID := StringVal(row, "source_repo_id")
	targetRepoID := StringVal(row, "target_repo_id")
	if sourceRepoID == "" || sourceRepoID != targetRepoID {
		return false
	}
	if !matchesExactRequestValue(req.RepoID, sourceRepoID) ||
		!matchesExactRequestValue(req.SourceFile, StringVal(row, "source_file")) ||
		!matchesExactRequestValue(req.TargetFile, StringVal(row, "target_file")) ||
		!matchesExactRequestValue(req.SourceModule, StringVal(row, "source_module")) ||
		!matchesExactRequestValue(req.TargetModule, StringVal(row, "target_module")) {
		return false
	}
	if language := req.normalizedLanguage(); language != "" {
		sourceLanguage := strings.ToLower(strings.TrimSpace(StringVal(row, "source_language")))
		targetLanguage := strings.ToLower(strings.TrimSpace(StringVal(row, "target_language")))
		if sourceLanguage != language && targetLanguage != language {
			return false
		}
	}
	return true
}

func matchesExactRequestValue(requested, actual string) bool {
	requested = strings.TrimSpace(requested)
	return requested == "" || actual == requested
}

func pythonSourceModule(sourceName string) string {
	sourceName = strings.TrimSpace(sourceName)
	if !strings.HasSuffix(strings.ToLower(sourceName), ".py") {
		return ""
	}
	return sourceName[:len(sourceName)-len(".py")]
}

func earlierPositiveLine(candidate, current int) bool {
	if candidate <= 0 {
		return false
	}
	return current <= 0 || candidate < current
}

func importCycleRowKey(row map[string]any) string {
	return strings.Join([]string{
		StringVal(row, "repo_id"),
		StringVal(row, "source_file"),
		StringVal(row, "target_file"),
		StringVal(row, "source_module"),
		StringVal(row, "target_module"),
	}, "\x00")
}

func compareImportCycleEdges(left, right importCycleEdge) int {
	return compareStrings(
		[]string{left.repoID, left.sourceFile, left.targetModule, left.sourcePath},
		[]string{right.repoID, right.sourceFile, right.targetModule, right.sourcePath},
	)
}

func compareImportCycleRows(left, right map[string]any) int {
	comparison := compareRowStrings(left, right, []string{
		"repo_id", "source_file", "target_file", "source_module", "target_module",
	})
	if comparison != 0 {
		return comparison
	}
	if leftLine, rightLine := IntVal(left, "source_line_number"), IntVal(right, "source_line_number"); leftLine != rightLine {
		return compareInts(leftLine, rightLine)
	}
	return compareInts(IntVal(left, "back_edge_line_number"), IntVal(right, "back_edge_line_number"))
}

func compareCrossModuleCallRows(left, right map[string]any) int {
	return compareRowStrings(left, right, []string{
		"source_repo_id", "source_file", "source_id", "target_repo_id",
		"target_file", "target_id", "call_kind", "reason", "source_module", "target_module",
	})
}

func compareRowStrings(left, right map[string]any, keys []string) int {
	leftValues := make([]string, len(keys))
	rightValues := make([]string, len(keys))
	for index, key := range keys {
		leftValues[index] = StringVal(left, key)
		rightValues[index] = StringVal(right, key)
	}
	return compareStrings(leftValues, rightValues)
}

func compareStrings(left, right []string) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}

func compareInts(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func pageImportDependencyRows(req importDependencyRequest, rows []map[string]any) []map[string]any {
	if req.Offset >= len(rows) {
		return []map[string]any{}
	}
	end := req.Offset + req.queryLimit()
	if end > len(rows) {
		end = len(rows)
	}
	return rows[req.Offset:end]
}
