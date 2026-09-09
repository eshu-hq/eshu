// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import "strings"

// codeContentGrantAdmits mirrors the `repo_id = $n` / `repo_id = ANY($n)`
// pair the shipped SQL builders emit: an explicit repo_id anchors the scan, a
// non-empty grant list restricts it, and an empty grant list does not.
// Duplicated in package query's code_grant_test_fixtures_test.go, whose
// canonical copy is auth_scoped_code_content_grant_test.go there (it moved
// back to package query at the #6060 move since it tests root's
// content_reader_*.go filter builders).
func codeContentGrantAdmits(rowRepoID, repoID string, allowedRepositoryIDs []string) bool {
	if anchor := strings.TrimSpace(repoID); anchor != "" && rowRepoID != anchor {
		return false
	}
	if len(allowedRepositoryIDs) == 0 {
		return true
	}
	for _, id := range allowedRepositoryIDs {
		if id == rowRepoID {
			return true
		}
	}
	return false
}
