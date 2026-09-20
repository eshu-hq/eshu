// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
)

// commitSHAByRelativePath indexes a snapshot's per-file commit SHAs by relative
// path. It reads RepositorySnapshot, so it belongs with the snapshot types rather
// than in the observability emitter it used to share a file with -- keeping it
// there would have forced observability to import git and closed an import cycle.

func commitSHAByRelativePath(repoPath string, snapshot *RepositorySnapshot) map[string]string {
	result := make(map[string]string, len(snapshot.ContentFileMetas)+len(snapshot.ContentFiles))
	for _, meta := range snapshot.ContentFileMetas {
		if strings.TrimSpace(meta.CommitSHA) != "" {
			result[meta.RelativePath] = meta.CommitSHA
		}
	}
	for _, file := range snapshot.ContentFiles {
		if strings.TrimSpace(file.CommitSHA) != "" {
			result[file.RelativePath] = file.CommitSHA
		}
	}
	for _, fileData := range snapshot.FileData {
		if revision := model.PayloadString(fileData, "commit_sha", "source_revision"); revision != "" {
			relativePath := model.RepositoryRelativePath(repoPath, model.PayloadPath(fileData, "path"))
			result[relativePath] = revision
		}
	}
	return result
}
