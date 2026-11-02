// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"testing"
)

// frozenSignInPolicyAdmission pins the exact method set behind the
// versioned #8872 admission contract. A renamed, added, removed, or
// re-signed method fails here and must bump
// SignInPolicyAdmissionContractVersion in the same diff, so the eshu-api
// extraction boundary cannot drift silently.
var frozenSignInPolicyAdmission = []string{
	"UpsertSignInPolicy func(context.Context, string, query.SignInPolicyUpdateRequest, string, time.Time) (auth.SignInPolicy, error)",
}

// signInPolicyAdmissionMethodSet reflects the live SignInPolicyAdmission
// method set into sorted "name signature" strings for comparison with the
// frozen list.
func signInPolicyAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*SignInPolicyAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestSignInPolicyAdmissionContractIsVersioned pins the #8872
// admission-contract version for the admin sign-in policy mutation. The
// version value plus the frozen method set below are the enforcement: a
// breaking change to the admission shape fails this test until the version
// bumps in the same diff.
func TestSignInPolicyAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SignInPolicyAdmissionContractVersion != "v0" {
		t.Errorf("SignInPolicyAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SignInPolicyAdmissionContractVersion, "v0")
	}
	got := signInPolicyAdmissionMethodSet(t)
	if len(got) != len(frozenSignInPolicyAdmission) {
		t.Fatalf("SignInPolicyAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSignInPolicyAdmission, len(frozenSignInPolicyAdmission), SignInPolicyAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSignInPolicyAdmission[i] {
			t.Errorf("SignInPolicyAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSignInPolicyAdmission[i], SignInPolicyAdmissionContractVersion)
		}
	}
}
