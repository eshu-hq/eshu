// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// interfaceMethodSet reflects an interface type into sorted
// "name signature" strings for comparison across package spellings.
func interfaceMethodSet(rt reflect.Type) []string {
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestLoginContractAliasesAreIdentical proves the D13 (#9669) drain moved
// the login contracts without changing them: every root alias names the
// exact auth-owned value, so errors.Is chains, status mappings, and audit
// classifications behave byte-for-byte as before.
func TestLoginContractAliasesAreIdentical(t *testing.T) {
	t.Run("sentinel identity", func(t *testing.T) {
		pairs := [][2]error{
			{ErrGitHubLoginUnavailable, auth.ErrGitHubLoginUnavailable},
			{ErrGitHubLoginInvalidRequest, auth.ErrGitHubLoginInvalidRequest},
			{ErrGitHubLoginDenied, auth.ErrGitHubLoginDenied},
			{ErrOIDCLoginUnavailable, auth.ErrOIDCLoginUnavailable},
			{ErrOIDCLoginInvalidRequest, auth.ErrOIDCLoginInvalidRequest},
			{ErrOIDCLoginDenied, auth.ErrOIDCLoginDenied},
			{ErrBearerCredentialUnrecognized, auth.ErrBearerCredentialUnrecognized},
		}
		for i, p := range pairs {
			if p[0] != p[1] {
				t.Fatalf("pair %d: query sentinel %v is not the auth sentinel %v", i, p[0], p[1])
			}
			if !errors.Is(p[0], p[1]) || !errors.Is(p[1], p[0]) {
				t.Fatalf("pair %d: errors.Is is not symmetric", i)
			}
		}
	})
	t.Run("versions and windows", func(t *testing.T) {
		if GitHubLoginAdmissionContractVersion != auth.GitHubLoginAdmissionContractVersion {
			t.Fatal("GitHubLoginAdmissionContractVersion drifted from auth")
		}
		if OIDCLoginAdmissionContractVersion != auth.OIDCLoginAdmissionContractVersion {
			t.Fatal("OIDCLoginAdmissionContractVersion drifted from auth")
		}
		if DefaultGitHubSessionRefreshWindow != auth.DefaultGitHubSessionRefreshWindow {
			t.Fatal("DefaultGitHubSessionRefreshWindow drifted from auth")
		}
		if DefaultOIDCSessionRefreshWindow != auth.DefaultOIDCSessionRefreshWindow {
			t.Fatal("DefaultOIDCSessionRefreshWindow drifted from auth")
		}
		if setupRecoveryCodeCount != auth.MFARecoveryCodeCount || setupRecoveryCodeBytes != auth.MFARecoveryCodeBytes {
			t.Fatal("recovery-code shape drifted from auth")
		}
	})
	t.Run("interface identity", func(t *testing.T) {
		// The aliases must name the identical method sets in both
		// spellings. Reflecting both sides (instead of a var _ assertion,
		// which QF1011 would strip to a no-op) proves the drain kept every
		// admission and resolver method exactly.
		pairs := [][2]reflect.Type{
			{reflect.TypeOf((*GitHubLoginAdmission)(nil)).Elem(), reflect.TypeOf((*auth.GitHubLoginAdmission)(nil)).Elem()},
			{reflect.TypeOf((*GitHubLoginService)(nil)).Elem(), reflect.TypeOf((*auth.GitHubLoginService)(nil)).Elem()},
			{reflect.TypeOf((*OIDCLoginAdmission)(nil)).Elem(), reflect.TypeOf((*auth.OIDCLoginAdmission)(nil)).Elem()},
			{reflect.TypeOf((*OIDCLoginService)(nil)).Elem(), reflect.TypeOf((*auth.OIDCLoginService)(nil)).Elem()},
			{reflect.TypeOf((*ScopedTokenResolver)(nil)).Elem(), reflect.TypeOf((*auth.ScopedTokenResolver)(nil)).Elem()},
			{reflect.TypeOf((*BrowserSessionResolver)(nil)).Elem(), reflect.TypeOf((*auth.BrowserSessionResolver)(nil)).Elem()},
			{reflect.TypeOf((*GitHubProviderLister)(nil)).Elem(), reflect.TypeOf((*auth.GitHubProviderLister)(nil)).Elem()},
			{reflect.TypeOf((*OIDCProviderLister)(nil)).Elem(), reflect.TypeOf((*auth.OIDCProviderLister)(nil)).Elem()},
		}
		for _, p := range pairs {
			got := interfaceMethodSet(p[0])
			want := interfaceMethodSet(p[1])
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("method sets diverge: query=%v auth=%v", got, want)
			}
			if len(got) == 0 {
				t.Fatalf("empty method set for %v", p[0])
			}
		}
	})
	t.Run("denial reason forwarder parity", func(t *testing.T) {
		errs := []error{
			nil,
			errors.New("boom"),
			ErrGitHubLoginDenied,
			&SSOLoginDeniedError{Sentinel: ErrOIDCLoginDenied, Reason: "no_grants"},
			&SSOLoginDeniedError{Reason: "state_invalid"},
		}
		for _, err := range errs {
			if got, want := SSOLoginDenialReason(err, "fb"), auth.SSOLoginDenialReason(err, "fb"); got != want {
				t.Fatalf("SSOLoginDenialReason(%v) = %q, auth gives %q", err, got, want)
			}
		}
	})
	t.Run("recovery generation delegation parity", func(t *testing.T) {
		zeros := func(b []byte) (int, error) { return len(b), nil }
		h := &SetupHandler{readEntropy: zeros}
		got, err := h.generateRecoveryCodes()
		if err != nil {
			t.Fatalf("generateRecoveryCodes error = %v", err)
		}
		want, err := auth.GenerateMFARecoveryCodes(zeros)
		if err != nil {
			t.Fatalf("auth.GenerateMFARecoveryCodes error = %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("handler delegation diverged from auth contract: %v vs %v", got, want)
		}
	})
}
