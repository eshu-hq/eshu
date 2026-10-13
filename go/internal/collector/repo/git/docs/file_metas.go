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
	metas, _ := DocumentationFileMetasWithReadFailures(repoPath, paths, commitSHA)
	return metas
}

// DocumentationFileMetasWithReadFailures is DocumentationFileMetasForPaths
// that also returns how many documentation files it skipped because their
// digest could not be read. The git snapshot marks itself degraded on a
// non-zero count, so it stamps no pre-parse identity (#7978 review F7-1).
func DocumentationFileMetasWithReadFailures(repoPath string, paths []string, commitSHA string) ([]model.ContentFileMeta, int) {
	metas := make([]model.ContentFileMeta, 0, len(paths))
	readFailures := 0
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
			readFailures++
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
	return metas, readFailures
}
