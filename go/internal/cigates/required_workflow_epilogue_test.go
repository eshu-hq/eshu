// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"strings"
	"testing"
)

// Why this file exists (#6218 review round 3).
//
// Two rounds of this review found the same class of hole in the same
// publisher, and each fix pinned one more piece of TEXT: first that the
// cancelled arm must exist, then that the `-f state=` argument must not be a
// literal. Neither says anything about the lines BETWEEN the case block and
// the `gh api` call. One `state=success` there overwrites whatever arm ran,
// publishes `success` for an exit code that means a gate genuinely failed, and
// passes the step's own closing `[[ "${state}" == "success" ]]` so the job
// goes green as well. `required-gates-complete` is one of the three checks
// this repository's ruleset requires on `main`, so that is a merge on a red
// gate.
//
// The load-bearing fix is in go/cmd/ci-gates: publishedRequiredStatus now
// evaluates the publisher from the PENDING_OUTCOME guard through the line
// before the `gh api` call under real bash, so the per-code tests observe
// whatever value actually reaches the publish. These tests are the static
// mirror of that, in the registry gate, so the two have to be defeated
// together rather than one at a time.

// epilogueAttack rebuilds the fixture's terminal step with extra lines
// injected between `esac` and the `gh api` call, and asserts the injection
// landed -- a replacement that matched nothing would report a clean run.
func epilogueAttack(t *testing.T, injected string) string {
	t.Helper()
	const anchor = "          esac\n          gh api -X POST"
	if !strings.Contains(trustedRequiredWorkflow, anchor) {
		t.Fatal("the terminal publisher's esac/gh api pair moved; this attack would be injecting nothing")
	}
	body := strings.Replace(trustedRequiredWorkflow, anchor,
		"          esac\n"+injected+"          gh api -X POST", 1)
	if body == trustedRequiredWorkflow {
		t.Fatal("the injection did not change the fixture")
	}
	return body
}

// TestCheckRequiredStatusWorkflows_RejectsAStateAssignmentBeforeThePublish is
// the finding itself. `state=success` after the case block makes every
// AGGREGATE_CODE arm decorative while all of them still read correctly.
func TestCheckRequiredStatusWorkflows_RejectsAStateAssignmentBeforeThePublish(t *testing.T) {
	t.Parallel()

	body := epilogueAttack(t, "          state=success\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a state assignment between the case block and the publish must be rejected; " +
			"it overwrites the arm's verdict and turns a failed gate's status green")
	}
	requireErrorContaining(t, errs, `assigns state="success" between its AGGREGATE_CODE case block`)
}

// TestCheckRequiredStatusWorkflows_RejectsADescriptionAssignmentBeforeThePublish
// covers the same door on the operator-facing half: "A required gate failed"
// posted for every outcome is the #6189 overclaim reached from one line lower.
func TestCheckRequiredStatusWorkflows_RejectsADescriptionAssignmentBeforeThePublish(t *testing.T) {
	t.Parallel()

	body := epilogueAttack(t, "          description='A required gate failed'\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a description assignment between the case block and the publish must be rejected")
	}
	requireErrorContaining(t, errs, `assigns description="A required gate failed"`)
}

// TestCheckRequiredStatusWorkflows_RejectsAnExportBeforeThePublish keeps the
// epilogue from being walked around the way the arms were: the check reads
// every word of every statement, not a leading assignment run, so the spelling
// that defeated effectiveStateAssignment does not help here either.
func TestCheckRequiredStatusWorkflows_RejectsAnExportBeforeThePublish(t *testing.T) {
	t.Parallel()

	body := epilogueAttack(t, "          export state=success\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("an exported state assignment before the publish must be rejected too")
	}
	requireErrorContaining(t, errs, `assigns state="success" between its AGGREGATE_CODE case block`)
}

// TestCheckRequiredStatusWorkflows_ReportsAnUnparseablePublishEpilogue keeps
// the refusal posture consistent with the arm reader. A line this validator
// cannot read is not a line it may clear.
func TestCheckRequiredStatusWorkflows_ReportsAnUnparseablePublishEpilogue(t *testing.T) {
	t.Parallel()

	body := epilogueAttack(t, "          state=$(printf success)\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a publish path built from unmodelled shell must be reported, not judged clean")
	}
	requireErrorContaining(t, errs, "outside the shell shapes")
}

// TestCheckRequiredStatusWorkflows_AcceptsAnEpilogueComment is the positive
// control. Without it a check that rejected every added line would pass all
// four attacks above and still be useless: comments are how the real workflow
// explains itself, and the scanner drops them before any assignment is read.
func TestCheckRequiredStatusWorkflows_AcceptsAnEpilogueComment(t *testing.T) {
	t.Parallel()

	body := epilogueAttack(t, "          # publish what the branch above decided: state=success is NOT set here\n")
	if errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry()); len(errs) != 0 {
		t.Fatalf("a comment before the publish must be accepted, got %v", errs)
	}
}
