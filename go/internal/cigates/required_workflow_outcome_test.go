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
// one status branch protection uses to summarize every other gate.
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
