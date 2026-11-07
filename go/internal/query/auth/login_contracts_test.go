// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auth

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

// TestAuthLoginContractsVersion pins the versioned contract set drained from
// root package query (D13, #9669). A breaking change to any named contract
// bumps AuthLoginContractsVersion in the same diff so dest consumers can pin
// the shape they code against.
func TestAuthLoginContractsVersion(t *testing.T) {
	if AuthLoginContractsVersion != "v0" {
		t.Fatalf("AuthLoginContractsVersion = %q, want %q", AuthLoginContractsVersion, "v0")
	}
	if GitHubLoginAdmissionContractVersion != "v0" {
		t.Fatalf("GitHubLoginAdmissionContractVersion = %q, want %q", GitHubLoginAdmissionContractVersion, "v0")
	}
	if OIDCLoginAdmissionContractVersion != "v0" {
		t.Fatalf("OIDCLoginAdmissionContractVersion = %q, want %q", OIDCLoginAdmissionContractVersion, "v0")
	}
}

// TestLoginSentinelMessages pins the exact fail-closed sentinel strings.
// Handlers map these to fixed 503/400/403 statuses and auditors match them
// with errors.Is; any rewording silently re-maps or un-maps an outcome.
func TestLoginSentinelMessages(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"github unavailable", ErrGitHubLoginUnavailable, "github login unavailable"},
		{"github invalid request", ErrGitHubLoginInvalidRequest, "github login invalid request"},
		{"github denied", ErrGitHubLoginDenied, "github login denied"},
		{"oidc unavailable", ErrOIDCLoginUnavailable, "oidc login unavailable"},
		{"oidc invalid request", ErrOIDCLoginInvalidRequest, "oidc login invalid request"},
		{"oidc denied", ErrOIDCLoginDenied, "oidc login denied"},
		{"bearer unrecognized", ErrBearerCredentialUnrecognized, "query: bearer credential not a recognized issued token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil {
				t.Fatal("sentinel is nil")
			}
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("sentinel message = %q, want %q", got, tc.want)
			}
		})
	}
	// Every sentinel is a distinct identity: errors.Is must never confuse an
	// unavailable outcome with a denial (503 vs 403).
	sentinels := []error{
		ErrGitHubLoginUnavailable, ErrGitHubLoginInvalidRequest, ErrGitHubLoginDenied,
		ErrOIDCLoginUnavailable, ErrOIDCLoginInvalidRequest, ErrOIDCLoginDenied,
		ErrBearerCredentialUnrecognized,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Fatalf("sentinels %d and %d alias: %v vs %v", i, j, a, b)
			}
		}
	}
}

// TestSSOLoginDeniedErrorFailClosed pins the denial wrapper's fail-closed
// behavior: a nil sentinel never panics (auditor still gets the reason), an
// empty reason degrades to the sentinel text, and Unwrap keeps errors.Is
// matching the provider sentinel through fmt wrap chains.
func TestSSOLoginDeniedErrorFailClosed(t *testing.T) {
	t.Run("nil sentinel reports reason without panic", func(t *testing.T) {
		err := &SSOLoginDeniedError{Reason: "org_not_allowed"}
		if got := err.Error(); got != "org_not_allowed" {
			t.Fatalf("Error() = %q, want %q", got, "org_not_allowed")
		}
		if err.Unwrap() != nil {
			t.Fatalf("Unwrap() = %v, want nil", err.Unwrap())
		}
	})
	t.Run("empty reason reports sentinel text", func(t *testing.T) {
		err := &SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied}
		if got := err.Error(); got != ErrGitHubLoginDenied.Error() {
			t.Fatalf("Error() = %q, want sentinel text", got)
		}
	})
	t.Run("sentinel plus reason joins with colon", func(t *testing.T) {
		err := &SSOLoginDeniedError{Sentinel: ErrOIDCLoginDenied, Reason: "no_grants"}
		want := ErrOIDCLoginDenied.Error() + ": no_grants"
		if got := err.Error(); got != want {
			t.Fatalf("Error() = %q, want %q", got, want)
		}
	})
	t.Run("errors.Is matches through double wrap", func(t *testing.T) {
		inner := &SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied, Reason: "no_team_role_mapping"}
		err := fmt.Errorf("callback: %w", fmt.Errorf("complete: %w", inner))
		if !errors.Is(err, ErrGitHubLoginDenied) {
			t.Fatal("errors.Is through double wrap does not match ErrGitHubLoginDenied")
		}
		if errors.Is(err, ErrGitHubLoginUnavailable) {
			t.Fatal("denial matches the unavailable sentinel: 403 would map to 503")
		}
	})
	t.Run("nil unwrap never matches", func(t *testing.T) {
		err := &SSOLoginDeniedError{Reason: "state_invalid"}
		if errors.Is(err, ErrGitHubLoginDenied) {
			t.Fatal("sentinel-less denial matches ErrGitHubLoginDenied")
		}
	})
}

// TestSSOLoginDenialReason pins reason extraction: the carried
// classification wins, and every non-carrying error fails closed to the
// caller-supplied fallback (auditors always get a valid non-empty code).
func TestSSOLoginDenialReason(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		fallback string
		want     string
	}{
		{"direct denial", &SSOLoginDeniedError{Sentinel: ErrOIDCLoginDenied, Reason: "no_grants"}, "sso_login_denied", "no_grants"},
		{"wrapped denial", fmt.Errorf("complete: %w", &SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied, Reason: "org_not_allowed"}), "sso_login_denied", "org_not_allowed"},
		{"empty reason falls back", &SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied}, "sso_login_denied", "sso_login_denied"},
		{"plain sentinel falls back", ErrGitHubLoginDenied, "sso_login_denied", "sso_login_denied"},
		{"unrelated error falls back", errors.New("boom"), "sso_login_error", "sso_login_error"},
		{"nil error falls back", nil, "sso_login_error", "sso_login_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SSOLoginDenialReason(tc.err, tc.fallback); got != tc.want {
				t.Fatalf("SSOLoginDenialReason = %q, want %q", got, tc.want)
			}
			if got := SSOLoginDenialReason(tc.err, tc.fallback); got == "" {
				t.Fatal("SSOLoginDenialReason returned empty: auditors require non-empty reason codes")
			}
		})
	}
}

func loginServiceMethods(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	if typ == nil {
		t.Fatal("nil service")
	}
	var names []string
	for i := 0; i < typ.NumMethod(); i++ {
		names = append(names, typ.Method(i).Name)
	}
	sort.Strings(names)
	return names
}

// TestLoginAdmissionMethodSets freezes the v0 admission method sets. A
// renamed, added, removed, or re-signed method fails here until the version
// moves with it, so eshu-api pins the exact shape it codes against. The
// assertion reads the interface method set directly, so no fake can drift
// out of conformance silently.
func TestLoginAdmissionMethodSets(t *testing.T) {
	t.Run("github", func(t *testing.T) {
		got := loginServiceMethods(t, reflect.TypeOf((*GitHubLoginService)(nil)).Elem())
		want := []string{"CompleteGitHubLogin", "StartGitHubLogin"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("GitHubLoginService methods = %v, want %v", got, want)
		}
		gotAdmission := loginServiceMethods(t, reflect.TypeOf((*GitHubLoginAdmission)(nil)).Elem())
		if !reflect.DeepEqual(gotAdmission, want) {
			t.Fatalf("GitHubLoginAdmission methods = %v, want %v", gotAdmission, want)
		}
	})
	t.Run("oidc", func(t *testing.T) {
		got := loginServiceMethods(t, reflect.TypeOf((*OIDCLoginService)(nil)).Elem())
		want := []string{"CompleteOIDCLogin", "StartOIDCLogin"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("OIDCLoginService methods = %v, want %v", got, want)
		}
		gotAdmission := loginServiceMethods(t, reflect.TypeOf((*OIDCLoginAdmission)(nil)).Elem())
		if !reflect.DeepEqual(gotAdmission, want) {
			t.Fatalf("OIDCLoginAdmission methods = %v, want %v", gotAdmission, want)
		}
	})
}

// TestLoginCompleteResponseCarriesAuthContext proves the callback response
// keeps the resolved auth context on the contract: dropping that field
// would issue sessions with zero bounds (fail-open).
func TestLoginCompleteResponseCarriesAuthContext(t *testing.T) {
	gh := GitHubLoginCompleteResponse{
		Auth:             AuthContext{Mode: AuthModeScoped, TenantID: "t", WorkspaceID: "w"},
		ProviderConfigID: "p",
	}
	if gh.Auth.TenantID != "t" || gh.Auth.Mode != AuthModeScoped {
		t.Fatalf("GitHubLoginCompleteResponse dropped auth bounds: %+v", gh.Auth)
	}
	oidc := OIDCLoginCompleteResponse{
		Auth:             AuthContext{Mode: AuthModeScoped, TenantID: "t", WorkspaceID: "w"},
		ProviderConfigID: "p",
	}
	if oidc.Auth.TenantID != "t" || oidc.Auth.Mode != AuthModeScoped {
		t.Fatalf("OIDCLoginCompleteResponse dropped auth bounds: %+v", oidc.Auth)
	}
}

// TestSessionRefreshWindowDefaults pins the 15-minute external-proof
// staleness bound both providers share. Lengthening it extends the window a
// revoked external grant keeps working; shortening it churns sessions.
func TestSessionRefreshWindowDefaults(t *testing.T) {
	if DefaultGitHubSessionRefreshWindow != DefaultOIDCSessionRefreshWindow {
		t.Fatalf("refresh windows diverge: github=%v oidc=%v", DefaultGitHubSessionRefreshWindow, DefaultOIDCSessionRefreshWindow)
	}
	const wantMinutes = 15
	if DefaultGitHubSessionRefreshWindow.Minutes() != wantMinutes {
		t.Fatalf("DefaultGitHubSessionRefreshWindow = %v, want 15m", DefaultGitHubSessionRefreshWindow)
	}
}
