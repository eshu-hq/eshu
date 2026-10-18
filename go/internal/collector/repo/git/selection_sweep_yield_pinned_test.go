// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"testing"
	"time"
)

// stubRepositoryReindexWatermarks pins one repository reindex request for
// every selection cycle: watermark rows persist until satisfied, so the pin
// stays at the head of the walk on resumed cycles too.
type stubRepositoryReindexWatermarks struct {
	pinned map[string]time.Time
}

func (s stubRepositoryReindexWatermarks) RepositoryReindexWatermarks(context.Context, time.Time) (map[string]time.Time, error) {
	return s.pinned, nil
}

// TestNativeSelectorSweepYieldResumeSkipsVisitedPinnedRepos is the #8512 P2
// follow-up regression: with a repository reindex pinned at the head of the
// walk and a trigger queued at every probe, resumed walks must not re-fetch
// the already-visited pinned repository. One logical sweep still fetches
// each repository exactly once.
//
// The fetch count reads resolver.fullScopeIDs, not scopeIDs: decideForScope
// consults FullReconcileState for every visited repository on every path,
// while the LastProjectedCommitSHA baseline lookup runs only off the forced
// path, so a forced pinned visit is invisible in scopeIDs.
func TestNativeSelectorSweepYieldResumeSkipsVisitedPinnedRepos(t *testing.T) {
	reposDir, repos := sweepYieldWorld(t, 5)
	config := reconcileTestConfig(reposDir)
	now := time.Date(2026, 10, 14, 6, 37, 0, 0, time.UTC)
	resolver := freshBaselineResolver(now)
	pinnedScopeID := gitScopeIDForRepositoryID(config, repos[0])
	if pinnedScopeID == "" {
		t.Fatal("scope ID of the pinned repository is empty")
	}
	selector := NativeRepositorySelector{
		Config: config,
		Now:    func() time.Time { return now },
		DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
			return RepositorySelection{RepositoryIDs: repos}, nil
		},
		Logger:           discardLogger(),
		BaselineResolver: resolver,
		RepositoryReindexWatermark: stubRepositoryReindexWatermarks{
			pinned: map[string]time.Time{pinnedScopeID: now},
		},
		SweepYieldProbe:      &stubQueuedTriggerProbe{queued: true},
		SweepYieldCheckEvery: 2,
	}

	for range 3 {
		if _, err := selector.SelectRepositories(context.Background()); err != nil {
			t.Fatalf("SelectRepositories() error = %v", err)
		}
	}
	fetches := resolver.fullScopeIDs
	if got := len(fetches); got != 5 {
		t.Fatalf("logical sweep with a pin fetched %d repositories, want exactly 5 (no re-fetch on resume)", got)
	}
	if got := distinctStrings(fetches); got != 5 {
		t.Fatalf("logical sweep with a pin fetched %d distinct repositories, want all 5", got)
	}
	pinnedVisits := 0
	for _, scopeID := range fetches {
		if scopeID == pinnedScopeID {
			pinnedVisits++
		}
	}
	if pinnedVisits != 1 {
		t.Fatalf("pinned repository fetched %d times, want exactly once", pinnedVisits)
	}
}
