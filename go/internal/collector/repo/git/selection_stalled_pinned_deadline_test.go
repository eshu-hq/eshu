// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package git

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/deadline"
)

// TestRealGitStalledPinnedRefFetchesAreBounded stalls the pinned-ref fetch
// against a listener that never answers. gitFetchPinnedRef always runs two
// fetches in sequence, one for refs/heads and one for refs/tags, so each is
// killed at its own deadline: the counter holds exactly {fetch:2}, the call
// takes about two deadlines (not one stalled command plus a hang), the helper
// is gone, and no lock file is left. Only the fetch deadline is short; the
// other two are an hour, so no other limit can be what ends the call.
func TestRealGitStalledPinnedRefFetchesAreBounded(t *testing.T) {
	isolateGitEnv(t)
	server := startTCPServer(t, 0)
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo-a")
	newRealRepo(t, repoPath, server.URL)
	instruments, reader := newSyncIsolationInstruments(t)
	ctx := deadline.WithMetrics(context.Background(), instruments)
	config := realGitConfig(reposDir, deadline.Limits{Resolve: time.Hour, Fetch: realGitDeadline, Clone: time.Hour})

	errCh := make(chan error, 1)
	start := time.Now()
	go func() {
		_, _, err := gitFetchPinnedRef(ctx, config, repoPath, "release", "", nil, gitSyncLogEvent{})
		errCh <- err
	}()
	server.waitAccepted(t)
	var err error
	select {
	case err = <-errCh:
	case <-time.After(2*realGitDeadline + realGitReturnBound):
		t.Fatal("the pinned-ref fetches did not end within two deadlines plus the guard; a pinned fetch ran without its deadline")
	}
	elapsed := time.Since(start)

	var deadlineErr *deadline.Error
	if !errors.As(err, &deadlineErr) || deadlineErr.Operation != deadline.Fetch || deadlineErr.Limit != realGitDeadline {
		t.Fatalf("error = %v, want a fetch *deadline.Error with a %s limit", err, realGitDeadline)
	}
	if elapsed < 2*realGitDeadline {
		t.Fatalf("returned after %s, before two sequential %s deadlines; the branch and tag fetches did not both run", elapsed, realGitDeadline)
	}
	if got := counterByOperation(t, reader, deadlineCounterName); got[deadline.Fetch] != 2 || len(got) != 1 {
		t.Fatalf("%s = %v, want {fetch:2}: one kill for the branch fetch and one for the tag fetch", deadlineCounterName, got)
	}
	assertNoGitProcessesFor(t, strings.TrimSuffix(server.URL, "/x.git"))
	assertNoLockFiles(t, repoPath)
}
