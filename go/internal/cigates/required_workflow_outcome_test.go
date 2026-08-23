// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"strings"
	"testing"
)

// TestCheckRequiredStatusWorkflows_RequiresOutcomeBranchedPublisher is the
// static mirror of #6075. The publisher used to default to `failure` for every
// non-success await outcome, so "a gate failed", "gates are still running",
// and "aggregation broke" all landed as the same red on the head SHA -- on the
// one status the repository ruleset uses to summarize every other gate.
//
// A workflow-only fix can be reverted with nothing to catch it, so the
// contract is asserted here: the terminal publisher must branch on the await
// exit code rather than treating any non-success as a gate failure.
func TestCheckRequiredStatusWorkflows_RequiresOutcomeBranchedPublisher(t *testing.T) {
	t.Parallel()

	// Collapse the branching back to the pre-#6075 shape: one unconditional
	// state=failure default, no reference to the classified exit code.
	body := strings.Replace(trustedRequiredWorkflow, `          case "${AGGREGATE_CODE}" in
            0) state=success ;;
            10) state=failure ;;
            11) exit 0 ;;
            13) state=error ;;
            *) state=error ;;
          esac
`, "          state=failure\n", 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a publisher that defaults every non-success outcome to failure must be rejected")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "await exit code") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an await-exit-code branching error, got %v", errs)
	}
}

// TestCheckRequiredStatusWorkflows_RejectsCancelledGateMappedToFailure is the
// static mirror of #6189. The aggregate published "A required gate failed" for
// heads where eleven dependency gates were CANCELLED and none had failed. The
// Go classification is only half the contract; the other half is this case
// arm, and a workflow-only revert of it would restore the overclaim with
// nothing to catch it.
func TestCheckRequiredStatusWorkflows_RejectsCancelledGateMappedToFailure(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow, "            13) state=error ;;\n", "            13) state=failure ;;\n", 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a publisher mapping the cancelled-gate outcome to failure must be rejected")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "cancelled-gate outcome") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a cancelled-gate overclaim error, got %v", errs)
	}
}

// TestCheckRequiredStatusWorkflows_RequiresCancelledGateArm catches deletion
// rather than inversion: with no `13)` arm at all, a cancelled dependency
// falls through to the `*)` arm and is described as the aggregator breaking,
// which is a different -- and wrong -- 3 AM investigation.
func TestCheckRequiredStatusWorkflows_RequiresCancelledGateArm(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow, "            13) state=error ;;\n", "", 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a publisher with no cancelled-gate arm must be rejected")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "no cancelled-gate arm") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a missing cancelled-gate arm error, got %v", errs)
	}
}

// TestCheckRequiredStatusWorkflows_RequiresCancelledGateArmToPublishError
// covers the third way the arm can be wrong: present, not mapped to failure,
// and still not publishing the one state that keeps the cancellation visible
// and the merge blocked. The fixture uses `state=success` because that is the
// worst version of it -- a cancelled dependency waved through as a pass.
//
// `state=pending` would exercise the same branch but is a poor fixture here:
// putting the literal `state=pending` in the terminal step makes the
// pending-publisher checks latch onto that step, so the fixture also returns
// "pending status publisher must be unconditional", "pending invalidation must
// be the publisher job's first step", and "publisher must post pending before
// await" -- three errors that have nothing to do with the branch under test.
// `state=success` returns exactly one error, the one this test is about.
//
// This is defence in depth rather than the only guard: an arm publishing
// anything but `error` also reds TestAwaitAllCancelledDependenciesDoNotPublishFailure
// in cmd/ci-gates, which executes the real workflow's case block. It is here
// because the two tests above leave this branch of validateCancelledArm the
// only one nothing exercises.
func TestCheckRequiredStatusWorkflows_RequiresCancelledGateArmToPublishError(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow, "            13) state=error ;;\n", "            13) state=success ;;\n", 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a cancelled-gate arm that publishes neither error nor failure must be rejected")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "must publish state=error") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a cancelled-gate arm state=error error, got %v", errs)
	}
}

// fixtureCaseBlock is the terminal publisher's case block as the shared
// fixture spells it. The locator attacks below rebuild it, so they assert it
// is still there rather than silently replacing nothing.
const fixtureCaseBlock = `          case "${AGGREGATE_CODE}" in
            0) state=success ;;
            10) state=failure ;;
            11) exit 0 ;;
            13) state=error ;;
            *) state=error ;;
          esac
`

// realShapedPublisher rebuilds the fixture's terminal step in the shape the
// real .github/workflows/required-gates.yml actually has: optional leading
// prose, then a PENDING_OUTCOME guard that assigns state=error on its own
// branch, and only then the AGGREGATE_CODE case.
//
// That guard is what makes these attacks reachable, and the shared fixture --
// which has neither comments nor a guard -- cannot express them. A locator
// that finds the arm by searching the whole step for the substring "13)" can
// match inside a COMMENT, and the region it extracts from there to the first
// ";;" already contains the guard's `state=error`. Every "this arm must
// publish state=error" assertion is then satisfied by an assignment on a
// branch that has nothing to do with a cancelled gate, so the arm itself can
// be deleted or inverted with nothing to catch it.
func realShapedPublisher(t *testing.T, lead, arm string) string {
	t.Helper()
	if !strings.Contains(trustedRequiredWorkflow, fixtureCaseBlock) {
		t.Fatal("the fixture's case block moved; these attacks would be replacing nothing")
	}
	replacement := lead + `          if [[ "${PENDING_OUTCOME}" != "success" ]]; then
            state=error
            description='Required gate aggregation could not start'
          else
            case "${AGGREGATE_CODE}" in
              0) state=success ;;
              10) state=failure ;;
              11) exit 0 ;;
` + arm + `              *) state=error ;;
            esac
          fi
`
	body := strings.Replace(trustedRequiredWorkflow, fixtureCaseBlock, replacement, 1)
	if body == trustedRequiredWorkflow {
		t.Fatal("the replacement did not change the fixture")
	}
	return body
}

// requireErrorContaining fails unless some error names the given text.
func requireErrorContaining(t *testing.T, errs []error, want string) {
	t.Helper()
	for _, err := range errs {
		if strings.Contains(err.Error(), want) {
			return
		}
	}
	t.Fatalf("expected an error containing %q, got %v", want, errs)
}

// TestCheckRequiredStatusWorkflows_AcceptsTheRealShapedPublisher is the
// positive control for the three attacks below. Without it a validator that
// rejected everything would pass all of them.
func TestCheckRequiredStatusWorkflows_AcceptsTheRealShapedPublisher(t *testing.T) {
	t.Parallel()

	body := realShapedPublisher(t, "", "              13) state=error ;;\n")
	if errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry()); len(errs) != 0 {
		t.Fatalf("the publisher shape this repository actually ships must validate, got %v", errs)
	}
}

// TestCheckRequiredStatusWorkflows_RejectsACancelledArmOverwrittenAfterAssignment
// is codex's finding on this PR. Bash runs a case arm top to bottom and the
// LAST assignment survives, so `state=error; state=success` publishes
// `success` -- a cancelled dependency satisfying the required merge status. A
// validator that asks only whether the arm's text CONTAINS `state=error` says
// yes to it.
func TestCheckRequiredStatusWorkflows_RejectsACancelledArmOverwrittenAfterAssignment(t *testing.T) {
	t.Parallel()

	body := realShapedPublisher(t, "", "              13) state=error; state=success ;;\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("an arm whose effective assignment is state=success must be rejected; it lets a cancelled dependency pass the required status")
	}
	requireErrorContaining(t, errs, "must publish")
}

// TestCheckRequiredStatusWorkflows_RejectsADeletedArmBehindAComment deletes the
// arm outright and leaves behind only prose that happens to contain "(13)".
// Nothing then maps exit 13, so a cancelled dependency falls through to the
// `*)` arm and is described as the aggregator breaking -- a different, and
// wrong, 3 AM investigation.
func TestCheckRequiredStatusWorkflows_RejectsADeletedArmBehindAComment(t *testing.T) {
	t.Parallel()

	body := realShapedPublisher(t, "          # the cancelled-gate outcome (13) is handled elsewhere now\n", "")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("prose mentioning (13) is not a case arm; with no 13) arm nothing maps the cancelled-gate outcome")
	}
	requireErrorContaining(t, errs, "no cancelled-gate arm")
}

// TestCheckRequiredStatusWorkflows_RejectsAFailureArmBehindAComment is the
// worst composition of the two: a comment supplies the "13)" the locator looks
// for, and a real arm underneath it restores the exact #6189 overclaim.
func TestCheckRequiredStatusWorkflows_RejectsAFailureArmBehindAComment(t *testing.T) {
	t.Parallel()

	body := realShapedPublisher(t,
		"          # The cancelled-gate arm (13) below is the honest mapping\n",
		"              13) state=failure ;;\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a comment naming (13) must not shield the arm below it from the overclaim check")
	}
	requireErrorContaining(t, errs, "cancelled-gate outcome")
}

// stillRunningArmReplaced swaps the still-running arm out of the real-shaped
// publisher, asserting the swap applied so an attack cannot pass by replacing
// nothing.
func stillRunningArmReplaced(t *testing.T, arm string) string {
	t.Helper()
	body := realShapedPublisher(t, "", "              13) state=error ;;\n")
	mutated := strings.Replace(body, "              11) exit 0 ;;\n", arm, 1)
	if mutated == body {
		t.Fatal("the still-running arm moved; this attack would be replacing nothing")
	}
	return mutated
}

// TestCheckRequiredStatusWorkflows_RejectsACancelledArmFailureBehindAQuotedDescription
// is the quoting half of codex's last-wins finding. Bash does not treat a
// `state=` token inside a quoted string as an assignment, so a description
// that merely MENTIONS state=error says nothing about what the arm publishes.
// A validator that reads the mention as the arm's effective assignment accepts
// the exact #6189 overclaim it exists to reject.
func TestCheckRequiredStatusWorkflows_RejectsACancelledArmFailureBehindAQuotedDescription(t *testing.T) {
	t.Parallel()

	body := realShapedPublisher(t, "",
		"              13) state=failure; description='cancelled: publishes state=error and blocks' ;;\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a description quoting state=error must not answer for an arm that assigns state=failure")
	}
	requireErrorContaining(t, errs, "cancelled-gate outcome")
}

// TestCheckRequiredStatusWorkflows_RejectsAStillRunningArmFailureBehindAQuotedDescription
// is the same attack against the #6075 arm: unfinished gates publishing
// `failure` is the collapse that trains people to ignore the one status the
// ruleset summarizes every other gate with.
func TestCheckRequiredStatusWorkflows_RejectsAStillRunningArmFailureBehindAQuotedDescription(t *testing.T) {
	t.Parallel()

	body := stillRunningArmReplaced(t,
		"              11) state=failure; description='not state=error, gates still running' ;;\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a description quoting state=error must not answer for a still-running arm that assigns state=failure")
	}
	requireErrorContaining(t, errs, "still-running outcome")
}

// TestCheckRequiredStatusWorkflows_RejectsAStillRunningArmFailureAfterAQuotedHash
// covers the other half of the same root cause. A `#` inside a quoted string
// is not a comment, so cutting the line there drops the assignment that
// follows it -- and an arm the validator reads as assigning nothing passes the
// still-running check.
func TestCheckRequiredStatusWorkflows_RejectsAStillRunningArmFailureAfterAQuotedHash(t *testing.T) {
	t.Parallel()

	body := stillRunningArmReplaced(t,
		"              11) description='see #6218'; state=failure ;;\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a `#` inside quotes is not a comment; the state=failure after it is still the arm's assignment")
	}
	requireErrorContaining(t, errs, "still-running outcome")
}

// TestCheckRequiredStatusWorkflows_RefusesToJudgeAnUnparseableArm pins the
// design choice behind requiredworkflow_shell.go: an arm built from shell this
// validator does not model is a loud error, never a permissive default. The
// alternative -- guessing -- is how #6194 spent nine rounds growing a textual
// bash model one bypass at a time.
func TestCheckRequiredStatusWorkflows_RefusesToJudgeAnUnparseableArm(t *testing.T) {
	t.Parallel()

	for name, arm := range map[string]string{
		"command substitution": "              13) state=$(echo error) ;;\n",
		"unterminated quote":   "              13) state='error ;;\n",
		"backslash escape":     "              13) state=err\\or ;;\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := realShapedPublisher(t, "", arm)
			errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
			if len(errs) == 0 {
				t.Fatal("an arm this validator cannot parse must be reported, not judged")
			}
			requireErrorContaining(t, errs, "outside the shell shapes")
		})
	}
}

// TestCheckRequiredStatusWorkflows_RejectsAPublishedStateLiteral is the
// consuming half of the whole #6075/#6189 contract. Everything above asserts
// what the case block ASSIGNS; if the `gh api` call below it posts a literal,
// the branch is decorative and every head gets the same status whatever the
// gates did.
func TestCheckRequiredStatusWorkflows_RejectsAPublishedStateLiteral(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow, `-f state="${state}"`, "-f state=success", 1)
	if body == trustedRequiredWorkflow {
		t.Fatal("the terminal publisher's -f state= argument moved; this attack would be replacing nothing")
	}
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a publisher that posts a literal state must be rejected; the AGGREGATE_CODE branch would decide nothing")
	}
	requireErrorContaining(t, errs, "literal -f state=")
}

// TestCheckRequiredStatusWorkflows_RejectsAPublishedDescriptionLiteral covers
// the same exposure on the operator-facing half: a hard-coded description
// sends every outcome the same sentence, so "A required gate failed" comes
// back for a head where nothing failed -- #6189 in a different place.
func TestCheckRequiredStatusWorkflows_RejectsAPublishedDescriptionLiteral(t *testing.T) {
	t.Parallel()

	// Anchored on the terminal publisher's own argument list: the pending
	// publisher posts to the same context, so an unanchored replace would
	// mutate the wrong step and prove nothing about this one.
	body := strings.Replace(trustedRequiredWorkflow,
		`-f state="${state}" -f context=required-gates-complete`,
		`-f state="${state}" -f context=required-gates-complete -f description='A required gate failed'`, 1)
	if body == trustedRequiredWorkflow {
		t.Fatal("the terminal publisher's argument list moved; this attack would be replacing nothing")
	}
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a publisher that posts a literal description must be rejected")
	}
	requireErrorContaining(t, errs, "literal -f description=")
}

// TestCheckRequiredStatusWorkflows_AcceptsBoundPublishedValues is the positive
// control for the two attacks above: the bound form both arguments actually
// ship in must validate, or those tests would pass against a check that
// rejects everything.
func TestCheckRequiredStatusWorkflows_AcceptsBoundPublishedValues(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow,
		`-f state="${state}" -f context=required-gates-complete`,
		`-f state="${state}" -f context=required-gates-complete -f description="${description}"`, 1)
	if body == trustedRequiredWorkflow {
		t.Fatal("the terminal publisher's arguments moved; this control would be asserting nothing")
	}
	if errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry()); len(errs) != 0 {
		t.Fatalf("a publisher whose state and description both come from the case block must validate, got %v", errs)
	}
}
