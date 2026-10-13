// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package git

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/deadline"
)

// warnRecords returns the JSON log records with msg == message.
func warnRecords(t *testing.T, logs *bytes.Buffer, message string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var rec map[string]any
		if line != "" && json.Unmarshal([]byte(line), &rec) == nil && rec["msg"] == message {
			out = append(out, rec)
		}
	}
	return out
}

// TestRealGitStalledPinnedRefFetchesAreBounded runs createRefWorktrees, the
// production caller of the pinned-ref fetch, against a listener that never
// answers. gitFetchPinnedRef always runs two fetches in sequence, one for
// refs/heads and one for refs/tags, so each is killed at its own deadline: the
// counter holds exactly {fetch:2}, the call takes about two deadlines (not one
// stalled command plus a hang), the helper is gone, and no lock file is left.
// The skip warning carries the deadline fields, and the cycle continues (no
// error, no entry). Only the fetch deadline is short; the other two are an
// hour, so no other limit can be what ends the call.
func TestRealGitStalledPinnedRefFetchesAreBounded(t *testing.T) {
	isolateGitEnv(t)
	server := startTCPServer(t, 0)
	reposDir := t.TempDir()
	repoID := "github/org/repo-a"
	repoPath := filepath.Join(reposDir, "github", "org", "repo-a")
	newRealRepo(t, repoPath, server.URL)
	instruments, reader := newSyncIsolationInstruments(t)
	ctx := deadline.WithMetrics(context.Background(), instruments)
	config := realGitConfig(reposDir, deadline.Limits{Resolve: time.Hour, Fetch: realGitDeadline, Clone: time.Hour})
	config.PinnedRefsByRepoID = map[string][]string{repoID: {"release"}}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	type result struct {
		entries []RefWorktreeEntry
		err     error
	}
	resCh := make(chan result, 1)
	start := time.Now()
	go func() {
		entries, _, err := createRefWorktrees(ctx, config, repoPath, repoID, "", logger, gitSyncLogEvent{}, 0, 0)
		resCh <- result{entries, err}
	}()
	server.waitAccepted(t)
	var res result
	select {
	case res = <-resCh:
	case <-time.After(2*realGitDeadline + realGitReturnBound):
		t.Fatal("the pinned-ref fetches did not end within two deadlines plus the guard; a pinned fetch ran without its deadline")
	}
	elapsed := time.Since(start)

	if res.err != nil || len(res.entries) != 0 {
		t.Fatalf("createRefWorktrees() = %v, %v; want no entries and no error (the cycle continues)", res.entries, res.err)
	}
	if elapsed < 2*realGitDeadline {
		t.Fatalf("returned after %s, before two sequential %s deadlines; the branch and tag fetches did not both run", elapsed, realGitDeadline)
	}
	if got := counterByOperation(t, reader, deadlineCounterName); got[deadline.Fetch] != 2 || len(got) != 1 {
		t.Fatalf("%s = %v, want {fetch:2}: one kill for the branch fetch and one for the tag fetch", deadlineCounterName, got)
	}
	assertNoGitProcessesFor(t, strings.TrimSuffix(server.URL, "/x.git"))
	assertNoLockFiles(t, repoPath)

	warns := warnRecords(t, &logs, "failed to fetch pinned ref; skipping")
	if len(warns) != 1 {
		t.Fatalf("got %d skip warnings, want 1:\n%s", len(warns), logs.String())
	}
	w := warns[0]
	if w["level"] != "WARN" || w["repo_id"] != repoID || w["ref"] != "release" ||
		w["failure_class"] != "deadline_exceeded" || w["deadline_operation"] != "fetch" ||
		w["deadline_seconds"] != float64(2) || w["operation"] != "fetch" {
		t.Fatalf("skip warning = %v, want WARN with repo_id, ref release, failure_class deadline_exceeded, deadline_operation fetch, deadline_seconds 2, operation fetch", w)
	}
	if secs, _ := w["elapsed_seconds"].(float64); secs < 2 {
		t.Fatalf("elapsed_seconds = %v, want at least the 2s deadline", w["elapsed_seconds"])
	}
	if text, _ := w["error"].(string); !strings.Contains(text, "exceeded the 2s deadline after") {
		t.Fatalf("error = %q, want it to name the deadline and the elapsed time", text)
	}
}

// TestPinnedRefFetchFailureWarningKeepsItsShape: a pinned-ref fetch that fails
// for any reason other than a deadline logs the same skip warning it logged
// before, with exactly those keys and no deadline fields.
func TestPinnedRefFetchFailureWarningKeepsItsShape(t *testing.T) {
	binDir := t.TempDir()
	script := `#!/bin/sh
case "$*" in
	*"symbolic-ref refs/remotes/origin/HEAD"*) printf "refs/remotes/origin/main\n" ;;
	*" fetch "*) echo "fatal: couldn't find remote ref" 1>&2; exit 128 ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	reposDir := t.TempDir()
	repoID := "github/org/repo-a"
	repoPath := makeManagedRepo(t, reposDir, repoID)
	config := syncIsolationTestConfig(reposDir)
	config.PinnedRefsByRepoID = map[string][]string{repoID: {"release"}}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	entries, _, err := createRefWorktrees(context.Background(), config, repoPath, repoID, "", logger, gitSyncLogEvent{}, 0, 0)
	if err != nil || len(entries) != 0 {
		t.Fatalf("createRefWorktrees() = %v, %v; want no entries and no error", entries, err)
	}
	warns := warnRecords(t, &logs, "failed to fetch pinned ref; skipping")
	if len(warns) != 1 {
		t.Fatalf("got %d skip warnings, want 1:\n%s", len(warns), logs.String())
	}
	var keys []string
	for k := range warns[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if want := "error,level,msg,ref,repo_id,time"; strings.Join(keys, ",") != want {
		t.Fatalf("skip warning keys = %v, want exactly %s", keys, want)
	}
}
