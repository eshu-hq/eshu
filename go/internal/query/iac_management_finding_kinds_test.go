// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestNormalizeIaCManagementFindingKindsAcceptsImageVersionDrift proves the
// finding_kinds allowlist admits "image_version_drift" (#5453) so
// list_cloud_runtime_drift_findings and list_aws_runtime_drift_findings can
// filter to the new kind instead of returning a 400 invalid_argument.
func TestNormalizeIaCManagementFindingKindsAcceptsImageVersionDrift(t *testing.T) {
	t.Parallel()

	got, err := normalizeIaCManagementFindingKinds([]string{"image_version_drift"})
	if err != nil {
		t.Fatalf("normalizeIaCManagementFindingKinds([image_version_drift]) error = %v, want nil", err)
	}
	want := []string{"image_version_drift"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("normalizeIaCManagementFindingKinds() = %#v, want %#v", got, want)
	}
}

// TestNormalizeIaCManagementFindingKindsStillRejectsUnknownKind is a
// regression guard: the allowlist must stay closed against typos.
func TestNormalizeIaCManagementFindingKindsStillRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	if _, err := normalizeIaCManagementFindingKinds([]string{"not_a_real_kind"}); err == nil {
		t.Fatalf("normalizeIaCManagementFindingKinds([not_a_real_kind]) error = nil, want an error")
	}
}

// TestNormalizeIaCManagementFindingKindsCombinesExistenceAndValueDriftKinds
// proves a caller can filter to both an existence kind and the value-drift
// kind in the same request.
func TestNormalizeIaCManagementFindingKindsCombinesExistenceAndValueDriftKinds(t *testing.T) {
	t.Parallel()

	got, err := normalizeIaCManagementFindingKinds([]string{"image_version_drift", "unmanaged_cloud_resource"})
	if err != nil {
		t.Fatalf("normalizeIaCManagementFindingKinds() error = %v, want nil", err)
	}
	want := []string{"image_version_drift", "unmanaged_cloud_resource"}
	if len(got) != len(want) {
		t.Fatalf("normalizeIaCManagementFindingKinds() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeIaCManagementFindingKinds() = %#v, want %#v", got, want)
		}
	}
}
