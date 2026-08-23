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

// aggregateCaseHeader opens the terminal publisher's AGGREGATE_CODE branch.
// Everything the arm locator reads starts after it, so an assignment on an
// earlier branch -- the PENDING_OUTCOME guard's own `state=error`, above the
// case -- can never be mistaken for an arm's.
const aggregateCaseHeader = `case "${AGGREGATE_CODE}" in`

// aggregateCodeArm parses the AGGREGATE_CODE case arm for one exit code into
// its statements, stopping at the arm's `;;` terminator. found is false when
// no such arm exists; err is non-nil when the arm is built from shell outside
// the grammar requiredworkflow_shell.go accepts, which callers report rather
// than judge.
//
// The arm is located STRUCTURALLY -- a line inside the case block that begins
// with `<code>)` -- rather than by searching the step for the substring
// "<code>)". The substring is defeatable, and for the cancelled-gate arm it
// was defeatable in the fail-OPEN direction (#6189 review). Ordinary prose
// produces the marker: a comment reading "the cancelled-gate outcome (13) is
// handled elsewhere" contains "13)". A locator that matched there extracted
// the region from the comment to the next `;;`, which spans the
// PENDING_OUTCOME guard's `state=error` and the `0)` arm, so "the arm must
// publish state=error" was satisfied by an assignment on another branch --
// while the real arm was deleted, or reverted to `state=failure`, underneath.
// Skipping comment lines and requiring the marker to START a line closes both.
//
// The `;;` that ends the arm is found by the scanner, not by a substring
// search: `;;` inside a quoted description is text, and truncating there
// dropped every assignment after it -- which reads as an arm that assigns
// nothing, and an arm that assigns nothing passes the still-running check.
func aggregateCodeArm(run string, code int) ([]shellStatement, bool, error) {
	header := strings.Index(run, aggregateCaseHeader)
	if header < 0 {
		return nil, false, nil
	}
	marker := fmt.Sprintf("%d)", code)
	lines := strings.Split(run[header+len(aggregateCaseHeader):], "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "esac" {
			// Past the end of the case block; anything below it is other
			// code, not an arm.
			return nil, false, nil
		}
		if strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, marker) {
			continue
		}
		statements, err := scanArmBody(append([]string{strings.TrimPrefix(trimmed, marker)}, lines[i+1:]...))
		if err != nil {
			return nil, true, fmt.Errorf("%s arm: %w", marker, err)
		}
		return statements, true, nil
	}
	return nil, false, nil
}

// scanArmBody parses an arm's lines up to its `;;` terminator, or to the
// `esac` that closes the case block when the arm has no terminator.
func scanArmBody(lines []string) ([]shellStatement, error) {
	var statements []shellStatement
	for _, line := range lines {
		if strings.TrimSpace(line) == "esac" {
			break
		}
		parsed, terminated, err := scanShellLine(line)
		if err != nil {
			return nil, err
		}
		statements = append(statements, parsed...)
		if terminated {
			break
		}
	}
	return statements, nil
}

// effectiveStateAssignment returns the value the arm actually leaves in
// `state`, and whether it assigns it at all.
//
// Bash runs an arm top to bottom and the LAST assignment is the one the `gh
// api` call below the case block reads, so `state=error; state=success`
// publishes `success`. Asking only whether the arm CONTAINS `state=error`
// accepts exactly that -- a cancelled dependency satisfying the required merge
// status (#6189 review). Only the surviving assignment is a claim about what
// the publisher does.
//
// A statement's assignments are the run of assignment words that OPENS it,
// which is bash's own rule: `state=error description=x` sets both, while
// `echo state=success` sets nothing because `echo` ends the run. Reading only
// the first word would miss the second assignment of a pair; reading every
// word would count a command's arguments as assignments.
func effectiveStateAssignment(statements []shellStatement) (string, bool) {
	value, found := "", false
	for _, statement := range statements {
		for _, word := range statement {
			name, assigned, ok := shellAssignment(word)
			if !ok {
				break
			}
			if name == "state" {
				value, found = assigned, true
			}
		}
	}
	return value, found
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
	} else {
		errs = append(errs, validateStillRunningArm(step, check)...)
	}
	errs = append(errs, validateCancelledArm(step, check)...)
	return errs
}

// validateStillRunningArm holds the #6075 half of the AGGREGATE_CODE contract:
// the still-running arm must exist, and it must not map an unfinished run back
// to state=failure.
func validateStillRunningArm(step requiredWorkflowStep, check RequiredStatusCheck) []error {
	arm, ok, err := aggregateCodeArm(step.Run, awaitExitStillRunningCode)
	if err != nil {
		return []error{unparseableArmError(check, err)}
	}
	if !ok {
		return []error{fmt.Errorf(
			"required status context %q: terminal publisher has no still-running arm (%d) in its "+
				"AGGREGATE_CODE branch; gates that have not finished must not publish a terminal status",
			check.Context, awaitExitStillRunningCode,
		)}
	}
	if state, assigned := effectiveStateAssignment(arm); assigned && state == "failure" {
		return []error{fmt.Errorf(
			"required status context %q: terminal publisher maps the still-running outcome (%d) to "+
				"state=failure; that is the collapse #6075 removed -- unfinished gates must not publish failure",
			check.Context, awaitExitStillRunningCode,
		)}
	}
	return nil
}

// unparseableArmError reports an arm the scanner declined to parse. Refusing
// to judge is the point: a validator that guesses at shell it does not model
// is the #6194 failure, where nine review rounds grew a textual bash model one
// bypass at a time without ever closing it.
func unparseableArmError(check RequiredStatusCheck, err error) error {
	return fmt.Errorf(
		"required status context %q: terminal publisher's %w; this validator will not judge an arm it "+
			"cannot parse -- keep arms to plain name=value assignments and quoted strings",
		check.Context, err,
	)
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
	arm, ok, err := aggregateCodeArm(step.Run, awaitExitGateCancelledCode)
	if err != nil {
		return []error{unparseableArmError(check, err)}
	}
	if !ok {
		return []error{fmt.Errorf(
			"required status context %q: terminal publisher has no cancelled-gate arm (%d) in its "+
				"AGGREGATE_CODE branch; a cancelled dependency would fall through to the aggregation-broke "+
				"description and send an operator hunting a red gate that does not exist",
			check.Context, awaitExitGateCancelledCode,
		)}
	}
	state, assigned := effectiveStateAssignment(arm)
	if assigned && state == "failure" {
		return []error{fmt.Errorf(
			"required status context %q: terminal publisher maps the cancelled-gate outcome (%d) to "+
				"state=failure; that is the #6189 overclaim -- a cancelled gate is not a failed gate",
			check.Context, awaitExitGateCancelledCode,
		)}
	}
	if !assigned || state != "error" {
		return []error{fmt.Errorf(
			"required status context %q: terminal publisher's cancelled-gate arm (%d) must publish "+
				"state=error so the cancellation stays visible and still blocks the merge; its effective "+
				"assignment is %q",
			check.Context, awaitExitGateCancelledCode, state,
		)}
	}
	return nil
}
