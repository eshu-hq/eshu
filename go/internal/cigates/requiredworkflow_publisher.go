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

// awaitExitStillRunningCode and awaitExitGateCancelledCode mirror
// awaitExitStillRunning and awaitExitGateCancelled in
// go/cmd/ci-gates/await_outcome.go. Duplicated rather than imported because
// those constants are unexported in package main; both pairs are asserted
// against the real source in TestStillRunningCodeMatchesAwaitContract and
// TestGateCancelledCodeMatchesAwaitContract (go/cmd/ci-gates), so they cannot
// drift apart.
const (
	awaitExitStillRunningCode  = 11
	awaitExitGateCancelledCode = 13
)

// aggregateCodeArm extracts the body of the AGGREGATE_CODE case arm for one
// exit code, up to its `;;` terminator. Returns false when no such arm exists.
func aggregateCodeArm(run string, code int) (string, bool) {
	marker := fmt.Sprintf("%d)", code)
	start := strings.Index(run, marker)
	if start < 0 {
		return "", false
	}
	rest := run[start+len(marker):]
	if end := strings.Index(rest, ";;"); end >= 0 {
		return rest[:end], true
	}
	return rest, true
}

// validateTerminalPublisher checks the terminal required-status step: that it
// targets the right context and head SHA, is cancellation-safe, and branches
// on the await exit code rather than defaulting every non-success outcome to
// failure (#6075). Split out of validateTrustedAggregator, which the added
// checks pushed past the funlen limit.
func validateTerminalPublisher(step requiredWorkflowStep, check RequiredStatusCheck) []error {
	var errs []error
	if !strings.Contains(step.Run, "-f context="+check.Context) {
		errs = append(errs, fmt.Errorf(
			"required status context %q: terminal status publisher must target the required context",
			check.Context,
		))
	}
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
	// #6075: the publisher must branch on the await exit code. Before
	// this, any non-success outcome defaulted to `failure`, so gates
	// merely still running published a red on the status branch
	// protection uses to summarize every other gate -- a red that
	// meant "look again", which is how a genuine aggregation failure
	// gets waved through and how a lander abandons a green PR.
	// Verify the branch SEMANTICS, not just that the variable is
	// mentioned (#6083 review). A publisher that keeps
	// `case "${AGGREGATE_CODE}"` but maps the still-running arm back
	// to state=failure would satisfy a substring check while
	// reintroducing the exact still-running -> failure collapse #6075
	// removed, and the error message would be claiming more than the
	// check proved.
	if !strings.Contains(step.Run, "AGGREGATE_CODE") {
		errs = append(errs, fmt.Errorf(
			"required status context %q: terminal publisher must branch on the await exit code "+
				"(AGGREGATE_CODE) rather than defaulting every non-success outcome to failure",
			check.Context,
		))
	} else if arm, ok := aggregateCodeArm(step.Run, awaitExitStillRunningCode); !ok {
		errs = append(errs, fmt.Errorf(
			"required status context %q: terminal publisher has no still-running arm (%d) in its "+
				"AGGREGATE_CODE branch; gates that have not finished must not publish a terminal status",
			check.Context, awaitExitStillRunningCode,
		))
	} else if strings.Contains(arm, "state=failure") {
		errs = append(errs, fmt.Errorf(
			"required status context %q: terminal publisher maps the still-running outcome (%d) to "+
				"state=failure; that is the collapse #6075 removed -- unfinished gates must not publish failure",
			check.Context, awaitExitStillRunningCode,
		))
	}
	errs = append(errs, validateCancelledArm(step, check)...)
	return errs
}

// validateCancelledArm is the static mirror of #6189. The aggregate used to
// classify a CANCELLED dependency gate as a failed gate and publish
// "A required gate failed" when no gate had failed, which is what teaches
// people to read a red required status as noise. A workflow-only revert of
// that arm would restore the overclaim with nothing to catch it, so the arm's
// existence and its state are asserted here alongside the #6075 contract.
func validateCancelledArm(step requiredWorkflowStep, check RequiredStatusCheck) []error {
	if !strings.Contains(step.Run, "AGGREGATE_CODE") {
		// Already reported by the caller; do not pile a second error on the
		// same root cause.
		return nil
	}
	arm, ok := aggregateCodeArm(step.Run, awaitExitGateCancelledCode)
	if !ok {
		return []error{fmt.Errorf(
			"required status context %q: terminal publisher has no cancelled-gate arm (%d) in its "+
				"AGGREGATE_CODE branch; a cancelled dependency would fall through to the aggregation-broke "+
				"description and send an operator hunting a red gate that does not exist",
			check.Context, awaitExitGateCancelledCode,
		)}
	}
	if strings.Contains(arm, "state=failure") {
		return []error{fmt.Errorf(
			"required status context %q: terminal publisher maps the cancelled-gate outcome (%d) to "+
				"state=failure; that is the #6189 overclaim -- a cancelled gate is not a failed gate",
			check.Context, awaitExitGateCancelledCode,
		)}
	}
	if !strings.Contains(arm, "state=error") {
		return []error{fmt.Errorf(
			"required status context %q: terminal publisher's cancelled-gate arm (%d) must publish "+
				"state=error so the cancellation stays visible and still blocks the merge",
			check.Context, awaitExitGateCancelledCode,
		)}
	}
	return nil
}
