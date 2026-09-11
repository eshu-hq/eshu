// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// HasExplicitImportedTarget reports whether call has an explicit import
// binding in fileData's import metadata.
func HasExplicitImportedTarget(
	fileData map[string]any,
	call map[string]any,
) bool {
	return len(codeCallImportedTargets(payloadcore.MapSlice(fileData["imports"]), call)) > 0
}

func resolvePythonImportedRepositorySymbolTarget(
	index EntityIndex,
	language string,
	rawPath string,
	relativePath string,
	importSource string,
	paths []string,
	symbolName string,
) (string, string) {
	if language != "python" || len(paths) != 1 {
		return "", ""
	}
	path := NormalizePath(paths[0])
	if path == "" {
		return "", ""
	}
	if !codeCallPythonImportSourceCanContainPath(rawPath, relativePath, importSource, path) {
		return "", ""
	}
	entityID := index.UniqueNameByPath[path][symbolName]
	if entityID == "" {
		return "", ""
	}
	return entityID, index.entityFileByID[entityID]
}

func codeCallPythonImportSourceCanContainPath(
	rawPath string,
	relativePath string,
	importSource string,
	targetPath string,
) bool {
	repositoryRoot := RepositoryRoot(rawPath, relativePath)
	if repositoryRoot == "" {
		return false
	}
	targetPath = NormalizePath(targetPath)
	if targetPath == "" {
		return false
	}
	relativeTarget := strings.TrimPrefix(targetPath, NormalizePath(repositoryRoot)+"/")
	if relativeTarget == targetPath {
		return false
	}
	modulePath := strings.ReplaceAll(strings.Trim(strings.TrimSpace(importSource), "."), ".", "/")
	if modulePath == "" || strings.Contains(modulePath, "/../") {
		return false
	}
	return relativeTarget == modulePath+".py" ||
		relativeTarget == modulePath+"/__init__.py" ||
		strings.HasPrefix(relativeTarget, modulePath+"/")
}

func resolvePythonImportedSourceCandidateTarget(
	index EntityIndex,
	language string,
	rawPath string,
	relativePath string,
	target codeCallImportedTarget,
) (string, string) {
	if language != "python" {
		return "", ""
	}
	matches := make(map[string]struct{})
	for _, path := range codeCallImportSourceCandidates(
		rawPath,
		relativePath,
		target.importSource,
		language,
	) {
		entityID := index.UniqueNameByPath[path][target.symbolName]
		if entityID != "" {
			matches[entityID] = struct{}{}
		}
	}
	if len(matches) != 1 {
		return "", ""
	}
	for entityID := range matches {
		return entityID, index.entityFileByID[entityID]
	}
	return "", ""
}
