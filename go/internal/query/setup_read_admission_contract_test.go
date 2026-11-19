// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenSetupReadAdmission pins the exact method set behind the
// versioned #9669 W5 admission contract for the setup-state read (GET
// /api/v0/auth/setup-state): the single wizard-reachability read the
// route admits through (SetupNeeded). The entry re-declares the store
// signature in this setup-read-scoped shape instead of embedding the
// setup store, so no other family can cite this pin — embedding or
// aliasing SetupStore here would be a blanket pin, which the arbiter
// ruling forbids. A renamed, added, removed, or re-signed method fails
// here until that list is deliberately updated; bump
// SetupReadAdmissionContractVersion alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers
// can pin the shape, keeping the eshu-api extraction boundary from
// drifting silently.
//
// W-wave consumer: W5 setup read half (GET /api/v0/auth/setup-state
// only; the three setup mutations ride the sibling SetupAdmission
// writer pin, never this one).
var frozenSetupReadAdmission = []string{
	"SetupNeeded func(context.Context) (bool, error)",
}

// setupReadAdmissionWitness proves SetupReadAdmission is satisfiable
// with the minimal shape: the single wizard-reachability read, no
// wider store surface.
type setupReadAdmissionWitness struct{}

func (setupReadAdmissionWitness) SetupNeeded(context.Context) (bool, error) {
	return false, nil
}

var _ SetupReadAdmission = setupReadAdmissionWitness{}

// setupReadAdmissionMethodSet reflects the live SetupReadAdmission
// method set into sorted "name signature" strings for comparison with
// the frozen list. The iface parameter forces every caller —
// including the seeded-drift RED proof below — through this same
// guard comparison, so a passing suite proves the guard itself can
// see drift rather than proving a private copy can.
func setupReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestSetupReadAdmissionContractIsVersioned pins the #9669 W5
// admission-contract version for the setup-state read. The frozen
// method set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is
// deliberately updated. Bump SetupReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
func TestSetupReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SetupReadAdmissionContractVersion != "v0" {
		t.Errorf("SetupReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SetupReadAdmissionContractVersion, "v0")
	}
	got := setupReadAdmissionMethodSet(t, (*SetupReadAdmission)(nil))
	if len(got) != len(frozenSetupReadAdmission) {
		t.Fatalf("SetupReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSetupReadAdmission, len(frozenSetupReadAdmission), SetupReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSetupReadAdmission[i] {
			t.Errorf("SetupReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSetupReadAdmission[i], SetupReadAdmissionContractVersion)
		}
	}
}

// TestSetupReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the one re-declared setup read, with no credential,
// owner, rotation, or MFA surface riding along. The frozen
// method-set contract test pins the exact name; this test pins the
// exclusion the arbiter ruling demands.
func TestSetupReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*SetupReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("SetupReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Verify", "Resolve", "Rotate", "Complete", "Credential", "Owner", "MFA", "Password",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("SetupReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededSetupReadDrift is a deliberately widened copy of the pin: the
// one frozen setup read plus a credential-verification read the
// setup-state lane never issues. It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededSetupReadDrift interface {
	SetupReadAdmission
	VerifyBootstrapCredential(context.Context, string, string) (bool, error)
}

// TestSetupReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on
// a clean tree precisely because the guard fires on drift — a guard
// that cannot see the seeded method is no guard.
func TestSetupReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := setupReadAdmissionMethodSet(t, (*seededSetupReadDrift)(nil))
	if reflect.DeepEqual(got, frozenSetupReadAdmission) {
		t.Fatal("seeded widening (VerifyBootstrapCredential) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenSetupReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenSetupReadAdmission)+1)
	}
}
