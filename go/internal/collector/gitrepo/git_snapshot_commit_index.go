// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitrepo

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gitmodel"
)

// commitSHAByRelativePath indexes a snapshot's per-file commit SHAs by relative
// path. It reads RepositorySnapshot, so it belongs with the snapshot types rather
// than in the observability emitter it used to share a file with -- keeping it
// there would have forced gitobs to import gitrepo and closed an import cycle.

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
		if revision := gitmodel.PayloadString(fileData, "commit_sha", "source_revision"); revision != "" {
			relativePath := gitmodel.RepositoryRelativePath(repoPath, gitmodel.PayloadPath(fileData, "path"))
			result[relativePath] = revision
		}
	}
	return result
}
