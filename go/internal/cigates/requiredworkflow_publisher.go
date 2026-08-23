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
	errs = append(errs, validatePublishedBindings(step, check)...)
	errs = append(errs, validatePublishEpilogue(step, check)...)
	return errs
}

// publishCommandMarker opens the `gh api` call that posts the status.
// Everything between the case block's `esac` and this line still runs on the
// way to the publish.
const publishCommandMarker = "gh api -X POST"

// validatePublishEpilogue closes the half of the consuming end that binding
// the ARGUMENT left open (#6218 review round 3). validatePublishedBindings
// pins the text of `-f state=`; it says nothing about the variable that text
// reads. One `state=success` on its own line between `esac` and the `gh api`
// call satisfies every arm check and every argument check above it, publishes
// `success` for an exit code that means a gate genuinely failed, and also
// passes the step's own closing `[[ "${state}" == "success" ]]` so the job
// goes green -- and since the ruleset requires this context, that is a merge
// on a red gate.
//
// The authoritative guard for this is dynamic, not static:
// publishedRequiredStatus in go/cmd/ci-gates executes the publisher from the
// PENDING_OUTCOME guard through the line before the `gh api` call under real
// bash, so the per-code tests observe the value that actually reaches the
// publish, whatever spelling put it there. This static mirror repeats the
// narrow part of that in the registry gate, so the two must be defeated
// together rather than one at a time.
//
// Every word is examined, not just a leading assignment run: a prefix
// assignment on the publish line's predecessor still overwrites `state` for
// the arm's purposes here, and over-reporting in this region costs a false red
// on a shape the publisher does not use.
func validatePublishEpilogue(step requiredWorkflowStep, check RequiredStatusCheck) []error {
	lines := strings.Split(step.Run, "\n")
	closing, publish := -1, -1
	for i, line := range lines {
		if closing < 0 {
			if strings.TrimSpace(line) == "esac" {
				closing = i
			}
			continue
		}
		if strings.Contains(line, publishCommandMarker) {
			publish = i
			break
		}
	}
	if closing < 0 || publish < 0 {
		// A publisher with no case block, or none of this `gh api` shape, is
		// already reported above, and publishedRequiredStatus fails loud on
		// the same two markers. Do not stack a second error on one cause.
		return nil
	}
	var errs []error
	for _, line := range lines[closing+1 : publish] {
		errs = append(errs, epilogueLineErrors(line, check)...)
	}
	return errs
}

// epilogueLineErrors reports one line between the case block and the publish:
// unparseable, or assigning one of the two names the publish reads.
func epilogueLineErrors(line string, check RequiredStatusCheck) []error {
	statements, _, err := scanShellLine(line)
	if err != nil {
		return []error{fmt.Errorf(
			"required status context %q: terminal publisher has a line between its AGGREGATE_CODE case "+
				"block and the status API call that is %w; this validator will not clear a publish path "+
				"it cannot read",
			check.Context, err,
		)}
	}
	var errs []error
	for _, statement := range statements {
		for _, word := range statement {
			name, value, ok := shellAssignment(word)
			if !ok || (name != "state" && name != "description") {
				continue
			}
			errs = append(errs, fmt.Errorf(
				"required status context %q: terminal publisher assigns %s=%q between its AGGREGATE_CODE "+
					"case block and the status API call, overwriting whatever the arm chose on the way to "+
					"the publish; every outcome would post that one value and the whole branch above it "+
					"would be decorative",
				check.Context, name, value,
			))
		}
	}
	return errs
}

// validateStillRunningArm holds the #6075 half of the AGGREGATE_CODE contract:
// the still-running arm must exist, and it must not map an unfinished run back
// to state=failure.
func validateStillRunningArm(step requiredWorkflowStep, check RequiredStatusCheck) []error {
	state, assigned, ok, err := armStateAssignment(step.Run, awaitExitStillRunningCode)
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
	if assigned && state == "failure" {
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
	state, assigned, ok, err := armStateAssignment(step.Run, awaitExitGateCancelledCode)
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

// validatePublishedBindings is the ARGUMENT half of the consuming end of the
// AGGREGATE_CODE contract (#6218 review); validatePublishEpilogue above is the
// other half, and neither is sufficient alone. Every arm check asserts what the case block
// ASSIGNS; nothing asserted that the `gh api` call below it POSTS what the
// block assigned. With `-f state=success` hard-coded there the branch decides
// nothing: every head gets `success`, a genuinely failed gate included, and
// the arm validators stay green while the status they protect lies.
//
// The description carries the same exposure one step over. Hard-code it and
// "A required gate failed" comes back for a head where nothing failed, which
// is the #6189 overclaim in a different argument. It is required to be BOUND
// rather than required to be PRESENT: no validator here depends on a
// description existing, and dropping it costs an operator a sentence rather
// than making a wrong status look right.
func validatePublishedBindings(step requiredWorkflowStep, check RequiredStatusCheck) []error {
	var errs []error
	states := publishedArgumentValues(step.Run, "state")
	if len(states) == 0 {
		errs = append(errs, fmt.Errorf(
			"required status context %q: terminal publisher passes no -f state= argument to the status API; "+
				"nothing carries the AGGREGATE_CODE branch's verdict to the head SHA",
			check.Context,
		))
	}
	for _, value := range states {
		if !referencesShellVariable(value, "state") {
			errs = append(errs, fmt.Errorf(
				"required status context %q: terminal publisher posts a literal -f state= argument (beginning %q) "+
					"instead of the ${state} its AGGREGATE_CODE branch assigns; every outcome would publish that "+
					"one status and the whole branch would be decorative",
				check.Context, value,
			))
		}
	}
	for _, value := range publishedArgumentValues(step.Run, "description") {
		if !referencesShellVariable(value, "description") {
			errs = append(errs, fmt.Errorf(
				"required status context %q: terminal publisher posts a literal -f description= argument "+
					"(beginning %q) instead of the ${description} its AGGREGATE_CODE branch assigns; every "+
					"outcome would carry that one sentence, which is how a cancelled gate gets described as "+
					"a failed one",
				check.Context, value,
			))
		}
	}
	return errs
}

// publishedArgumentValues returns the leading non-blank run of every
// `-f <name>=` argument in the step, in order. For an unquoted value that is
// the whole argv entry; for a quoted one it is only the first word.
//
// The truncation is deliberate, not a second attempt to parse shell --
// requiredworkflow_shell.go records why this package does not do that -- and
// it errs in the safe direction. A value that mentions `${state}` only after a
// blank reads as a literal and gets REPORTED, so the failure mode is a false
// red on a shape this workflow does not use, never a literal slipping past.
func publishedArgumentValues(run, name string) []string {
	flag := "-f " + name + "="
	var values []string
	for i := 0; ; {
		found := strings.Index(run[i:], flag)
		if found < 0 {
			return values
		}
		start := i + found + len(flag)
		end := start
		for end < len(run) && run[end] != ' ' && run[end] != '\t' && run[end] != '\n' {
			end++
		}
		values = append(values, run[start:end])
		i = end
	}
}

// referencesShellVariable reports whether an argument takes its value from the
// named shell variable rather than a literal. Both spellings the workflow
// could use count; the trailing check keeps `$statement` from passing as
// `$state`.
func referencesShellVariable(value, name string) bool {
	if strings.Contains(value, "${"+name+"}") {
		return true
	}
	plain := "$" + name
	at := strings.Index(value, plain)
	if at < 0 {
		return false
	}
	rest := value[at+len(plain):]
	if rest == "" {
		return true
	}
	return !isShellNameByte(rest[0])
}
