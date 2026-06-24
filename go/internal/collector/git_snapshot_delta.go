// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"strings"
)

func relativePathsForSnapshotTargets(repoPath string, fileTargets []string) []string {
	relativePaths := make([]string, 0, len(fileTargets))
	for _, fileTarget := range fileTargets {
		absoluteTarget, err := filepath.Abs(fileTarget)
		if err != nil {
			continue
		}
		if resolvedTarget, resolveErr := filepath.EvalSymlinks(absoluteTarget); resolveErr == nil {
			absoluteTarget = resolvedTarget
		}
		relativePath, err := filepath.Rel(repoPath, absoluteTarget)
		if err != nil {
			continue
		}
		relativePaths = append(relativePaths, filepath.ToSlash(filepath.Clean(relativePath)))
	}
	return normalizeSnapshotRelativePaths(relativePaths)
}

func normalizeSnapshotRelativePaths(relativePaths []string) []string {
	normalized := make([]string, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		relativePath = filepath.ToSlash(filepath.Clean(relativePath))
		if relativePath == "." || relativePath == "" || relativePath == ".." ||
			strings.HasPrefix(relativePath, "../") || strings.HasPrefix(relativePath, "/") {
			continue
		}
		normalized = append(normalized, relativePath)
	}
	return sortUniquePathStrings(normalized)
}
