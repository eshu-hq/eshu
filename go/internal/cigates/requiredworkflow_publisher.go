// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"strings"
)

// Publisher-contract helpers for the required-gates terminal status (#6075),
// split out of requiredworkflow.go to keep that file under the 500-line cap --
// same pattern as requiredworkflow_concurrency.go and
// requiredworkflow_triggers.go.
//
// This file holds the checks that are genuinely facts about the workflow YAML:
// which step runs, under what condition, with which head SHA. What the step
// POSTS is not a YAML fact and is no longer read out of the step's text --
// requiredworkflow_publishrun.go runs the step and
// requiredworkflow_publishcontract.go asserts what it posted. See those files
// for why (#6218 review rounds 2-4).

// awaitExit*Code mirror the exit codes in go/cmd/ci-gates/await_outcome.go.
// Duplicated rather than imported because those constants are unexported in
// package main; every pair is asserted against the real source in
// TestStillRunningCodeMatchesAwaitContract,
// TestGateCancelledCodeMatchesAwaitContract and
// TestPublisherMirrorsEveryAwaitExitCode (go/cmd/ci-gates), so they cannot
// drift apart.
const (
	awaitExitPassedCode        = 0
	awaitExitGateFailedCode    = 10
	awaitExitStillRunningCode  = 11
	awaitExitBrokenCode        = 12
	awaitExitGateCancelledCode = 13
)

// validateTerminalPublisher checks the terminal required-status step: that it
// is cancellation-safe, targets the right head SHA, and -- the part that
// matters most -- that what it actually posts for each await exit code is
// what that code means (#6075, #6189).
func validateTerminalPublisher(step requiredWorkflowStep, check RequiredStatusCheck) []error {
	var errs []error
	condition := strings.TrimSpace(step.If)
	hasExpressionDelimiters := strings.HasPrefix(condition, "${{") && strings.HasSuffix(condition, "}}")
	if hasExpressionDelimiters {
		condition = strings.TrimSpace(condition[3 : len(condition)-2])
	}
	if !hasExpressionDelimiters || condition != "!cancelled()" {
		errs = append(errs, fmt.Errorf(
			"required status context %q: terminal status publisher must use !cancelled() to run after failure without publishing on cancellation",
			check.Context,
		))
	}
	if !strings.Contains(step.Env["HEAD_SHA"], "workflow_run.head_sha") {
		errs = append(errs, fmt.Errorf(
			"required status context %q: terminal status must target github.event.workflow_run.head_sha",
			check.Context,
		))
	}
	return append(errs, validatePublishedOutcomes(step, check)...)
}
