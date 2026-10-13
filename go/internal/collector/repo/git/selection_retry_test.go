// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/deadline"
)

// retryTestLimits is a fast limits value for the retry cells: a short
// deadline, a short backoff, and the default two attempts.
func retryTestLimits(budget int) deadline.Limits {
	return deadline.Limits{
		Resolve: time.Minute, Fetch: time.Minute, Clone: 750 * time.Millisecond,
		RetryAttempts: 2, RetryBackoff: 20 * time.Millisecond, RetrySweepBudget: budget,
	}
}

func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var rec map[string]any
		if line != "" && json.Unmarshal([]byte(line), &rec) == nil {
			out = append(out, rec)
		}
	}
	return out
}

func recordsNamed(records []map[string]any, msg string) []map[string]any {
	var out []map[string]any
	for _, rec := range records {
		if rec["msg"] == msg {
			out = append(out, rec)
		}
	}
	return out
}

// writeFlakyGit installs a fake git whose first fetch of each repository fails
// with a resolver error and whose later fetches succeed. State lives in the
// returned directory, one file per repository.
func writeFlakyGit(t *testing.T) (stateDir string) {
	t.Helper()
	binDir, stateDir := t.TempDir(), t.TempDir()
	script := `#!/bin/sh
case "$*" in
	*"symbolic-ref refs/remotes/origin/HEAD"*) printf "refs/remotes/origin/main\n" ;;
	*"fetch --progress"*)
		key=$(basename "$2")
		n=$(cat "` + stateDir + `/$key" 2>/dev/null || echo 0)
		n=$((n+1))
		echo "$n" > "` + stateDir + `/$key"
		if [ "$n" = 1 ]; then
			echo "fatal: unable to access 'https://github.com/org/$key.git/': Could not resolve host: github.com" >&2
			exit 128
		fi
		;;
	*"rev-parse refs/remotes/origin/main"*) printf "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n" ;;
	*"ls-remote"*)
		printf "ref: refs/heads/main\tHEAD\n"
		printf "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\trefs/heads/main\n"
		;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stateDir
}

// TestSweepRetryBudgetFallsThroughToTodaysBehaviour is the per-sweep cap cell:
// with a budget of one retry, repository A's resolver failure is retried and
// recovers, and repository B's identical failure is final on its first attempt,
// logged once with its class, counted once, and the budget exhaustion is
// counted. The sweep is not slowed by B's retry that the budget refused.
func TestSweepRetryBudgetFallsThroughToTodaysBehaviour(t *testing.T) {
	repoIDs := []string{"github/org/repo-a", "github/org/repo-b"}
	reposDir := t.TempDir()
	pathA := makeManagedRepo(t, reposDir, repoIDs[0])
	makeManagedRepo(t, reposDir, repoIDs[1])
	writeFlakyGit(t)
	instruments, reader := newSyncIsolationInstruments(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	config := syncIsolationTestConfig(reposDir)
	config.GitDeadlines = retryTestLimits(1)

	synced, err := syncGitRepositoriesWithLogger(context.Background(), config, repoIDs, logger,
		gitDeltaBaseline{Instruments: instruments})
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}
	if len(synced.SelectedRepoPaths) != 1 || synced.SelectedRepoPaths[0] != pathA {
		t.Fatalf("SelectedRepoPaths = %v, want only %q (A recovered on its retry, B fell through)", synced.SelectedRepoPaths, pathA)
	}
	if got := counterByLabel(t, reader, "eshu_dp_git_command_retry_budget_exhausted_total", "scope"); got["sweep"] != 1 || len(got) != 1 {
		t.Fatalf("retry_budget_exhausted_total = %v, want {sweep:1}", got)
	}
	if got := counterByLabel(t, reader, "eshu_dp_git_command_retries_total", "operation"); got["fetch"] != 1 || len(got) != 1 {
		t.Fatalf("retries_total = %v, want {fetch:1}", got)
	}
	if got := counterByOperation(t, reader, "eshu_dp_git_repo_sync_failures_total"); got["fetch"] != 1 || len(got) != 1 {
		t.Fatalf("eshu_dp_git_repo_sync_failures_total = %v, want {fetch:1}: one failure per repository, not per attempt", got)
	}

	records := logRecords(t, &logs)
	failed := recordsNamed(records, "git repository sync failed")
	if len(failed) != 1 {
		t.Fatalf("'git repository sync failed' lines = %d, want 1 (B only):\n%s", len(failed), logs.String())
	}
	if failed[0]["failure_class"] != "dns_resolve_failed" || failed[0]["attempts"] != float64(1) ||
		failed[0]["repository_id"] != "github/org/repo-b" {
		t.Fatalf("failed line = %v, want failure_class dns_resolve_failed, attempts 1, repository_id github/org/repo-b", failed[0])
	}
	if got := recordsNamed(records, "git command recovered on attempt 2"); len(got) != 1 || got[0]["repository_id"] != "github/org/repo-a" {
		t.Fatalf("recovery lines = %v, want one for repository A", got)
	}
	var exhaustedLine map[string]any
	for _, rec := range recordsNamed(records, "git command attempt failed") {
		if rec["retry_budget_exhausted"] == true {
			exhaustedLine = rec
		}
	}
	if exhaustedLine == nil || exhaustedLine["will_retry"] != false || exhaustedLine["repository_id"] != "github/org/repo-b" ||
		exhaustedLine["host"] != "github.com" || exhaustedLine["operation"] != "fetch" {
		t.Fatalf("no WARN line for B names the exhausted budget with will_retry=false: %v", exhaustedLine)
	}
}

// TestKilledCloneRemovesPartialDirectoryBeforeRetry is the killed-clone cell. A
// clone killed at its deadline leaves a partial checkout (a .git directory with
// a pack fragment) and git refuses to clone into a non-empty directory. The
// retry must find the path empty: the shim records whether the directory
// existed when the second attempt started and fails the attempt if it did.
func TestKilledCloneRemovesPartialDirectoryBeforeRetry(t *testing.T) {
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo-a")
	binDir, stateDir := t.TempDir(), t.TempDir()
	script := `#!/bin/sh
case "$*" in
	*"clone"*)
		for last; do :; done
		n=$(cat "` + stateDir + `/n" 2>/dev/null || echo 0)
		n=$((n+1))
		echo "$n" > "` + stateDir + `/n"
		if [ -e "$last" ]; then
			echo "present at attempt $n" > "` + stateDir + `/leak"
			echo "fatal: destination path already exists and is not an empty directory." >&2
			exit 128
		fi
		mkdir -p "$last/.git/objects/pack"
		echo partial > "$last/.git/objects/pack/tmp_pack_X"
		if [ "$n" = 1 ]; then
			sleep 30 &
			wait
		fi
		;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	instruments, reader := newSyncIsolationInstruments(t)
	ctx := deadline.WithMetrics(context.Background(), instruments)
	config := syncIsolationTestConfig(reposDir)
	config.GitDeadlines = retryTestLimits(0)

	type cloneResult struct {
		ok  bool
		err error
	}
	result := within(t, 15*time.Second, "cloneRepository", func() cloneResult {
		ok, cloneErr := cloneRepository(ctx, config, "github/org/repo-a", repoPath, "", nil, gitSyncLogEvent{})
		return cloneResult{ok, cloneErr}
	})
	cloned, err := result.ok, result.err
	if err != nil || !cloned {
		t.Fatalf("cloneRepository() = %v, %v; want success on the second attempt", cloned, err)
	}
	if leak, readErr := os.ReadFile(filepath.Join(stateDir, "leak")); readErr == nil {
		t.Fatalf("the retry started with the killed clone's directory still present: %s", leak)
	}
	if n, _ := os.ReadFile(filepath.Join(stateDir, "n")); strings.TrimSpace(string(n)) != "2" {
		t.Fatalf("clone attempts = %q, want 2", n)
	}
	if got := counterByOperation(t, reader, deadlineCounterName); got["clone"] != 1 || len(got) != 1 {
		t.Fatalf("%s = %v, want {clone:1}: the first attempt was killed once", deadlineCounterName, got)
	}
	if got := counterByLabel(t, reader, "eshu_dp_git_command_retries_total", "operation"); got["clone"] != 1 {
		t.Fatalf("retries_total = %v, want {clone:1}", got)
	}
	var deadlineErr *deadline.Error
	if errors.As(err, &deadlineErr) {
		t.Fatalf("a recovered clone returned a deadline error: %v", err)
	}
}

// counterByLabel sums an int64 counter by one label.
func counterByLabel(t *testing.T, reader *metric.ManualReader, name, label string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				value, _ := dp.Attributes.Value(attribute.Key(label))
				out[value.AsString()] += dp.Value
			}
		}
	}
	return out
}

// writeAttemptGit installs a fake git that counts its invocations in a state
// file and runs the first shell branch on invocation 1 and the second on every
// later one. The branches are shell text run for the git subcommand under test.
func writeAttemptGit(t *testing.T, subcommand, firstAttempt, laterAttempts string) {
	t.Helper()
	binDir, stateDir := t.TempDir(), t.TempDir()
	script := `#!/bin/sh
case "$*" in
	*"` + subcommand + `"*)
		n=$(cat "` + stateDir + `/n" 2>/dev/null || echo 0)
		n=$((n+1))
		echo "$n" > "` + stateDir + `/n"
		if [ "$n" = 1 ]; then
` + firstAttempt + `
		else
` + laterAttempts + `
		fi
		;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

const resolverStderrShell = `echo "fatal: unable to access 'https://github.com/org/repo-a.git/': Could not resolve host: github.com" >&2; exit 128`

// TestRetriedCommandStartsEachAttemptWithEmptyBuffers proves the runner's
// shared output buffers are reset per attempt. An ls-remote killed after it
// wrote a ref line must not leave that line in front of the retry's output, and
// a retry that fails silently must be classified on its own empty stderr, not
// on the first attempt's resolver text.
func TestRetriedCommandStartsEachAttemptWithEmptyBuffers(t *testing.T) {
	limits := retryTestLimits(0)
	limits.Resolve = 400 * time.Millisecond
	repoPath := t.TempDir()
	reposDir := filepath.Dir(repoPath)
	config := syncIsolationTestConfig(reposDir)
	config.GitDeadlines = limits
	ref := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\trefs/heads/main"

	t.Run("stdout of a killed attempt is dropped", func(t *testing.T) {
		writeAttemptGit(t, "ls-remote",
			`printf "`+ref+`\n"; sleep 30 & wait`,
			`printf "`+ref+`\n"; exit 0`)
		out, err := gitRunRemote(context.Background(), deadline.LsRemote, repoPath, config, "", "ls-remote", "origin")
		if err != nil {
			t.Fatalf("gitRunRemote() error = %v, want recovery on attempt 2", err)
		}
		if want := strings.ReplaceAll(ref, `\t`, "\t"); out != want {
			t.Fatalf("ls-remote output = %q, want one copy of %q: attempt 1's stdout leaked into attempt 2", out, want)
		}
	})

	t.Run("stderr of the first attempt does not classify the second", func(t *testing.T) {
		writeAttemptGit(t, "fetch", resolverStderrShell, `exit 1`)
		_, err := gitRunRemote(context.Background(), deadline.Fetch, repoPath, config, "", "fetch", "origin")
		var failure *deadline.Failure
		if !errors.As(err, &failure) || failure.Attempts != 2 || failure.Class != deadline.ClassOther {
			t.Fatalf("error = %v (%+v), want a git_sync_failure Failure after 2 attempts: attempt 2 wrote nothing", err, failure)
		}
	})
}

// TestRetriedCloneStartsEachAttemptWithAnEmptyStderr is the clone half of the
// same rule: cloneRepository keeps one stderr buffer across attempts, and the
// final failure must be classified on the last attempt's text only.
func TestRetriedCloneStartsEachAttemptWithAnEmptyStderr(t *testing.T) {
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo-a")
	writeAttemptGit(t, "clone", resolverStderrShell, `exit 1`)
	config := syncIsolationTestConfig(reposDir)
	config.GitDeadlines = retryTestLimits(0)

	_, err := cloneRepository(context.Background(), config, "github/org/repo-a", repoPath, "", nil, gitSyncLogEvent{})
	var failure *deadline.Failure
	if !errors.As(err, &failure) || failure.Attempts != 2 || failure.Class != deadline.ClassOther {
		t.Fatalf("error = %v (%+v), want a git_sync_failure Failure after 2 attempts: attempt 2 wrote nothing", err, failure)
	}
}
