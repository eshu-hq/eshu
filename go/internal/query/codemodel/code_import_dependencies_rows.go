// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

var ErrImportDependencyScopeTooBroad = errors.New("import dependency scope exceeds internal scan limit")

// cloneQueryAnyMap is a family-local copy of root's code_search_metadata.go
// helper of the same name. The shallow map clone is shared with staying
// root shapers that cannot cross the package boundary, so the leaf carries
// this byte-identical copy instead of importing root. Keep it
// behavior-identical to its root source.
func cloneQueryAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

type importCycleEdge struct {
	repoID       string
	repoName     string
	sourcePath   string
	sourceFile   string
	sourceModule string
	language     string
	targetModule string
	lineNumber   int
	state        importCycleEdgeState
}

type importDependencyScopeKey struct {
	repoID string
	path   string
}

// BuildFileImportCycleRows enumerates bounded Python simple import cycles
// after a bounded candidate scan (#6851): reciprocal pairs through
// max_cycle_length (default 5) file cycles, rotation-deduplicated to the
// lexicographically smallest start and ordered length-ascending, then
// normalized path. Duplicate directed edges retain their earliest
// positive source line.
//
// The second return reports how the enumeration ended: the cap or the
// step budget can stop it early. The response carries that state as
// truncated:true plus the stop reason, so a capped list is never silently
// partial. The cap (1,000) sits far below the 25,000-row internal scan
// limit the edge fetch enforces, so the old reciprocal-era overflow guard
// is subsumed: a capped enumeration always pages truncated.
//
// Directional anchors (source_file, target_file, source_module,
// target_module) match when any cycle member matches, after enumeration:
// the edge fetch intentionally carries no directional filter so an anchor
// cannot remove the closing edge of a cycle it belongs to. Narrow
// repo_id for complete answers on dense graphs: the enumeration cap
// applies before this filter.
func BuildFileImportCycleRows(
	req ImportDependencyRequest,
	edgeRows []map[string]any,
) ([]map[string]any, CycleEnumeration, error) {
	if err := ImportDependencyScanBoundError(len(edgeRows)); err != nil {
		return nil, CycleEnumeration{}, err
	}

	directedEdges, edgeFlags := partitionImportCycleEdges(deduplicateImportCycleEdges(req, edgeRows))
	cycles, enumeration := enumerateImportCycles(directedEdges, req.effectiveMaxCycleLength())
	enumeration.EdgeFlags = edgeFlags

	cycleRows := make([]map[string]any, 0, len(cycles))
	seenCycles := make(map[string]struct{})
	for _, cycle := range cycles {
		row := importCycleRow(cycle)
		if !importCycleRowMatches(req, row) {
			continue
		}
		key := importCycleRowKey(row)
		if _, exists := seenCycles[key]; exists {
			continue
		}
		seenCycles[key] = struct{}{}
		cycleRows = append(cycleRows, row)
	}

	sort.Slice(cycleRows, func(i, j int) bool {
		return compareImportCycleRows(cycleRows[i], cycleRows[j]) < 0
	})
	return PageImportDependencyRows(req, cycleRows), enumeration, nil
}

// FilterCrossModuleCallRows removes cross-repository candidates before stable
// ordering and paging. Repository identity is normalized only after filtering.
func FilterCrossModuleCallRows(
	req ImportDependencyRequest,
	rows []map[string]any,
	sourceScopes []map[string]any,
	targetScopes []map[string]any,
) ([]map[string]any, error) {
	if err := ImportDependencyScanBoundError(len(rows)); err != nil {
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

	page := PageImportDependencyRows(req, filtered)
	for _, row := range page {
		row["repo_id"] = querycontract.StringVal(row, "source_repo_id")
		delete(row, "source_repo_id")
		delete(row, "target_repo_id")
		delete(row, "source_language")
		delete(row, "target_language")
		delete(row, "source_path")
		delete(row, "target_path")
	}
	return page, nil
}

func FilterImportDependencyScopeRows(
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
		repoID: querycontract.StringVal(row, repoKey),
		path:   querycontract.StringVal(row, pathKey),
	}]
	return exists
}

func newImportDependencyScopeIndex(scopes []map[string]any) map[importDependencyScopeKey]struct{} {
	index := make(map[importDependencyScopeKey]struct{}, len(scopes))
	for _, scope := range scopes {
		key := importDependencyScopeKey{
			repoID: querycontract.StringVal(scope, "repo_id"),
			path:   querycontract.StringVal(scope, "path"),
		}
		if key.repoID == "" || key.path == "" {
			continue
		}
		index[key] = struct{}{}
	}
	return index
}

func UniquePackageImportRows(rows []map[string]any) []map[string]any {
	seen := make(map[string]struct{}, len(rows))
	unique := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		key := strings.Join([]string{
			querycontract.StringVal(row, "repo_id"),
			querycontract.StringVal(row, "target_module"),
			querycontract.StringVal(row, "language"),
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

func StripImportDependencyInternalPaths(rows []map[string]any) {
	for _, row := range rows {
		delete(row, "source_path")
		delete(row, "target_path")
	}
}

func ImportDependencyScanBoundError(rowCount int) error {
	if rowCount <= querycontract.ImportDependencyInternalScanLimit {
		return nil
	}
	return fmt.Errorf(
		"%w: received %d candidates, maximum is %d",
		ErrImportDependencyScopeTooBroad,
		rowCount,
		querycontract.ImportDependencyInternalScanLimit,
	)
}

func deduplicateImportCycleEdges(
	req ImportDependencyRequest,
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
		if exists {
			// Rows sharing an edge key fold their flag states whichever row's
			// line number wins, so the kept edge is as strong as its strongest
			// proof.
			folded := foldImportCycleEdgeState(current.state, edge.state)
			if earlierPositiveLine(edge.lineNumber, current.lineNumber) {
				current = edge
			}
			current.state = folded
			deduplicated[key] = current
			continue
		}
		deduplicated[key] = edge
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
	req ImportDependencyRequest,
	row map[string]any,
) (importCycleEdge, bool) {
	edge := importCycleEdge{
		repoID:       querycontract.StringVal(row, "repo_id"),
		repoName:     querycontract.StringVal(row, "repo_name"),
		sourcePath:   querycontract.StringVal(row, "source_path"),
		sourceFile:   querycontract.StringVal(row, "source_file"),
		sourceModule: pythonSourceModule(querycontract.StringVal(row, "source_name")),
		language:     strings.ToLower(strings.TrimSpace(querycontract.StringVal(row, "language"))),
		targetModule: strings.TrimSpace(querycontract.StringVal(row, "target_module")),
		lineNumber:   querycontract.IntVal(row, "line_number"),
		state:        importCycleEdgeStateFromRow(row),
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
	if language := req.NormalizedLanguage(); language != "" && edge.language != language {
		return importCycleEdge{}, false
	}
	return edge, true
}

func crossModuleCallRowMatches(req ImportDependencyRequest, row map[string]any) bool {
	sourceRepoID := querycontract.StringVal(row, "source_repo_id")
	targetRepoID := querycontract.StringVal(row, "target_repo_id")
	if sourceRepoID == "" || sourceRepoID != targetRepoID {
		return false
	}
	if !matchesExactRequestValue(req.RepoID, sourceRepoID) ||
		!matchesExactRequestValue(req.SourceFile, querycontract.StringVal(row, "source_file")) ||
		!matchesExactRequestValue(req.TargetFile, querycontract.StringVal(row, "target_file")) ||
		!matchesExactRequestValue(req.SourceModule, querycontract.StringVal(row, "source_module")) ||
		!matchesExactRequestValue(req.TargetModule, querycontract.StringVal(row, "target_module")) {
		return false
	}
	if language := req.NormalizedLanguage(); language != "" {
		sourceLanguage := strings.ToLower(strings.TrimSpace(querycontract.StringVal(row, "source_language")))
		targetLanguage := strings.ToLower(strings.TrimSpace(querycontract.StringVal(row, "target_language")))
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

func compareImportCycleEdges(left, right importCycleEdge) int {
	return compareStrings(
		[]string{left.repoID, left.sourceFile, left.targetModule, left.sourcePath},
		[]string{right.repoID, right.sourceFile, right.targetModule, right.sourcePath},
	)
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
		leftValues[index] = querycontract.StringVal(left, key)
		rightValues[index] = querycontract.StringVal(right, key)
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

func PageImportDependencyRows(req ImportDependencyRequest, rows []map[string]any) []map[string]any {
	if req.Offset >= len(rows) {
		return []map[string]any{}
	}
	end := req.Offset + req.QueryLimit()
	if end > len(rows) {
		end = len(rows)
	}
	return rows[req.Offset:end]
}
