// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auth

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

// frozenSignInPolicyReadAdmission pins the exact method set behind the
// versioned #9669 O6 admission contract for the sign-in-policy reads:
// the single GetSignInPolicy read the admin sign-in-policy GET, the
// public sign-in-policy GET, and the providers GET admit through. The
// entry re-declares the shared-port signature in this
// sign-in-policy-scoped shape instead of embedding the shared port,
// so no other family can cite this pin — embedding or aliasing
// SignInPolicyReadStore here would be a blanket pin, which the
// arbiter ruling forbids. A renamed, added, removed, or re-signed
// method fails here until that list is deliberately updated; bump
// SignInPolicyReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// D-wave consumer: O6 pin 1 (per the arbiter O6 ruling mint order).
var frozenSignInPolicyReadAdmission = []string{
	"GetSignInPolicy func(context.Context, string) (auth.SignInPolicy, error)",
}

// signInPolicyReadAdmissionWitness proves SignInPolicyReadAdmission
// is satisfiable: the core-owned sign-in-policy store satisfies it. It
// defines the package's minimal witness rather than inventing a new
// satisfier shape — no other test fake in this package carries the
// read method.
type signInPolicyReadAdmissionWitness struct{}

func (signInPolicyReadAdmissionWitness) GetSignInPolicy(context.Context, string) (SignInPolicy, error) {
	return SignInPolicy{}, nil
}

var _ SignInPolicyReadAdmission = signInPolicyReadAdmissionWitness{}

// signInPolicyReadAdmissionMethodSet reflects the live
// SignInPolicyReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func signInPolicyReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*SignInPolicyReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestSignInPolicyReadAdmissionContractIsVersioned pins the #9669 O6
// admission-contract version for the sign-in-policy reads. The frozen
// method set below is the enforcement: an added, removed, renamed,
// or re-signed method fails this test until the frozen list is
// deliberately updated. Bump
// SignInPolicyReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
func TestSignInPolicyReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SignInPolicyReadAdmissionContractVersion != "v0" {
		t.Errorf("SignInPolicyReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SignInPolicyReadAdmissionContractVersion, "v0")
	}
	got := signInPolicyReadAdmissionMethodSet(t)
	if len(got) != len(frozenSignInPolicyReadAdmission) {
		t.Fatalf("SignInPolicyReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSignInPolicyReadAdmission, len(frozenSignInPolicyReadAdmission), SignInPolicyReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSignInPolicyReadAdmission[i] {
			t.Errorf("SignInPolicyReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSignInPolicyReadAdmission[i], SignInPolicyReadAdmissionContractVersion)
		}
	}
}
