// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"context"
	"strings"
)

// NonBlankRepoFilePaths reports the fixture files at relativePaths whose
// content is not blank under strings.TrimSpace, repo-filtered like
// GetFileContent. A path with no fixture file is absent, as it is in the real
// reader.
func (f FakePortContentStore) NonBlankRepoFilePaths(
	_ context.Context,
	repoID string,
	relativePaths []string,
) ([]string, error) {
	wanted := make(map[string]struct{}, len(relativePaths))
	for _, relativePath := range relativePaths {
		wanted[relativePath] = struct{}{}
	}
	seen := make(map[string]struct{}, len(relativePaths))
	nonBlank := make([]string, 0, len(relativePaths))
	for _, file := range f.RepoFiles {
		if file.RepoID != "" && repoID != "" && file.RepoID != repoID {
			continue
		}
		if _, ok := wanted[file.RelativePath]; !ok {
			continue
		}
		if _, ok := seen[file.RelativePath]; ok {
			continue
		}
		seen[file.RelativePath] = struct{}{}
		if strings.TrimSpace(file.Content) != "" {
			nonBlank = append(nonBlank, file.RelativePath)
		}
	}
	return nonBlank, nil
}
