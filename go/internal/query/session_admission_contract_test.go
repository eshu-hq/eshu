// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The four witnesses below prove each #9669 D8 admission pin is
// satisfiable by the doubles the existing handler tests already use:
// the seam constrains the boundary without stranding any caller. They
// are compile-time assertions only.
var (
	_ SessionResolverAdmission = &fakeBrowserSessionResolver{}
	_ BrowserSessionAdmission  = &fakeBrowserSessionListStore{}
	_ LocalIdentityAdmission   = &fakeLocalIdentityStore{}
	_ APITokenAdmission        = &fakeLocalIdentityStore{}
)

// frozenSessionResolverAdmission pins the exact method set behind the
// versioned #9669 D8 session-resolver admission contract (TBD5-resolver).
// A renamed, added, removed, or re-signed method fails here until that
// list is deliberately updated; bump SessionResolverAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the eshu-api
// extraction boundary from drifting silently.
var frozenSessionResolverAdmission = []string{
	"ResolveBrowserSession func(context.Context, string, string, bool, time.Time) (auth.AuthContext, bool, error)",
}

// sessionResolverAdmissionMethodSet reflects the live
// SessionResolverAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func sessionResolverAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*SessionResolverAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestSessionResolverAdmissionContractIsVersioned pins the #9669 D8
// admission-contract version for the fail-closed session resolver. The
// version value plus the frozen method set below are the enforcement: a
// breaking change to the admission shape fails this test until the version
// bumps in the same diff.
func TestSessionResolverAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SessionResolverAdmissionContractVersion != "v0" {
		t.Errorf("SessionResolverAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SessionResolverAdmissionContractVersion, "v0")
	}
	got := sessionResolverAdmissionMethodSet(t)
	if len(got) != len(frozenSessionResolverAdmission) {
		t.Fatalf("SessionResolverAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSessionResolverAdmission, len(frozenSessionResolverAdmission), SessionResolverAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSessionResolverAdmission[i] {
			t.Errorf("SessionResolverAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSessionResolverAdmission[i], SessionResolverAdmissionContractVersion)
		}
	}
}

// frozenBrowserSessionAdmission pins the exact method set behind the
// versioned #9669 D8 browser-session admission contract (the BrowserSessions
// 4 lifecycle plus the SessionList read). A renamed, added, removed, or
// re-signed method fails here until that list is deliberately updated;
// bump BrowserSessionAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
var frozenBrowserSessionAdmission = []string{
	"CreateBrowserSession func(context.Context, session.BrowserSessionCreateRecord) error",
	"ListSessionsBySubject func(context.Context, string, time.Time, string, int, int) ([]query.BrowserSessionListItem, error)",
	"RevokeBrowserSession func(context.Context, string, time.Time) error",
	"SwitchBrowserSessionWorkspace func(context.Context, string, string, string, time.Time) (auth.AuthContext, bool, error)",
}

// browserSessionAdmissionMethodSet reflects the live BrowserSessionAdmission
// method set into sorted "name signature" strings for comparison with the
// frozen list. The iface parameter forces every caller — including the
// seeded-drift RED proof below — through this same guard comparison, so a
// passing suite proves the guard itself can see drift rather than proving
// a private copy can.
func browserSessionAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestBrowserSessionAdmissionContractIsVersioned pins the #9669 D8
// admission-contract version for the browser-session lifecycle and list
// surface. The version value plus the frozen method set below are the
// enforcement: a breaking change to the admission shape fails this test
// until the version bumps in the same diff.
func TestBrowserSessionAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if BrowserSessionAdmissionContractVersion != "v0" {
		t.Errorf("BrowserSessionAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", BrowserSessionAdmissionContractVersion, "v0")
	}
	got := browserSessionAdmissionMethodSet(t, (*BrowserSessionAdmission)(nil))
	if len(got) != len(frozenBrowserSessionAdmission) {
		t.Fatalf("BrowserSessionAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenBrowserSessionAdmission, len(frozenBrowserSessionAdmission), BrowserSessionAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenBrowserSessionAdmission[i] {
			t.Errorf("BrowserSessionAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenBrowserSessionAdmission[i], BrowserSessionAdmissionContractVersion)
		}
	}
}

// TestBrowserSessionAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the four D8 session methods — the SessionList read
// (ListSessionsBySubject) plus the three lifecycle methods that ride
// the same store — with no admin, impersonation, or snapshot surface
// riding along. The frozen method-set contract test pins the exact
// names; this test pins the exclusion the arbiter ruling demands. The
// lifecycle methods are D8 standing, recorded here not silently
// assumed: Wave 5 adopts ListSessionsBySubject only, and a satellite
// PR citing this record for lifecycle coverage is invalid.
func TestBrowserSessionAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*BrowserSessionAdmission)(nil)).Elem()
	if rt.NumMethod() != 4 {
		t.Fatalf("BrowserSessionAdmission has %d methods, want exactly 4: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Delete", "Update", "Upsert", "Impersonate", "Admin", "Snapshot", "Ledger",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("BrowserSessionAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededBrowserSessionDrift is a deliberately widened copy of the pin:
// the four frozen session methods plus a session-purge the session
// lane never issues. It never ships; it proves the frozen-set
// comparison below can see a one-method widening.
type seededBrowserSessionDrift interface {
	BrowserSessionAdmission
	PurgeBrowserSessions(context.Context) error
}

// TestBrowserSessionAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes
// on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestBrowserSessionAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := browserSessionAdmissionMethodSet(t, (*seededBrowserSessionDrift)(nil))
	if reflect.DeepEqual(got, frozenBrowserSessionAdmission) {
		t.Fatal("seeded widening (PurgeBrowserSessions) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenBrowserSessionAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenBrowserSessionAdmission)+1)
	}
}

// frozenLocalIdentityAdmission pins the exact method set behind the
// versioned #9669 D8 local-identity admission contract (the LocalIdentity
// 16 routes plus the Profile MFA-status read). A renamed, added, removed,
// or re-signed method fails here until that list is deliberately updated;
// bump LocalIdentityAdmissionContractVersion alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest consumers can
// pin the shape.
var frozenLocalIdentityAdmission = []string{
	"AcceptLocalIdentityInvitation func(context.Context, local.IdentityInvitationAcceptance) error",
	"AuthenticateLocalIdentity func(context.Context, local.IdentityAuthenticationAttempt) (local.IdentityAuthenticationResult, error)",
	"BeginLocalIdentityTOTPEnrollment func(context.Context, local.IdentityTOTPEnrollmentBegin) error",
	"BootstrapLocalIdentity func(context.Context, local.IdentityBootstrapRecord) error",
	"ConfirmLocalIdentityTOTPEnrollment func(context.Context, local.IdentityTOTPEnrollmentConfirm) error",
	"CreateLocalIdentityAPIToken func(context.Context, local.IdentityAPITokenCreate) error",
	"CreateLocalIdentityInvitation func(context.Context, local.IdentityInvitationRecord) error",
	"DisableLocalIdentityUser func(context.Context, local.IdentityDisableUser) error",
	"EnableLocalIdentityBreakGlass func(context.Context, local.IdentityBreakGlassWindow) error",
	"GetLocalIdentityMFAStatus func(context.Context, string, time.Time) (local.IdentityMFAStatus, error)",
	"ListAPITokensBySubject func(context.Context, string, time.Time) ([]local.IdentityAPITokenListItem, error)",
	"ResetLocalIdentityMFA func(context.Context, local.IdentityMFAReset) error",
	"ResetLocalIdentityPassword func(context.Context, local.IdentityPasswordReset) error",
	"ResolveLocalIdentityBreakGlass func(context.Context, local.IdentityBreakGlassAttempt) (local.IdentityAuthContext, error)",
	"ResolveLocalIdentityUserID func(context.Context, string) (string, bool, error)",
	"RevokeLocalIdentityAPIToken func(context.Context, local.IdentityAPITokenRevoke) error",
	"RotateLocalIdentityAPIToken func(context.Context, local.IdentityAPITokenRotate) error",
	"RotateLocalIdentityPassword func(context.Context, local.IdentityPasswordRotation) (local.IdentityAuthenticationResult, error)",
}

// localIdentityAdmissionMethodSet reflects the live LocalIdentityAdmission
// method set into sorted "name signature" strings for comparison with the
// frozen list.
func localIdentityAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*LocalIdentityAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestLocalIdentityAdmissionContractIsVersioned pins the #9669 D8
// admission-contract version for the local-identity lifecycle and profile
// surface. The version value plus the frozen method set below are the
// enforcement: a breaking change to the admission shape fails this test
// until the version bumps in the same diff.
func TestLocalIdentityAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if LocalIdentityAdmissionContractVersion != "v0" {
		t.Errorf("LocalIdentityAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", LocalIdentityAdmissionContractVersion, "v0")
	}
	got := localIdentityAdmissionMethodSet(t)
	if len(got) != len(frozenLocalIdentityAdmission) {
		t.Fatalf("LocalIdentityAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenLocalIdentityAdmission, len(frozenLocalIdentityAdmission), LocalIdentityAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenLocalIdentityAdmission[i] {
			t.Errorf("LocalIdentityAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenLocalIdentityAdmission[i], LocalIdentityAdmissionContractVersion)
		}
	}
}

// frozenAPITokenAdmission pins the exact method set behind the versioned
// #9669 D8 API-token admission contract (the four generated-token routes:
// create, list, revoke, rotate). A renamed, added, removed, or re-signed
// method fails here until that list is deliberately updated; bump
// APITokenAdmissionContractVersion alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers can pin
// the shape.
var frozenAPITokenAdmission = []string{
	"CreateLocalIdentityAPIToken func(context.Context, local.IdentityAPITokenCreate) error",
	"ListAPITokensBySubject func(context.Context, string, time.Time) ([]local.IdentityAPITokenListItem, error)",
	"RevokeLocalIdentityAPIToken func(context.Context, local.IdentityAPITokenRevoke) error",
	"RotateLocalIdentityAPIToken func(context.Context, local.IdentityAPITokenRotate) error",
}

// apiTokenAdmissionMethodSet reflects the live APITokenAdmission method set
// into sorted "name signature" strings for comparison with the frozen list.
func apiTokenAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*APITokenAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestAPITokenAdmissionContractIsVersioned pins the #9669 D8
// admission-contract version for the API-token lifecycle surface. The
// version value plus the frozen method set below are the enforcement: a
// breaking change to the admission shape fails this test until the version
// bumps in the same diff.
func TestAPITokenAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if APITokenAdmissionContractVersion != "v0" {
		t.Errorf("APITokenAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", APITokenAdmissionContractVersion, "v0")
	}
	got := apiTokenAdmissionMethodSet(t)
	if len(got) != len(frozenAPITokenAdmission) {
		t.Fatalf("APITokenAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenAPITokenAdmission, len(frozenAPITokenAdmission), APITokenAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenAPITokenAdmission[i] {
			t.Errorf("APITokenAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenAPITokenAdmission[i], APITokenAdmissionContractVersion)
		}
	}
}

// TestSessionResolverAdmissionFailClosed proves the #9669 D8 resolver pin
// denies when no pin resolves instead of inventing an auth context: a
// presented session cookie the resolver does not recognize (ok=false), or
// a resolver failure, answers 401 through the production middleware path
// and never reaches the next handler.
func TestSessionResolverAdmissionFailClosed(t *testing.T) {
	t.Parallel()

	cases := map[string]*fakeBrowserSessionResolver{
		"unrecognized credential denies": {ok: false},
		"resolver error denies":          {ok: false, err: errors.New("session store boom")},
	}
	for name, resolver := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var nextCalled bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
			})
			handler := AuthMiddlewareWithBrowserSessionsAndScopedTokens("shared-token", nil, resolver, next)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/profile", nil)
			req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "presented-session-secret"})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if !resolver.called {
				t.Error("resolver was not consulted for a presented session cookie")
			}
			if nextCalled {
				t.Error("next handler ran for an unresolvable session credential: deny must never invent a context")
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}
