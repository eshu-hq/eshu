// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auth

import (
	"context"
	"reflect"
	"sort"
	"strings"
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
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func signInPolicyReadAdmissionMethodSet(t *testing.T, iface any) []string {
	t.Helper()
	rt := reflect.TypeOf(iface).Elem()
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
	got := signInPolicyReadAdmissionMethodSet(t, (*SignInPolicyReadAdmission)(nil))
	if len(got) != len(frozenSignInPolicyReadAdmission) {
		t.Fatalf("SignInPolicyReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSignInPolicyReadAdmission, len(frozenSignInPolicyReadAdmission), SignInPolicyReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSignInPolicyReadAdmission[i] {
			t.Errorf("SignInPolicyReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSignInPolicyReadAdmission[i], SignInPolicyReadAdmissionContractVersion)
		}
	}
}

// signInPolicyReadAdmissionResignedWitness carries a deliberately
// re-signed GetSignInPolicy (an extra trailing parameter). It MUST
// NOT satisfy SignInPolicyReadAdmission: the seeded rejection test
// below proves a signature drift on the pinned method breaks the pin
// instead of sliding through silently.
type signInPolicyReadAdmissionResignedWitness struct{}

func (signInPolicyReadAdmissionResignedWitness) GetSignInPolicy(context.Context, string, bool) (SignInPolicy, error) {
	return SignInPolicy{}, nil
}

// TestSignInPolicyReadAdmissionRejectsResignedMethod is the seeded RED
// half of the contract proof: a witness whose GetSignInPolicy carries
// one extra parameter MUST NOT satisfy the pin. If a re-signed method
// ever slides through, this test fails and the frozen shape above can
// no longer be trusted to catch drift.
func TestSignInPolicyReadAdmissionRejectsResignedMethod(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*SignInPolicyReadAdmission)(nil)).Elem()
	if reflect.TypeOf(signInPolicyReadAdmissionResignedWitness{}).Implements(pin) {
		t.Errorf("signInPolicyReadAdmissionResignedWitness implements SignInPolicyReadAdmission: a re-signed GetSignInPolicy must break the pin")
	}
	if !reflect.TypeOf(signInPolicyReadAdmissionWitness{}).Implements(pin) {
		t.Errorf("signInPolicyReadAdmissionWitness does not implement SignInPolicyReadAdmission: the minimal shape must satisfy the pin")
	}
}

// TestSignInPolicyReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the one re-declared policy read, with no mutation,
// list, or other-family read surface riding along. The frozen
// method-set contract test pins the exact name; this test pins the
// exclusion the arbiter ruling demands.
func TestSignInPolicyReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*SignInPolicyReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("SignInPolicyReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Upsert", "Put", "Delete", "Set", "List", "Search", "Match", "Snapshot", "Ledger",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("SignInPolicyReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededSignInPolicyDrift is a deliberately widened copy of the pin:
// the one frozen policy read plus a provider-list read, which the
// sign-in-policy lane never issues (providers serve through the same
// store but the pin adopts GetSignInPolicy only). It never ships; it
// proves the frozen-set comparison below can see a one-method
// widening.
type seededSignInPolicyDrift interface {
	SignInPolicyReadAdmission
	ListAuthProviders(context.Context, string) ([]string, error)
}

// TestSignInPolicyReadAdmissionSeededDriftIsDetected is the RED half
// of the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes
// on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestSignInPolicyReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := signInPolicyReadAdmissionMethodSet(t, (*seededSignInPolicyDrift)(nil))
	if reflect.DeepEqual(got, frozenSignInPolicyReadAdmission) {
		t.Fatal("seeded widening (ListAuthProviders) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenSignInPolicyReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenSignInPolicyReadAdmission)+1)
	}
}
