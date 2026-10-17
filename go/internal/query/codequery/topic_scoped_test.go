// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import "testing"

// TestCodeTopicIsRepoScoped pins the one shared repo-scoped predicate both the
// serial builder and the parallel partition builder gate bind-hiding on
// (#8370). A scoping-rule change must update this table, so the two paths
// cannot silently diverge.
func TestCodeTopicIsRepoScoped(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		repoID   string
		language string
		want     bool
	}{
		{name: "explicit repo without language", repoID: "repo:payments", want: true},
		{name: "corpus-wide", repoID: "", want: false},
		{name: "repo with language filter", repoID: "repo:payments", language: "go", want: false},
		{name: "blank repo", repoID: "   ", want: false},
		{name: "blank language still scoped", repoID: "repo:payments", language: "  ", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CodeTopicIsRepoScoped(CodeTopicInvestigationRequest{RepoID: tc.repoID, Language: tc.language})
			if got != tc.want {
				t.Fatalf("CodeTopicIsRepoScoped(repo=%q language=%q) = %v, want %v", tc.repoID, tc.language, got, tc.want)
			}
		})
	}
}
