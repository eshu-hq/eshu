// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
)

// IsEmpty reports whether the Git update carried no file-level delta metadata.
func (d GitSyncDelta) IsEmpty() bool {
	return len(d.ChangedFileTargets) == 0 && len(d.DeletedRelativePaths) == 0
}

func gitDiffDelta(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
	oldRef string,
	newRef string,
) (GitSyncDelta, error) {
	output, err := gitRun(
		ctx,
		repoPath,
		config,
		token,
		"diff",
		"--name-status",
		"-z",
		"--find-renames",
		oldRef,
		newRef,
	)
	if err != nil {
		return GitSyncDelta{}, err
	}
	return parseGitDiffNameStatusDelta(repoPath, output), nil
}

func parseGitDiffNameStatusDelta(repoPath string, output string) GitSyncDelta {
	fields := strings.Split(output, "\x00")
	changed := make([]string, 0)
	deleted := make([]string, 0)
	for i := 0; i < len(fields); {
		status := strings.TrimSpace(fields[i])
		i++
		if status == "" {
			continue
		}
		if i >= len(fields) {
			break
		}
		oldPath := normalizeGitDeltaRelativePath(fields[i])
		i++
		if oldPath == "" {
			if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
				i++
			}
			continue
		}
		switch status[0] {
		case 'D':
			deleted = append(deleted, oldPath)
		case 'R':
			deleted = append(deleted, oldPath)
			if i >= len(fields) {
				continue
			}
			newPath := normalizeGitDeltaRelativePath(fields[i])
			i++
			if newPath != "" {
				changed = append(changed, filepath.Join(repoPath, filepath.FromSlash(newPath)))
			}
		case 'C':
			if i >= len(fields) {
				continue
			}
			newPath := normalizeGitDeltaRelativePath(fields[i])
			i++
			if newPath != "" {
				changed = append(changed, filepath.Join(repoPath, filepath.FromSlash(newPath)))
			}
		default:
			changed = append(changed, filepath.Join(repoPath, filepath.FromSlash(oldPath)))
		}
	}
	return GitSyncDelta{
		ChangedFileTargets:   sortUniquePathStrings(changed),
		DeletedRelativePaths: sortUniquePathStrings(deleted),
	}
}

func normalizeGitDeltaRelativePath(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	if path == "." || path == "" || strings.HasPrefix(path, "../") || path == ".." {
		return ""
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, ".git/") || path == ".git" {
		return ""
	}
	return path
}

func sortUniquePathStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
