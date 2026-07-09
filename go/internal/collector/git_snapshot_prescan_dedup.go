// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// partitionPreScanFilesForDerive splits files into legacyFiles (still routed
// through Engine.PreScanRepositoryPathsWithWorkers) and deriveEligibleFiles
// (php, javascript, typescript, tsx — parser.IsDerivedPreScanLanguage), so
// the collector can skip a dedicated pre-scan tree-sitter pass for the
// languages proven (#4764) to reproduce their ImportsMap contribution from
// the parse-stage payload alone.
//
// deriveFromParseEnabled MUST be false whenever this generation's parse stage
// does not cover the same file set pre-scan does — i.e. any delta sync with
// FileTargets set, where fullParserFiles (what pre-scan needs) is broader
// than parserFileSet.Files (what parse actually visits this cycle). Passing
// true in that case would silently drop ImportsMap entries for unchanged
// derive-eligible files. See git_snapshot_native.go's SnapshotRepository for
// the caller that decides this.
func partitionPreScanFilesForDerive(
	files []discovery.FileWithSize,
	registry parser.Registry,
	deriveFromParseEnabled bool,
) (legacyFiles []discovery.FileWithSize, deriveEligibleFiles []discovery.FileWithSize) {
	if !deriveFromParseEnabled {
		return files, nil
	}
	legacyFiles = make([]discovery.FileWithSize, 0, len(files))
	deriveEligibleFiles = make([]discovery.FileWithSize, 0, len(files))
	for _, file := range files {
		definition, ok := registry.LookupByPath(file.Path)
		if !ok || !parser.IsDerivedPreScanLanguage(definition.Language) {
			legacyFiles = append(legacyFiles, file)
			continue
		}
		deriveEligibleFiles = append(deriveEligibleFiles, file)
	}
	return legacyFiles, deriveEligibleFiles
}

// mergeParsedFilesIntoDerivedImportsMap folds every derive-eligible parsed
// payload's declaration names into importsMap, reproducing the ImportsMap
// contribution Engine.PreScanRepositoryPathsWithWorkers would have returned
// for those same files, without a second tree-sitter parse pass. Payloads for
// languages outside parser.IsDerivedPreScanLanguage are skipped: their
// ImportsMap contribution already came from the legacy pre-scan pass.
func mergeParsedFilesIntoDerivedImportsMap(importsMap map[string][]string, parsedFiles []map[string]any) {
	for _, parsed := range parsedFiles {
		language, _ := parsed["lang"].(string)
		if !parser.IsDerivedPreScanLanguage(language) {
			continue
		}
		absPath, _ := parsed["path"].(string)
		if absPath == "" {
			continue
		}
		for _, name := range parser.DerivePreScanNames(parsed) {
			importsMap[name] = append(importsMap[name], absPath)
		}
	}
}

// finalizeDerivedPreScanImportsMap sorts each name's path list, matching the
// deterministic ordering Engine.PreScanRepositoryPathsWithWorkers guarantees,
// so callers cannot observe an ordering difference between the legacy and
// derive-from-parse code paths.
func finalizeDerivedPreScanImportsMap(importsMap map[string][]string) {
	for _, paths := range importsMap {
		slices.Sort(paths)
	}
}
