// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitProgressWriterLogsSanitizedProgress(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	event := gitSyncLogEvent{
		Operation:       "clone",
		RepositoryID:    "example/private-service",
		RepositoryIndex: 2,
		RepositoryCount: 5,
		StartedAt:       time.Date(2026, time.May, 18, 12, 0, 0, 0, time.UTC),
	}
	writer := newGitProgressWriter(context.Background(), logger, event, nil)

	_, err := writer.Write([]byte("remote: Counting objects: 42% (5/12)\r"))
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	_, err = writer.Write([]byte("fatal: Authentication failed for 'https://user:secret@example.com/org/repo.git'\n"))
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	got := logs.String()
	if !strings.Contains(got, `"msg":"git repository sync progress"`) {
		t.Fatalf("logs = %s, want git progress message", got)
	}
	if !strings.Contains(got, `"operation":"clone"`) {
		t.Fatalf("logs = %s, want operation field", got)
	}
	if !strings.Contains(got, `"repository_index":2`) || !strings.Contains(got, `"repository_count":5`) {
		t.Fatalf("logs = %s, want repository ordinal fields", got)
	}
	if strings.Contains(got, "secret") || strings.Contains(got, "user:") {
		t.Fatalf("logs = %s, want credential material redacted", got)
	}
	if !strings.Contains(got, "https://[redacted]@example.com/org/repo.git") {
		t.Fatalf("logs = %s, want sanitized URL", got)
	}
}

func TestGitProgressWriterBuffersSplitLinesBeforeLogging(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	event := gitSyncLogEvent{
		Operation:       "fetch",
		RepositoryID:    "example/private-service",
		RepositoryIndex: 1,
		RepositoryCount: 1,
		StartedAt:       time.Date(2026, time.May, 18, 12, 0, 0, 0, time.UTC),
	}
	writer := newGitProgressWriter(context.Background(), logger, event, nil)

	if _, err := writer.Write([]byte("fatal: Authentication failed for 'https://user:")); err != nil {
		t.Fatalf("first Write() error = %v, want nil", err)
	}
	if got := logs.String(); got != "" {
		t.Fatalf("logs before line delimiter = %s, want empty", got)
	}
	if _, err := writer.Write([]byte("secret@example.com/org/repo.git'\n")); err != nil {
		t.Fatalf("second Write() error = %v, want nil", err)
	}

	got := logs.String()
	if strings.Contains(got, "secret") || strings.Contains(got, "user:") {
		t.Fatalf("logs = %s, want split credential material redacted", got)
	}
	if !strings.Contains(got, "https://[redacted]@example.com/org/repo.git") {
		t.Fatalf("logs = %s, want sanitized reconstructed URL", got)
	}
}

func TestGitProgressWriterFlushesTrailingProgressLine(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	event := gitSyncLogEvent{
		Operation:       "clone",
		RepositoryID:    "example/private-service",
		RepositoryIndex: 1,
		RepositoryCount: 1,
		StartedAt:       time.Date(2026, time.May, 18, 12, 0, 0, 0, time.UTC),
	}
	writer := newGitProgressWriter(context.Background(), logger, event, nil)

	if _, err := writer.Write([]byte("remote: Resolving deltas: 100% (2/2)")); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if logs.String() != "" {
		t.Fatalf("logs before Flush() = %s, want empty", logs.String())
	}
	writer.Flush()
	if got := logs.String(); !strings.Contains(got, "Resolving deltas") {
		t.Fatalf("logs after Flush() = %s, want trailing progress line", got)
	}
}

func TestGitSyncLogEventForUsesUnknownForMalformedProvider(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"example/private-service":        "github",
		"github/example/private-service": "github",
		"gitlab/example/private-service": "gitlab",
		"private-service":                "unknown",
		"other/example/private-service":  "unknown",
	}
	for repoID, want := range cases {
		t.Run(repoID, func(t *testing.T) {
			event := gitSyncLogEventFor(repoID, 1, 1)
			if event.ProviderKind != want {
				t.Fatalf("ProviderKind = %q, want %q", event.ProviderKind, want)
			}
		})
	}
}

func TestGitProgressLineIsTerminalRequiresTerminalPrefix(t *testing.T) {
	t.Parallel()

	if gitProgressLineIsTerminal("remote: 0 errors found") {
		t.Fatal("remote progress with non-terminal errors text classified terminal")
	}
	if !gitProgressLineIsTerminal("error: authentication failed") {
		t.Fatal("error prefix was not classified terminal")
	}
	if !gitProgressLineIsTerminal("fatal: Authentication failed") {
		t.Fatal("fatal prefix was not classified terminal")
	}
}

func TestCloneRepositoryLogsVisibleHostedSyncLifecycle(t *testing.T) {
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	if err := os.WriteFile(fakeGit, []byte(`#!/bin/sh
last=""
for arg in "$@"; do
	last="$arg"
done
mkdir -p "$last/.git"
printf "remote: Counting objects: 100%% (12/12)\r" >&2
printf "done\n" >&2
`), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	repoPath := filepath.Join(t.TempDir(), "example", "private-service")
	event := gitSyncLogEventFor("example/private-service", 1, 1)
	cloned, err := cloneRepository(
		context.Background(),
		RepoSyncConfig{CloneDepth: 1, GitAuthMethod: "none"},
		"example/private-service",
		repoPath,
		"",
		logger,
		event,
	)
	if err != nil {
		t.Fatalf("cloneRepository() error = %v, want nil", err)
	}
	if !cloned {
		t.Fatal("cloneRepository() cloned = false, want true")
	}

	got := logs.String()
	for _, want := range []string{
		`"msg":"git repository sync started"`,
		`"msg":"git repository sync progress"`,
		`"msg":"git repository sync completed"`,
		`"repository_id":"example/private-service"`,
		`"repository_index":1`,
		`"repository_count":1`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("logs = %s, want %s", got, want)
		}
	}
	if strings.Contains(got, repoPath) {
		t.Fatalf("logs = %s, want no full local checkout path", got)
	}
}

func TestUpdateRepositoryStartedLogIncludesResolvedBranch(t *testing.T) {
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	if err := os.WriteFile(fakeGit, []byte(`#!/bin/sh
case "$*" in
	*"symbolic-ref refs/remotes/origin/HEAD"*)
		printf "refs/remotes/origin/main\n"
		;;
	*"fetch --progress"*)
		printf "remote: Counting objects: 100%% (1/1)\r" >&2
		;;
	*"rev-parse HEAD"*|*"rev-parse refs/remotes/origin/main"*)
		printf "abc123\n"
		;;
	*)
		;;
esac
`), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	event := gitSyncLogEventFor("example/private-service", 1, 1)
	updated, _, err := updateRepository(
		context.Background(),
		RepoSyncConfig{CloneDepth: 1, GitAuthMethod: "none"},
		t.TempDir(),
		"",
		logger,
		event,
		"abc123",
		nil,
	)
	if err != nil {
		t.Fatalf("updateRepository() error = %v, want nil", err)
	}
	if updated {
		t.Fatal("updateRepository() updated = true, want false for equal refs")
	}

	got := logs.String()
	if !strings.Contains(got, `"msg":"git repository sync started"`) {
		t.Fatalf("logs = %s, want started log", got)
	}
	if !strings.Contains(got, `"branch":"main"`) {
		t.Fatalf("logs = %s, want started log to include branch", got)
	}
}

func TestUpdateRepositoryParsesSymrefHeadBranchFromLsRemote(t *testing.T) {
	binDir := t.TempDir()
	fetchMarker := filepath.Join(binDir, "fetch.ok")
	fakeGit := filepath.Join(binDir, "git")
	if err := os.WriteFile(fakeGit, []byte(`#!/bin/sh
case "$*" in
	*"symbolic-ref refs/remotes/origin/HEAD"*)
		exit 1
		;;
	*"ls-remote --symref origin HEAD"*)
		printf "ref: refs/heads/main\tHEAD\nabc123\tHEAD\n"
		;;
	*"fetch --progress origin +refs/heads/main:refs/remotes/origin/main"*)
		touch "`+fetchMarker+`"
		;;
	*"rev-parse HEAD"*|*"rev-parse refs/remotes/origin/main"*)
		printf "abc123\n"
		;;
	*)
		;;
esac
`), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	updated, _, err := updateRepository(
		context.Background(),
		RepoSyncConfig{CloneDepth: 1, GitAuthMethod: "none"},
		t.TempDir(),
		"",
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		gitSyncLogEventFor("example/private-service", 1, 1),
		"abc123",
		nil,
	)
	if err != nil {
		t.Fatalf("updateRepository() error = %v, want nil", err)
	}
	if updated {
		t.Fatal("updateRepository() updated = true, want false for equal refs")
	}
	if _, err := os.Stat(fetchMarker); err != nil {
		t.Fatalf("fetch marker missing: %v", err)
	}
}

func TestUpdateRepositoryRecoversOldShallowLockAndRetriesFetch(t *testing.T) {
	repoPath := t.TempDir()
	lockPath := filepath.Join(repoPath, ".git", "shallow.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write shallow.lock: %v", err)
	}
	staleTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
		t.Fatalf("age shallow.lock: %v", err)
	}

	binDir := t.TempDir()
	attemptsPath := filepath.Join(binDir, "fetch-attempts")
	fakeGit := filepath.Join(binDir, "git")
	if err := os.WriteFile(fakeGit, []byte(`#!/bin/sh
case "$*" in
	*"symbolic-ref refs/remotes/origin/HEAD"*)
		printf "refs/remotes/origin/main\n"
		;;
	*"fetch --progress"*)
		attempts=0
		if [ -f "`+attemptsPath+`" ]; then
			attempts=$(cat "`+attemptsPath+`")
		fi
		attempts=$((attempts + 1))
		printf "%s" "$attempts" > "`+attemptsPath+`"
		if [ "$attempts" -eq 1 ]; then
			printf "fatal: Unable to create '`+lockPath+`': File exists.\n" >&2
			exit 128
		fi
		;;
	*"rev-parse HEAD"*|*"rev-parse refs/remotes/origin/main"*)
		printf "abc123\n"
		;;
	*)
		;;
esac
`), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	updated, _, err := updateRepository(
		context.Background(),
		RepoSyncConfig{CloneDepth: 1, GitAuthMethod: "none"},
		repoPath,
		"",
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		gitSyncLogEventFor("example/private-service", 1, 1),
		"abc123",
		nil,
	)
	if err != nil {
		t.Fatalf("updateRepository() error = %v, want nil", err)
	}
	if updated {
		t.Fatal("updateRepository() updated = true, want false for equal refs")
	}
	attemptsBytes, err := os.ReadFile(attemptsPath)
	if err != nil {
		t.Fatalf("read attempts: %v", err)
	}
	if got, want := strings.TrimSpace(string(attemptsBytes)), "2"; got != want {
		t.Fatalf("fetch attempts = %s, want %s", got, want)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("shallow.lock stat error = %v, want removed", err)
	}
}
