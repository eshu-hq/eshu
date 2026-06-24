// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestReplatformingSourceStateForManagementStatusIsDeterministic(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   ReplatformingSourceState
	}{
		{"managed_by_terraform", managementStatusManagedByTerraform, ReplatformingSourceStateExact},
		{"terraform_state_only", managementStatusTerraformStateOnly, ReplatformingSourceStateDerived},
		{"terraform_config_only", managementStatusTerraformConfigOnly, ReplatformingSourceStateDerived},
		{"cloud_only", managementStatusCloudOnly, ReplatformingSourceStateDerived},
		{"managed_by_other_iac", managementStatusManagedByOtherIaC, ReplatformingSourceStateDerived},
		{"ambiguous_management", managementStatusAmbiguous, ReplatformingSourceStateAmbiguous},
		{"stale_iac_candidate", managementStatusStaleIaCCandidate, ReplatformingSourceStateStale},
		{"unknown_management", managementStatusUnknown, ReplatformingSourceStateUnknown},
		{"unrecognized falls back to unknown", "totally_unmapped_status", ReplatformingSourceStateUnknown},
		{"empty falls back to unknown", "", ReplatformingSourceStateUnknown},
		{"whitespace is trimmed", "  cloud_only  ", ReplatformingSourceStateDerived},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReplatformingSourceStateForManagementStatus(tc.status); got != tc.want {
				t.Fatalf("ReplatformingSourceStateForManagementStatus(%q) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}

func TestReplatformingSourceStateForManagementStatusCoversEveryAWSStatus(t *testing.T) {
	// Every AWS management status the query layer can emit must map to a valid,
	// non-unknown taxonomy state except for the explicit unknown_management
	// status. A new AWS status added without a mapping must fail this test
	// rather than silently degrade to unknown.
	awsStatuses := map[string]bool{
		managementStatusManagedByTerraform:  false,
		managementStatusTerraformStateOnly:  false,
		managementStatusTerraformConfigOnly: false,
		managementStatusCloudOnly:           false,
		managementStatusManagedByOtherIaC:   false,
		managementStatusAmbiguous:           false,
		managementStatusStaleIaCCandidate:   false,
		managementStatusUnknown:             true, // only this one is allowed to map to unknown
	}
	for status, allowUnknown := range awsStatuses {
		got := ReplatformingSourceStateForManagementStatus(status)
		if !got.Valid() {
			t.Fatalf("status %q mapped to invalid taxonomy state %q", status, got)
		}
		if got == ReplatformingSourceStateUnknown && !allowUnknown {
			t.Fatalf("status %q degraded to unknown; add an explicit deterministic mapping", status)
		}
	}
}

func TestReplatformingSourceStateForMultiCloudQueryStateAdoptsContract(t *testing.T) {
	// The multi-cloud collector contract per-item states must adopt the
	// taxonomy without renaming. GCP/Azure facts keep their provider-specific
	// names; only the query-facing item state is normalized here.
	cases := map[string]ReplatformingSourceState{
		"exact":       ReplatformingSourceStateExact,
		"derived":     ReplatformingSourceStateDerived,
		"partial":     ReplatformingSourceStatePartial,
		"stale":       ReplatformingSourceStateStale,
		"unavailable": ReplatformingSourceStateUnavailable,
		"unsupported": ReplatformingSourceStateUnsupported,
		"surprise":    ReplatformingSourceStateUnknown,
		"":            ReplatformingSourceStateUnknown,
	}
	for state, want := range cases {
		if got := ReplatformingSourceStateForMultiCloudQueryState(state); got != want {
			t.Fatalf("ReplatformingSourceStateForMultiCloudQueryState(%q) = %q, want %q", state, got, want)
		}
	}
}

func TestResolveReplatformingSourceStateAppliesRejectedGate(t *testing.T) {
	// A safety-gate promotion rejection wins over the evidence-derived state so
	// that a read-only finding is never presented as ready for automation.
	if got := ResolveReplatformingSourceState(managementStatusCloudOnly, true); got != ReplatformingSourceStateRejected {
		t.Fatalf("rejected promotion = %q, want rejected", got)
	}
	if got := ResolveReplatformingSourceState(managementStatusCloudOnly, false); got != ReplatformingSourceStateDerived {
		t.Fatalf("non-rejected cloud_only = %q, want derived", got)
	}
	if got := ResolveReplatformingSourceState(managementStatusAmbiguous, false); got != ReplatformingSourceStateAmbiguous {
		t.Fatalf("ambiguous = %q, want ambiguous", got)
	}
}

func TestAllReplatformingSourceStatesAreValidAndStable(t *testing.T) {
	want := []ReplatformingSourceState{
		ReplatformingSourceStateExact,
		ReplatformingSourceStateDerived,
		ReplatformingSourceStatePartial,
		ReplatformingSourceStateAmbiguous,
		ReplatformingSourceStateStale,
		ReplatformingSourceStateUnavailable,
		ReplatformingSourceStateUnsupported,
		ReplatformingSourceStateUnknown,
		ReplatformingSourceStateRejected,
	}
	got := AllReplatformingSourceStates()
	if len(got) != len(want) {
		t.Fatalf("AllReplatformingSourceStates len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("AllReplatformingSourceStates[%d] = %q, want %q", i, got[i], want[i])
		}
		if !got[i].Valid() {
			t.Fatalf("state %q reported invalid", got[i])
		}
	}
	if ReplatformingSourceState("not_a_state").Valid() {
		t.Fatal("unexpected state reported valid")
	}
}

func TestReplatformingPlanReadinessCapabilityProfileGate(t *testing.T) {
	// An unsupported profile must surface unsupported_capability, never a
	// downgraded answer, for the provider-neutral replatforming readiness
	// capability.
	if !capabilityUnsupported(ProfileLocalLightweight, replatformingPlanReadinessCapability) {
		t.Fatal("local_lightweight must not support replatforming plan readiness")
	}
	for _, profile := range []QueryProfile{ProfileLocalAuthoritative, ProfileLocalFullStack, ProfileProduction} {
		if capabilityUnsupported(profile, replatformingPlanReadinessCapability) {
			t.Fatalf("profile %q must support replatforming plan readiness", profile)
		}
	}
	if got := requiredProfile(replatformingPlanReadinessCapability); got != ProfileLocalAuthoritative {
		t.Fatalf("requiredProfile = %q, want local_authoritative", got)
	}
}
