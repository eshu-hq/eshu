// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"testing"
)

// frozenSetupAdmission pins the exact method set behind the versioned #8872
// admission contract. A renamed, added, removed, or re-signed method fails
// here until that list is deliberately updated; bump
// SetupAdmissionContractVersion alongside so dest consumers can pin the
// shape, keeping the eshu-api extraction boundary from drifting silently.
var frozenSetupAdmission = []string{
	"CompleteSetupMFA func(context.Context, query.CompleteSetupMFAInput) (bool, error)",
	"ResolveSetupOwner func(context.Context) (query.SetupOwner, error)",
	"RotateSetupPassword func(context.Context, local.IdentityPasswordReset) error",
	"SetupNeeded func(context.Context) (bool, error)",
	"VerifyBootstrapCredential func(context.Context, string, string) (bool, error)",
}

// setupAdmissionMethodSet reflects the live SetupAdmission method set into
// sorted "name signature" strings for comparison with the frozen list.
func setupAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*SetupAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestSetupAdmissionContractIsVersioned pins the #8872 admission-contract
// version for the first-run setup wizard. The version value plus the frozen
// method set below are the enforcement: a breaking change to the admission
// shape fails this test until the frozen list is deliberately updated and
// the version bumps in the same diff.
func TestSetupAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SetupAdmissionContractVersion != "v0" {
		t.Errorf("SetupAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SetupAdmissionContractVersion, "v0")
	}
	got := setupAdmissionMethodSet(t)
	if len(got) != len(frozenSetupAdmission) {
		t.Fatalf("SetupAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSetupAdmission, len(frozenSetupAdmission), SetupAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSetupAdmission[i] {
			t.Errorf("SetupAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSetupAdmission[i], SetupAdmissionContractVersion)
		}
	}
}
