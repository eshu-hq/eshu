// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
)

func IsNotebookDocumentationPath(filePath string) bool {
	return strings.ToLower(filepath.Ext(filePath)) == ".ipynb"
}

func DocumentationFileMetasForPaths(repoPath string, paths []string, commitSHA string) []model.ContentFileMeta {
	metas := make([]model.ContentFileMeta, 0, len(paths))
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
		digest, ok := model.DocumentationDigestForFile(filePath)
		if !ok {
			continue
		}
		metas = append(metas, model.ContentFileMeta{
			RelativePath: relativePath,
			Digest:       digest,
			Language:     format.Language,
			ArtifactType: "documentation",
			CommitSHA:    commitSHA,
		})
	}
	return metas
}
