// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Pending-status retry and no-premature-error-verdict for the required-gates
// aggregate (#8039). Run 38334797669 failed its one `gh api -X POST
// .../statuses/<sha>` write with `unexpected end of JSON input`, steps 3-7
// skipped, and the terminal step published required-gates-complete as `error`
// on a green merge group, which ejected the entry from the queue.
//
// These tests run the committed workflow's own steps with a fake `gh` that
// fails the first status write with an empty body and succeeds on the second:
// the fixed pending step stays green, and the fixed terminal step leaves a
// merge-group head on pending instead of publishing `error` before the gates
// are classified.

// seedPostFailures writes the remaining-failure counter the fake `gh` in
// runRequiredStepWithPRs reads: the next n status POSTs fail with
// `unexpected end of JSON input`, later ones succeed.
func seedPostFailures(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "post-failures")
	if err := os.WriteFile(path, []byte(strconv.Itoa(n)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// countStatusPosts counts the status-write attempts in a recorded call log.
func countStatusPosts(calls string) int {
	return strings.Count(calls, "api -X POST")
}

// requirePendingPosts asserts every recorded status write targets the
// required context on the probe head with state=pending.
func requirePendingPosts(t *testing.T, calls string, want int) {
	t.Helper()
	if got := countStatusPosts(calls); got != want {
		t.Fatalf("want %d status POST attempts, got %d; calls:\n%s", want, got, calls)
	}
	for _, call := range strings.Split(strings.TrimSpace(calls), "\n") {
		if !strings.Contains(call, "api -X POST") {
			continue
		}
		for _, fragment := range []string{
			"statuses/probe-head",
			"context=required-gates-complete",
			"state=pending",
		} {
			if !strings.Contains(call, fragment) {
				t.Fatalf("retried status write lacks %q; calls:\n%s", fragment, calls)
			}
		}
	}
}

// TestRequiredWorkflowPendingStatusRetriesTransientWriteFailure is #8039's
// core: one transient empty-body failure followed by a valid response leaves
// the pending step green, with the retry carrying the same pending status.
func TestRequiredWorkflowPendingStatusRetriesTransientWriteFailure(t *testing.T) {
	t.Parallel()

	pending := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish pending required status")
	got := runRequiredStepWithPRs(t, pending, "merge_group", `[]`, map[string]string{
		"ESHU_GH_POST_FAIL_FILE": seedPostFailures(t, 1),
	})
	if got.err != nil {
		t.Fatalf("pending step must survive one transient status-write failure: %v\n%s", got.err, got.output)
	}
	requirePendingPosts(t, got.calls, 2)
}

// TestRequiredWorkflowSingleShotPostFailsTheSameFixture is the control the
// acceptance criterion asks for: the old shape -- one `gh api` write, no
// retry -- fails the exact fixture the retry test above survives.
func TestRequiredWorkflowSingleShotPostFailsTheSameFixture(t *testing.T) {
	t.Parallel()

	singleShot := requiredWorkflowStep{Run: `gh api -X POST "repos/${GITHUB_REPOSITORY}/statuses/${HEAD_SHA}" ` +
		`-f state=pending -f context=required-gates-complete`}
	got := runRequiredStepWithPRs(t, singleShot, "merge_group", `[]`, map[string]string{
		"ESHU_GH_POST_FAIL_FILE": seedPostFailures(t, 1),
	})
	if got.err == nil {
		t.Fatal("a single-shot status write must fail the transient-failure fixture; " +
			"otherwise the fixture proves nothing about the retry")
	}
}

// TestRequiredWorkflowPendingStatusFailsWhenEveryAttemptFails pins the other
// half of the retry contract: the step still fails -- loudly -- when every
// attempt fails, so a real outage is never silently swallowed.
func TestRequiredWorkflowPendingStatusFailsWhenEveryAttemptFails(t *testing.T) {
	t.Parallel()

	pending := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish pending required status")
	got := runRequiredStepWithPRs(t, pending, "merge_group", `[]`, map[string]string{
		"ESHU_GH_POST_FAIL_FILE": seedPostFailures(t, 3),
	})
	if got.err == nil {
		t.Fatal("pending step must fail when every status-write attempt fails")
	}
	if got := countStatusPosts(got.calls); got != 3 {
		t.Fatalf("want 3 status POST attempts before giving up, got %d", got)
	}
}

// TestRequiredWorkflowTerminalLeavesMergeGroupPendingOnSetupFailure is
// #8039's second half: when the pending step never succeeded on a
// merge_group event, the terminal step must not publish `error` before the
// gates are classified. It leaves the status on the pending write (or the
// absence of one) for the next run's real verdict.
func TestRequiredWorkflowTerminalLeavesMergeGroupPendingOnSetupFailure(t *testing.T) {
	t.Parallel()

	terminal := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish terminal required status")
	// PR_DISPOSITION=active mirrors run 38334797669: the pending step
	// recorded its disposition before the status write failed, so the
	// terminal step sees a failed setup, not an unclassified source.
	got := runRequiredStepWithPRs(t, terminal, "merge_group", `[]`, map[string]string{
		"PR_DISPOSITION":  "active",
		"PENDING_OUTCOME": "failure",
		"AGGREGATE_CODE":  "",
	})
	if got.err != nil {
		t.Fatalf("terminal step must leave a merge-group head alone when setup never classified the gates: %v\n%s",
			got.err, got.output)
	}
	if strings.Contains(got.calls, "-X POST") {
		t.Fatalf("no terminal verdict may be published before the gates are classified; calls:\n%s", got.calls)
	}
}

// TestRequiredWorkflowTerminalLeavesMergeGroupPendingOnUnclassifiedGates
// covers the sibling shape the owner named inline on PR 8118: the pending
// write landed, but a later setup step (e.g. the default-branch checkout)
// failed, so the aggregate never ran and AGGREGATE_CODE is empty. The `""`
// no-verdict arm would publish `error` and eject a green group the same way
// run 38334797669 did. A merge-group head with no classified verdict must be
// left for the next queued run instead.
func TestRequiredWorkflowTerminalLeavesMergeGroupPendingOnUnclassifiedGates(t *testing.T) {
	t.Parallel()

	terminal := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish terminal required status")
	got := runRequiredStepWithPRs(t, terminal, "merge_group", `[]`, map[string]string{
		"PR_DISPOSITION":  "active",
		"PENDING_OUTCOME": "success",
		"AGGREGATE_CODE":  "",
	})
	if got.err != nil {
		t.Fatalf("terminal step must leave a merge-group head with no classified verdict alone: %v\n%s",
			got.err, got.output)
	}
	if strings.Contains(got.calls, "-X POST") {
		t.Fatalf("no terminal verdict may be published before the gates are classified; calls:\n%s", got.calls)
	}
}

// TestRequiredWorkflowTerminalStillErrorsPullRequestSetupFailure pins the
// unchanged pull_request path: there the pending step also classifies the PR,
// so its failure still fails closed with `error` rather than leaving a head
// pending forever with nothing queued behind it.
func TestRequiredWorkflowTerminalStillErrorsPullRequestSetupFailure(t *testing.T) {
	t.Parallel()

	terminal := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish terminal required status")
	const oneOpen = `[{"number":7206,"state":"open","head":{"sha":"probe-head"}}]`
	got := runRequiredStepWithPRs(t, terminal, "pull_request", oneOpen, map[string]string{
		"PR_DISPOSITION":  "active",
		"PR_NUMBER":       "7206",
		"PENDING_OUTCOME": "failure",
		"AGGREGATE_CODE":  "",
	})
	if got.err == nil {
		t.Fatal("a pull_request setup failure must still fail closed")
	}
	assertRequiredStatusCall(t, got.calls, "error")
}
