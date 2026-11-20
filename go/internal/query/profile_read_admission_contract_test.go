// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/local"
)

// frozenProfileReadAdmission pins the exact method set behind the
// versioned #9669 W5 admission contract for the profile read (GET
// /api/v0/auth/profile): the single MFA-status read the route admits
// through (GetLocalIdentityMFAStatus). The entry re-declares the store
// signature in this profile-read-scoped shape instead of embedding the
// profile lister, so no other family can cite this pin — embedding or
// aliasing local.IdentityProfileLister here would be a blanket pin,
// which the arbiter ruling forbids. A renamed, added, removed, or
// re-signed method fails here until that list is deliberately updated;
// bump ProfileReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// W-wave consumer: W5 profile read (GET /api/v0/auth/profile only).
var frozenProfileReadAdmission = []string{
	"GetLocalIdentityMFAStatus func(context.Context, string, time.Time) (local.IdentityMFAStatus, error)",
}

// profileReadAdmissionWitness proves ProfileReadAdmission is
// satisfiable with the minimal shape: the single MFA-status read, no
// wider store surface.
type profileReadAdmissionWitness struct{}

func (profileReadAdmissionWitness) GetLocalIdentityMFAStatus(context.Context, string, time.Time) (local.IdentityMFAStatus, error) {
	return local.IdentityMFAStatus{}, nil
}

var _ ProfileReadAdmission = profileReadAdmissionWitness{}

// profileReadAdmissionMethodSet reflects the live ProfileReadAdmission
// method set into sorted "name signature" strings for comparison with
// the frozen list. The iface parameter forces every caller —
// including the seeded-drift RED proof below — through this same
// guard comparison, so a passing suite proves the guard itself can
// see drift rather than proving a private copy can.
func profileReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestProfileReadAdmissionContractIsVersioned pins the #9669 W5
// admission-contract version for the profile read. The frozen method
// set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is
// deliberately updated. Bump ProfileReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
func TestProfileReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ProfileReadAdmissionContractVersion != "v0" {
		t.Errorf("ProfileReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ProfileReadAdmissionContractVersion, "v0")
	}
	got := profileReadAdmissionMethodSet(t, (*ProfileReadAdmission)(nil))
	if len(got) != len(frozenProfileReadAdmission) {
		t.Fatalf("ProfileReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenProfileReadAdmission, len(frozenProfileReadAdmission), ProfileReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenProfileReadAdmission[i] {
			t.Errorf("ProfileReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenProfileReadAdmission[i], ProfileReadAdmissionContractVersion)
		}
	}
}

// TestProfileReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the one re-declared profile read, with no token-list,
// user-ID resolution, TOTP enrollment, or session surface riding
// along. The frozen method-set contract test pins the exact name;
// this test pins the exclusion the arbiter ruling demands.
func TestProfileReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*ProfileReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("ProfileReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Token", "Resolve", "TOTP", "Enroll", "Session", "Revoke", "Password",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("ProfileReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededProfileReadDrift is a deliberately widened copy of the pin:
// the one frozen profile read plus a token-list read the profile lane
// never issues. It never ships; it proves the frozen-set comparison
// below can see a one-method widening.
type seededProfileReadDrift interface {
	ProfileReadAdmission
	ListAPITokensBySubject(context.Context, string, time.Time) ([]string, error)
}

// TestProfileReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on
// a clean tree precisely because the guard fires on drift — a guard
// that cannot see the seeded method is no guard.
func TestProfileReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := profileReadAdmissionMethodSet(t, (*seededProfileReadDrift)(nil))
	if reflect.DeepEqual(got, frozenProfileReadAdmission) {
		t.Fatal("seeded widening (ListAPITokensBySubject) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenProfileReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenProfileReadAdmission)+1)
	}
}
