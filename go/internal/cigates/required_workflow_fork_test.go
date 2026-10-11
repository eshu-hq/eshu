// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"strings"
	"testing"
)

// forkPR is #7818 as the pull request list returns it, trimmed to the fields
// the workflow and the stub read. Its head lives in smash-og/eshu, a fork, so
// commits/<sha>/pulls omitted it and the publisher treated it as closed (#7950).
const forkPR = `{"number":7818,"state":"open","head":{"sha":"probe-head","repo":{"full_name":"smash-og/eshu","fork":true}},"base":{"ref":"main"}}`

// sameRepoPR shares forkPR's head but lives in the base repository.
const sameRepoPR = `{"number":7206,"state":"open","head":{"sha":"probe-head","repo":{"full_name":"eshu-hq/required-gates-probe","fork":false}},"base":{"ref":"main"}}`

// otherPR is open on a different head. Listed first, it puts the PR that owns
// probe-head on the second page.
const otherPR = `{"number":7300,"state":"open","head":{"sha":"other-head","repo":{"full_name":"eshu-hq/required-gates-probe","fork":false}},"base":{"ref":"main"}}`

func TestRequiredWorkflowPendingFindsPRPastFirstPage(t *testing.T) {
	pending := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish pending required status")
	got := runRequiredStepWithPRs(t, pending, "pull_request", "["+otherPR+","+forkPR+"]", nil)
	if got.err != nil {
		t.Fatalf("pending step failed: %v\n%s", got.err, got.output)
	}
	if !strings.Contains(got.outputs, "disposition=active") || !strings.Contains(got.outputs, "number=7818") {
		t.Fatalf("a PR on the second page must be classified active, outputs: %q", got.outputs)
	}
	assertRequiredStatusCall(t, got.calls, "pending")
}

func TestRequiredWorkflowPendingClassifiesForkPRActive(t *testing.T) {
	pending := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish pending required status")
	got := runRequiredStepWithPRs(t, pending, "pull_request", "["+forkPR+"]", nil)
	if got.err != nil {
		t.Fatalf("fork PR pending step failed: %v\n%s", got.err, got.output)
	}
	if !strings.Contains(got.outputs, "disposition=active") || !strings.Contains(got.outputs, "number=7818") {
		t.Fatalf("an open fork PR must be classified active as #7818, outputs: %q", got.outputs)
	}
	assertRequiredStatusCall(t, got.calls, "pending")
}

func TestRequiredWorkflowPendingForkSharingHeadIsAmbiguous(t *testing.T) {
	pending := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish pending required status")
	got := runRequiredStepWithPRs(t, pending, "pull_request", "["+forkPR+","+sameRepoPR+"]", nil)
	if got.err != nil {
		t.Fatalf("pending step failed: %v\n%s", got.err, got.output)
	}
	if !strings.Contains(got.outputs, "disposition=ambiguous") {
		t.Fatalf("a fork PR and a same-repo PR sharing a head must be ambiguous, outputs: %q", got.outputs)
	}
	assertRequiredStatusCall(t, got.calls, "pending")
}

// A PR opened between two page requests repeats the last PR of one page at the
// top of the next. That is one owner, not an ambiguous head.
func TestRequiredWorkflowPendingCountsARepeatedPROnce(t *testing.T) {
	pending := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish pending required status")
	got := runRequiredStepWithPRs(t, pending, "pull_request", "["+sameRepoPR+","+sameRepoPR+"]", nil)
	if got.err != nil {
		t.Fatalf("pending step failed: %v\n%s", got.err, got.output)
	}
	if !strings.Contains(got.outputs, "disposition=active") || !strings.Contains(got.outputs, "number=7206") {
		t.Fatalf("a PR listed on two pages must resolve once, outputs: %q", got.outputs)
	}
}

func TestRequiredWorkflowTerminalPublishesForkPRVerdict(t *testing.T) {
	terminal := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish terminal required status")
	got := runRequiredStepWithPRs(t, terminal, "pull_request", "["+forkPR+"]", map[string]string{
		"PR_DISPOSITION": "active", "PR_NUMBER": "7818", "AGGREGATE_CODE": "0",
	})
	if got.err != nil {
		t.Fatalf("fork PR terminal step failed: %v\n%s", got.err, got.output)
	}
	assertRequiredStatusCall(t, got.calls, "success")
}

func TestRequiredWorkflowTerminalRejectsForkSharingHead(t *testing.T) {
	terminal := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish terminal required status")
	got := runRequiredStepWithPRs(t, terminal, "pull_request", "["+forkPR+","+sameRepoPR+"]", map[string]string{
		"PR_DISPOSITION": "active", "PR_NUMBER": "7206", "AGGREGATE_CODE": "0",
	})
	if got.err == nil {
		t.Fatal("a head shared with a fork PR must fail closed")
	}
	assertRequiredStatusCall(t, got.calls, "error")
}

func TestRequiredWorkflowTerminalFindsPRPastFirstPage(t *testing.T) {
	terminal := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish terminal required status")
	got := runRequiredStepWithPRs(t, terminal, "pull_request", "["+otherPR+","+forkPR+"]", map[string]string{
		"PR_DISPOSITION": "active", "PR_NUMBER": "7818", "AGGREGATE_CODE": "0",
	})
	if got.err != nil {
		t.Fatalf("terminal step failed: %v\n%s", got.err, got.output)
	}
	assertRequiredStatusCall(t, got.calls, "success")
}

func TestRequiredWorkflowTerminalCountsARepeatedPROnce(t *testing.T) {
	terminal := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish terminal required status")
	got := runRequiredStepWithPRs(t, terminal, "pull_request", "["+sameRepoPR+","+sameRepoPR+"]", map[string]string{
		"PR_DISPOSITION": "active", "PR_NUMBER": "7206", "AGGREGATE_CODE": "0",
	})
	if got.err != nil {
		t.Fatalf("a PR listed on two pages must resolve once: %v\n%s", got.err, got.output)
	}
	assertRequiredStatusCall(t, got.calls, "success")
}
