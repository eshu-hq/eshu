// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// GetFileContents returns the fixture rows at relativePaths, answered exactly
// as GetFileContent answers each path: the first fixture file at the path,
// repo-filtered, with an empty fixture RepoID matching any repository. A path
// with no fixture file is absent, a repeated path yields one row, and the
// result follows the order of the first occurrence of each path.
func (f FakePortContentStore) GetFileContents(
	ctx context.Context,
	repoID string,
	relativePaths []string,
) ([]querycontract.FileContent, error) {
	seen := make(map[string]struct{}, len(relativePaths))
	files := make([]querycontract.FileContent, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		if _, ok := seen[relativePath]; ok {
			continue
		}
		seen[relativePath] = struct{}{}
		file, err := f.GetFileContent(ctx, repoID, relativePath)
		if err != nil {
			return nil, err
		}
		if file != nil {
			files = append(files, *file)
		}
	}
	return files, nil
}
