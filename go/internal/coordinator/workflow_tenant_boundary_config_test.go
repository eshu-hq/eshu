// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import "testing"

func TestParseWorkflowTenantBoundaryJSON(t *testing.T) {
	t.Parallel()

	boundary, err := parseWorkflowTenantBoundaryJSON(`{
		"tenant_id": "tenant-a",
		"workspace_id": "workspace-a",
		"subject_class": "collector",
		"policy_revision_hash": "policy-a"
	}`)
	if err != nil {
		t.Fatalf("parseWorkflowTenantBoundaryJSON() error = %v, want nil", err)
	}
	if got, want := boundary.TenantID, "tenant-a"; got != want {
		t.Fatalf("TenantID = %q, want %q", got, want)
	}
	if got, want := boundary.WorkspaceID, "workspace-a"; got != want {
		t.Fatalf("WorkspaceID = %q, want %q", got, want)
	}
	if got, want := boundary.SubjectClass, "collector"; got != want {
		t.Fatalf("SubjectClass = %q, want %q", got, want)
	}
	if got, want := boundary.PolicyRevisionHash, "policy-a"; got != want {
		t.Fatalf("PolicyRevisionHash = %q, want %q", got, want)
	}
}

func TestParseWorkflowTenantBoundaryJSONRejectsPartialBoundary(t *testing.T) {
	t.Parallel()

	_, err := parseWorkflowTenantBoundaryJSON(`{"tenant_id":"tenant-a"}`)
	if err == nil {
		t.Fatal("parseWorkflowTenantBoundaryJSON() error = nil, want validation error")
	}
}
