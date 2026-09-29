// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// importCycleRow shapes one enumerated simple cycle. The legacy scalar
// keys keep their reciprocal-era meaning for two-node cycles (the
// smallest file leads, the second file follows) and generalize to the
// first hop for longer ones; the full normalized order travels in the
// cycle_* internal keys the response shaper turns into cycle_length,
// cycle_path, and cycle_edges before stripping them.
func importCycleRow(cycle importCycle) map[string]any {
	first := cycle.steps[0]
	last := cycle.steps[len(cycle.steps)-1]
	files := make([]string, 0, len(cycle.steps))
	sourceModules := make([]string, 0, len(cycle.steps))
	targetModules := make([]string, 0, len(cycle.steps))
	lines := make([]int, 0, len(cycle.steps))
	for _, step := range cycle.steps {
		files = append(files, step.file)
		sourceModules = append(sourceModules, step.sourceModule)
		targetModules = append(targetModules, step.targetModule)
		lines = append(lines, step.lineNumber)
	}
	return map[string]any{
		"repo_id":               first.repoID,
		"repo_name":             first.repoName,
		"source_file":           first.file,
		"target_file":           cycle.steps[1].file,
		"source_module":         first.sourceModule,
		"target_module":         first.targetModule,
		"source_line_number":    first.lineNumber,
		"back_edge_line_number": last.lineNumber,
		"cycle_files":           files,
		"cycle_source_modules":  sourceModules,
		"cycle_target_modules":  targetModules,
		"cycle_lines":           lines,
	}
}

// importCycleRowMatches keeps a cycle when every directional anchor is
// empty or names any member of the cycle. Membership (not position)
// matching is what lets a file anchor find the 3- and 5-node cycles it
// belongs to however the normalized order leads them.
func importCycleRowMatches(req ImportDependencyRequest, row map[string]any) bool {
	if !matchesExactRequestValue(req.RepoID, querycontract.StringVal(row, "repo_id")) {
		return false
	}
	files := importCycleStringList(row, "cycle_files")
	modules := append(
		append([]string{}, importCycleStringList(row, "cycle_source_modules")...),
		importCycleStringList(row, "cycle_target_modules")...,
	)
	if len(files) == 0 {
		files = []string{querycontract.StringVal(row, "source_file"), querycontract.StringVal(row, "target_file")}
	}
	if len(modules) == 0 {
		modules = []string{querycontract.StringVal(row, "source_module"), querycontract.StringVal(row, "target_module")}
	}
	return matchesAnyRequestValue(req.SourceFile, files) &&
		matchesAnyRequestValue(req.TargetFile, files) &&
		matchesAnyRequestValue(req.SourceModule, modules) &&
		matchesAnyRequestValue(req.TargetModule, modules)
}

// matchesAnyRequestValue keeps an empty anchor open and otherwise
// requires an exact member hit.
func matchesAnyRequestValue(requested string, members []string) bool {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return true
	}
	for _, member := range members {
		if member == requested {
			return true
		}
	}
	return false
}

// importCycleStringList reads one internal cycle list back as strings,
// tolerating the []any form JSON round-trips produce.
func importCycleStringList(row map[string]any, key string) []string {
	switch values := row[key].(type) {
	case []string:
		return append([]string{}, values...)
	case []any:
		members := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil
			}
			members = append(members, text)
		}
		return members
	default:
		return nil
	}
}

func importCycleRowKey(row map[string]any) string {
	return strings.Join([]string{
		querycontract.StringVal(row, "repo_id"),
		strings.Join(importCycleStringList(row, "cycle_files"), "\x01"),
	}, "\x00")
}

// compareImportCycleRows orders cycle rows length-ascending, then
// normalized path (repository first, then files), matching the
// enumeration contract (#6851). Legacy scalar keys break any residual
// tie before the proof line numbers do.
func compareImportCycleRows(left, right map[string]any) int {
	leftFiles := importCycleStringList(left, "cycle_files")
	rightFiles := importCycleStringList(right, "cycle_files")
	if len(leftFiles) != len(rightFiles) {
		return compareInts(len(leftFiles), len(rightFiles))
	}
	comparison := compareRowStrings(left, right, []string{"repo_id"})
	if comparison != 0 {
		return comparison
	}
	if path := compareStrings(
		[]string{strings.Join(leftFiles, "\x00")},
		[]string{strings.Join(rightFiles, "\x00")},
	); path != 0 {
		return path
	}
	comparison = compareRowStrings(left, right, []string{
		"source_file", "target_file", "source_module", "target_module",
	})
	if comparison != 0 {
		return comparison
	}
	if leftLine, rightLine := querycontract.IntVal(left, "source_line_number"), querycontract.IntVal(right, "source_line_number"); leftLine != rightLine {
		return compareInts(leftLine, rightLine)
	}
	return compareInts(querycontract.IntVal(left, "back_edge_line_number"), querycontract.IntVal(right, "back_edge_line_number"))
}

// compareImportCycles orders cycles length-ascending, then normalized
// path, so responses are deterministic across runs.
func compareImportCycles(left, right importCycle) int {
	if len(left.steps) != len(right.steps) {
		return compareInts(len(left.steps), len(right.steps))
	}
	for index := range left.steps {
		if left.steps[index].file != right.steps[index].file {
			return compareStrings(
				[]string{left.steps[index].file},
				[]string{right.steps[index].file},
			)
		}
	}
	leftKey := importCycleKey(left)
	rightKey := importCycleKey(right)
	if leftKey != rightKey {
		return compareStrings([]string{leftKey}, []string{rightKey})
	}
	for index := range left.steps {
		if left.steps[index].lineNumber != right.steps[index].lineNumber {
			return compareInts(left.steps[index].lineNumber, right.steps[index].lineNumber)
		}
	}
	return 0
}
