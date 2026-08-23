// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"strings"
	"testing"
)

// The shape of the publish itself, split out of
// required_workflow_outcome_test.go at the 500-line cap. Those tests attack
// what the publisher DECIDES; these attack the call it makes with that
// decision -- how many times it publishes, which head SHA and which context
// it writes to, and whether the step is still recognised as the publisher at
// all once its arms are reworded.

// TestCheckRequiredStatusWorkflows_RequiresOutcomeBranchedPublisher is the
// pre-#6075 shape: no branch at all, one unconditional failure. Every
// non-success outcome landed as the same red on the status the ruleset uses to
// summarize every other gate.
func TestCheckRequiredStatusWorkflows_RequiresOutcomeBranchedPublisher(t *testing.T) {
	t.Parallel()

	body := strings.Replace(trustedRequiredWorkflow, fixtureCaseBlock,
		"          state=failure\n          description='A required gate failed'\n", 1)
	if body == trustedRequiredWorkflow {
		t.Fatal("the fixture's case block moved; this attack would be replacing nothing")
	}
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a publisher that defaults every non-success outcome to failure must be rejected")
	}
	requireErrorContaining(t, errs, `posts state="failure" for AGGREGATE_CODE=0`)
}

// TestCheckRequiredStatusWorkflows_RejectsASecondPublish keeps the "post
// again afterwards" door shut. One correct publish followed by a second one
// leaves the head SHA carrying whichever ran last.
func TestCheckRequiredStatusWorkflows_RejectsASecondPublish(t *testing.T) {
	t.Parallel()

	body := injectBeforePublish(t, realShapedPublisher(t, "", cancelledArm), "") +
		"          gh api -X POST repos/example/repo/statuses/${HEAD_SHA} -f state=success" +
		" -f context=required-gates-complete -f description='All gates passed'\n"
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a second status publish must be rejected; the verdict on the head SHA would depend on order")
	}
	requireErrorContaining(t, errs, "posts 2 statuses for AGGREGATE_CODE=10")
}

// TestCheckRequiredStatusWorkflows_RejectsAPublishToAnotherHead pins the
// target. A verdict written to some other SHA leaves the head being merged
// with the `pending` the first step wrote.
func TestCheckRequiredStatusWorkflows_RejectsAPublishToAnotherHead(t *testing.T) {
	t.Parallel()

	body := strings.Replace(realShapedPublisher(t, "", cancelledArm),
		"          gh api -X POST repos/example/repo/statuses/${HEAD_SHA}",
		"          gh api -X POST repos/example/repo/statuses/main", 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("publishing to anything but the head SHA must be rejected")
	}
	requireErrorContaining(t, errs, "must target the head SHA it was given")
}

// TestCheckRequiredStatusWorkflows_RejectsAPublishToAnotherContext keeps the
// verdict on the context the ruleset actually reads.
func TestCheckRequiredStatusWorkflows_RejectsAPublishToAnotherContext(t *testing.T) {
	t.Parallel()

	body := strings.Replace(realShapedPublisher(t, "", cancelledArm),
		"-f context=required-gates-complete -f description=",
		"-f context=required-gates-advisory -f description=", 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("publishing to a different context must be rejected")
	}
	requireErrorContaining(t, errs, `posts context="required-gates-advisory"`)
}

// TestCheckRequiredStatusWorkflows_RejectsATerminalPublisherDeletedByRewording
// is the selection half of the same class. The terminal step used to be found
// by the literal `state=failure` appearing somewhere in it, so a publisher
// that reworded that arm was no longer recognised as a publisher and every
// check above stopped running with nothing to say so.
func TestCheckRequiredStatusWorkflows_RejectsATerminalPublisherDeletedByRewording(t *testing.T) {
	t.Parallel()

	body := strings.Replace(realShapedPublisher(t, "", cancelledArm),
		"              10) state=failure; description='A required gate failed' ;;\n",
		"              10) state=\"fail${empty}ure\"; description='A required gate failed' ;;\n", 1)
	// The publisher still behaves correctly, so it must still be recognised
	// and still pass. A selector keyed on the literal would have skipped it.
	if errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry()); len(errs) != 0 {
		t.Fatalf("a publisher that spells state=failure differently but behaves correctly must pass, got %v", errs)
	}
	// And the same spelling with the wrong verdict must be caught, which is
	// only possible because selection no longer depends on that literal.
	broken := strings.Replace(realShapedPublisher(t, "", cancelledArm),
		"              10) state=failure; description='A required gate failed' ;;\n",
		"              10) state=\"succ${empty}ess\"; description='A required gate failed' ;;\n", 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, broken), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a publisher with no literal state=failure anywhere must still be validated")
	}
	requireErrorContaining(t, errs, `posts state="success" for AGGREGATE_CODE=10`)
}
