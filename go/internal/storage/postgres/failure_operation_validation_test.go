// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// The in-tree store is the second eshu-side claim boundary: it refuses a
// mutation whose failure operation is not a bounded token, so a bypassing
// caller can never persist a URL, response body, or credential into the
// durable claim record that the read plane serves.

func validClaimMutation() workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID: "item-1", ClaimID: "claim-1",
		FencingToken: 1, OwnerID: "owner-1",
		ObservedAt:    time.Date(2026, time.June, 10, 13, 0, 0, 0, time.UTC),
		LeaseDuration: time.Minute,
	}
}

func TestValidateClaimMutationAcceptsBoundedFailureOperation(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"", "collect_failure", "list_org_alerts"} {
		mutation := validClaimMutation()
		mutation.FailureOperation = operation
		if err := validateClaimMutation(mutation); err != nil {
			t.Errorf("validateClaimMutation() with operation %q error = %v, want nil", operation, err)
		}
	}
}

func TestValidateClaimMutationRejectsUnboundedFailureOperation(t *testing.T) {
	t.Parallel()

	mutation := validClaimMutation()
	mutation.FailureOperation = "https://hooks.example.com/deploy?token=secret"
	err := validateClaimMutation(mutation)
	if err == nil {
		t.Fatal("validateClaimMutation() error = nil, want rejection")
	}
	if !strings.Contains(err.Error(), "failure_operation") {
		t.Fatalf("validateClaimMutation() error = %q, want failure_operation", err)
	}
}
