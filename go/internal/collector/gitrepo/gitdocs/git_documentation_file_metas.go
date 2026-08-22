// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitdocs

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gitmodel"
)

func IsNotebookDocumentationPath(filePath string) bool {
	return strings.ToLower(filepath.Ext(filePath)) == ".ipynb"
}

func DocumentationFileMetasForPaths(repoPath string, paths []string, commitSHA string) []gitmodel.ContentFileMeta {
	metas := make([]gitmodel.ContentFileMeta, 0, len(paths))
	for _, filePath := range paths {
		relativePath, err := filepath.Rel(repoPath, filePath)
		if err != nil {
			continue
		}
		relativePath = filepath.ToSlash(filepath.Clean(relativePath))
		format, ok := gitDocumentationFormatForPath(relativePath)
		if !ok {
			continue
		}
		digest, ok := gitmodel.DocumentationDigestForFile(filePath)
		if !ok {
			continue
		}
		metas = append(metas, gitmodel.ContentFileMeta{
			RelativePath: relativePath,
			Digest:       digest,
			Language:     format.Language,
			ArtifactType: "documentation",
			CommitSHA:    commitSHA,
		})
	}
	return metas
}
