// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// When the drain times out it prints one number: "fact residual=3". That number
// cannot distinguish work still running from work permanently blocked, and the
// gate destroys its stack on exit, so the rows behind the number are gone before
// anyone can look. A real occurrence of this cost most of a day and still did
// not establish the cause (#5717, #5875 follow-up).
//
// formatResidualBreakdown turns the rows the gate already has into a line that
// names them: how many are live, how many are deferred by a readiness gate that
// is waiting on something else, and which domains and failure classes they sit
// in. It changes no verdict — the drain still fails on residual > 0 exactly as
// before. It only makes the failure legible.

func TestResidualBreakdownNamesDeferredRowsAndTheirFailureClass(t *testing.T) {
	t.Parallel()

	rows := []residualRow{
		{Domain: "aws_cloud_runtime_drift", Status: "retrying", FailureClass: "aws_cloud_runtime_drift_state_pending", Count: 3},
	}
	got := formatResidualBreakdown(rows)

	for _, want := range []string{
		"aws_cloud_runtime_drift",
		"retrying",
		"aws_cloud_runtime_drift_state_pending",
		"3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("breakdown missing %q: %s", want, got)
		}
	}
}

// A readiness deferral and an ordinary in-flight row are different situations
// and the operator needs to tell them apart at a glance: one is the system
// working, the other is the system waiting on something that may never arrive.
func TestResidualBreakdownSeparatesDeferredFromLive(t *testing.T) {
	t.Parallel()

	rows := []residualRow{
		{Domain: "repo_dependency", Status: "pending", FailureClass: "", Count: 2},
		{Domain: "aws_cloud_runtime_drift", Status: "retrying", FailureClass: "aws_cloud_runtime_drift_state_pending", Count: 3},
	}
	got := formatResidualBreakdown(rows)

	if !strings.Contains(got, "live=2") {
		t.Errorf("breakdown does not total live rows: %s", got)
	}
	if !strings.Contains(got, "readiness-deferred=3") {
		t.Errorf("breakdown does not total readiness-deferred rows: %s", got)
	}
}

// The whole point of the split: residual made entirely of readiness deferrals is
// not the same failure as residual with live work in it. The first means the
// pipeline stopped making progress and is waiting on a precondition; the second
// means it simply needed longer. Say which one happened.
func TestResidualBreakdownFlagsAllDeferredAsNoProgress(t *testing.T) {
	t.Parallel()

	rows := []residualRow{
		{Domain: "aws_cloud_runtime_drift", Status: "retrying", FailureClass: "aws_cloud_runtime_drift_state_pending", Count: 3},
	}
	got := formatResidualBreakdown(rows)

	if !strings.Contains(got, "no live work remained") {
		t.Errorf("breakdown does not flag the all-deferred case: %s", got)
	}
}

func TestResidualBreakdownHandlesEmpty(t *testing.T) {
	t.Parallel()

	if got := formatResidualBreakdown(nil); got != "" {
		t.Errorf("formatResidualBreakdown(nil) = %q, want empty", got)
	}
}

// A dead-lettered row is neither live nor deferred; it is a terminal failure
// sitting in the residual. Counting it as live would hide it.
func TestResidualBreakdownCountsDeadLetterSeparately(t *testing.T) {
	t.Parallel()

	rows := []residualRow{
		{Domain: "supply_chain_impact", Status: "dead_letter", FailureClass: "input_invalid", Count: 1},
	}
	got := formatResidualBreakdown(rows)

	if !strings.Contains(got, "dead_letter=1") {
		t.Errorf("breakdown does not total dead-lettered rows: %s", got)
	}
}

// A dead-lettered row is a terminal failure sitting in the residual, not work
// waiting on a precondition. When both are present, saying "every residual row
// is waiting on readiness" is false and points the reader away from the real
// problem (codex/Copilot review of #5902).
func TestResidualBreakdownDoesNotClaimAllDeferredWhenDeadLettersPresent(t *testing.T) {
	t.Parallel()

	rows := []residualRow{
		{Domain: "aws_cloud_runtime_drift", Status: "retrying", FailureClass: "aws_cloud_runtime_drift_state_pending", Count: 3},
		{Domain: "supply_chain_impact", Status: "dead_letter", FailureClass: "input_invalid", Count: 1},
	}
	got := formatResidualBreakdown(rows)

	if strings.Contains(got, "no live work remained") {
		t.Errorf("claims all-deferred while dead letters are present: %s", got)
	}
	if !strings.Contains(got, "dead_letter=1") {
		t.Errorf("breakdown does not surface the dead letter: %s", got)
	}
}

// `failed` is a terminal residual state in the drain contract, so counting it as
// live work misreports a stuck pipeline as a busy one.
func TestResidualBreakdownCountsFailedAsTerminalNotLive(t *testing.T) {
	t.Parallel()

	rows := []residualRow{
		{Domain: "repo_dependency", Status: "failed", FailureClass: "input_invalid", Count: 2},
	}
	got := formatResidualBreakdown(rows)

	if strings.Contains(got, "live=2") {
		t.Errorf("counts terminal failed rows as live: %s", got)
	}
	if !strings.Contains(got, "failed=2") {
		t.Errorf("breakdown does not total failed rows separately: %s", got)
	}
}
