// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"fmt"
	"testing"
	"time"
)

// BenchmarkGitHubOrgSelectionRequest measures building the observation
// request for an 800-repository org listing, dominated by one scope id
// derivation per listed repository.
func BenchmarkGitHubOrgSelectionRequest(b *testing.B) {
	records := make([]GitHubRepositoryRecord, 0, 800)
	for i := range 800 {
		records = append(records, GitHubRepositoryRecord{RepoID: fmt.Sprintf("acme/repo-%03d", i), GitHubID: int64(i + 1)})
	}
	config := RepoSyncConfig{ReposDir: b.TempDir(), SourceMode: "githubOrg", GithubOrg: "acme", RepoLimit: 4000}
	discovered := selectGitHubRepositoryIDs(records, nil, false)
	discovered.ListingComplete = true
	now := time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	for b.Loop() {
		if request := githubOrgSelectionRequest(config, discovered, now); len(request.Listing.Repositories) != 800 {
			b.Fatalf("listed = %d, want 800", len(request.Listing.Repositories))
		}
	}
}
