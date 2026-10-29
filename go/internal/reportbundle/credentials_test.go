// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"errors"
	"strings"
	"testing"
)

// TestCheckTargetCredentials pins the shared refusal predicate the offline
// command and the S4 capture endpoint both run: authority userinfo (in every
// spelling net/url recognizes) and unparseable targets are refused; "@"
// inside path segments, scheme-less relatives, and empty targets pass.
func TestCheckTargetCredentials(t *testing.T) {
	t.Parallel()
	const secret = "UNIT-SENTINEL-9d4c2a"
	cases := []struct {
		name    string
		target  string
		refused bool
	}{
		{"empty passes", "", false},
		{"plain path passes", "/api/v0/search/code", false},
		{"query params pass", "/api/v0/search/code?q=x", false},
		{"at in path segment passes", "/api/v0/owners/dev@example.com/services", false},
		{"userinfo refused", "https://svc:" + secret + "@h.internal/tool", true},
		{"userinfo password-only refused", "https://:" + secret + "@h.internal/tool", true},
		{"userinfo scheme-relative refused", "//svc:" + secret + "@h.internal/tool", true},
		{"userinfo username-only refused", "https://" + secret + "@h.internal/tool", true},
		{"no scheme authority refused", "svc:" + secret + "@h.internal:5432/tool", true},
		// Unparseable query strings are Capture's refusal (SplitTargetQuery),
		// not this predicate's: without userinfo there is nothing to refuse
		// here, and Capture fails closed on the split.
		{"bad escape without userinfo passes here", "/api/v0/x?bad=%zz", false},
		{"space in target refused", "https://svc:pw word@h.internal/tool", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := CheckTargetCredentials(tc.target)
			if tc.refused && err == nil {
				t.Fatalf("CheckTargetCredentials(%q) = nil, want refusal", tc.target)
			}
			if !tc.refused && err != nil {
				t.Fatalf("CheckTargetCredentials(%q) = %v, want nil", tc.target, err)
			}
			if err != nil {
				var refusal *CredentialRefusal
				if !errors.As(err, &refusal) {
					t.Fatalf("error type = %T, want *CredentialRefusal", err)
				}
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("refusal echoes the secret: %q", err.Error())
				}
			}
		})
	}
}
