// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// Why this file exists (#8336).
//
// A pull-request merge-preview failure (PR #8334's registry-mirror failure on
// refs/remotes/pull/8334/merge) was published correctly on the PR head, yet
// the aggregate's own runs concluded failure 9 times filed under the main tip:
// workflow_run runs always report the default-branch head, and the row JSON
// carries no PR attribution. The await log named only workflow, job and run
// id, so nothing beside an awaited run said which tree it evaluated.
//
// This file owns the evaluated-tree half of that fix: every await prints the
// tree its runs evaluate, and every judged finding carries its owning run's
// tree into the log line. The other half — the run must not conclude failure
// for a verdict published on another head — is the terminal publisher's
// closing line in .github/workflows/required-gates.yml.

// awaitTargetTree describes the tree an await's runs evaluate, printed once
// per await. For pull_request the SHA is the PR head and the ref is what the
// runs check out (the merge preview, which resolves to its own merge commit,
// never the head SHA), so the two are named on separate sides of the line.
// For merge_group the ref and the SHA are one pair: the queue branch at the
// group commit.
func awaitTargetTree(event, ref, headSHA string) string {
	if event == eventPullRequest {
		return fmt.Sprintf("pull_request head %s; runs evaluate %s",
			headSHA, ref)
	}
	return fmt.Sprintf("merge_group %s @ %s", ref, headSHA)
}

// pullRequestMergeRef is the ref a pull_request run checks out: the PR's
// merge preview, not its head commit.
func pullRequestMergeRef(pr int) string {
	return fmt.Sprintf("refs/remotes/pull/%d/merge", pr)
}

// printAwaitHeader names the evaluated tree before the first poll, so every
// awaited run below it is attributable even when its own row carries no run.
func printAwaitHeader(out io.Writer, event, ref, headSHA string) {
	_, _ = fmt.Fprintf(out, "required-gates: awaiting %s\n", awaitTargetTree(event, ref, headSHA))
}

// findingFor builds a finding with no run attribution: a pending wait, a
// missing check, or a gate the newest run has no row for. The await header
// names the tree in that case.
func findingFor(gate resolvedRequiredGate, state string, runID int64) requiredCheckFinding {
	return requiredCheckFinding{
		WorkflowName: gate.WorkflowName,
		Job:          gate.Job,
		GateIDs:      append([]string(nil), gate.GateIDs...),
		State:        state,
		RunID:        runID,
		Event:        normalizeGateEvent(gate.Event),
	}
}

// findingForCheck builds a finding from a judged check row, carrying the
// owning run's tree so the verdict names what it evaluated.
func findingForCheck(gate resolvedRequiredGate, check checkRollup, state string) requiredCheckFinding {
	finding := findingFor(gate, state, check.RunID)
	finding.Called = check.Workflow == "Build Test" && strings.HasPrefix(check.Name, "call-")
	if check.Event != "" {
		finding.Event = check.Event
	}
	finding.HeadBranch = check.HeadBranch
	finding.HeadSHA = check.HeadSHA
	return finding
}

// normalizeGateEvent maps the original empty event to pull_request, the
// original and default aggregation mode.
func normalizeGateEvent(event string) string {
	if event == "" {
		return eventPullRequest
	}
	return event
}

// runAttribution names the judged run and the tree it belongs to, for use
// beside a finding. A run without a tree keeps today's bare shape; a row
// without a run keeps none. The short SHA is an identifier only — the await
// header carries the full head SHA.
func runAttribution(event, branch, sha string, runID int64) string {
	if runID == 0 {
		return ""
	}
	if branch == "" {
		return fmt.Sprintf(" (run %d)", runID)
	}
	if sha == "" {
		return fmt.Sprintf(" (run %d %s %s)", runID, normalizeGateEvent(event), branch)
	}
	short := sha
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf(" (run %d %s %s@%s)", runID, normalizeGateEvent(event), branch, short)
}

func formatFindings(findings []requiredCheckFinding) string {
	parts := make([]string, 0, len(findings))
	for _, finding := range findings {
		detail := finding.WorkflowName + " / " + finding.Job + " [" + strings.Join(finding.GateIDs, ",") + "]"
		if finding.State != "" {
			detail += "=" + finding.State
		}
		// The run id says which event's verdict was judged (#8053), and the
		// tree says what that run evaluated (#8336): without both, a failure
		// line cannot tell a main-tree failure from a PR-preview one. Zero
		// still means the source row carried no attribution and is omitted,
		// never printed as "run 0".
		detail += runAttribution(finding.Event, finding.HeadBranch, finding.HeadSHA, finding.RunID)
		parts = append(parts, detail)
	}
	return strings.Join(parts, "; ")
}

// awaitPRRequiredChecks waits for the selected gates' pull_request check rows
// on a PR head, read through `gh pr checks`.
func awaitPRRequiredChecks(
	ctx context.Context,
	runner ghRunner,
	repo string,
	pr int,
	headSHA string,
	required []resolvedRequiredGate,
	pollInterval time.Duration,
	out io.Writer,
) error {
	printAwaitHeader(out, eventPullRequest, pullRequestMergeRef(pr), headSHA)
	readChecks := func(ctx context.Context) ([]checkRollup, error) {
		return readPRChecksWithCalled(ctx, runner, repo, pr, headSHA, required)
	}
	return awaitRequiredChecks(ctx, runner, repo, headSHA, eventPullRequest, readChecks, required, pollInterval, out)
}
