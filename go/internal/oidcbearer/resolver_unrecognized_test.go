// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestResolveScopedToken_UnrecognizedSentinel_OnlyPreMatch proves the issue
// #5163 (F-2) §C sentinel contract: deny wraps
// query.ErrBearerCredentialUnrecognized for exactly the PRE-match outcomes —
// a JWT unparseable before verification (outcomeMalformed at the peek site)
// and a JWT whose issuer is not in the active snapshot (outcomeUnknownIssuer)
// — and NEVER for a POST-match denial (expired, bad signature, wrong audience,
// no grants). The distinction cannot be read from the outcome string alone
// (outcomeMalformed is reused post-verify), so this guards the explicit
// per-call-site flag. If the flag regressed, the augment-vs-bare WWW-Authenticate
// decision in internal/query would leak a discovery challenge on a credential
// that was actually understood (or hide it from one that was not).
func TestResolveScopedToken_UnrecognizedSentinel_OnlyPreMatch(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)

	cases := []struct {
		name             string
		token            func(t *testing.T) string
		grantResolver    *fakeGrantResolver
		wantUnrecognized bool
	}{
		{
			name: "pre_parse_malformed_is_unrecognized",
			// Three dot segments (passes isJWTShaped) but an unparseable
			// payload segment, so peekUnverifiedIssuer fails before any match.
			token:            func(*testing.T) string { return "aaaa.bbbb.cccc" },
			grantResolver:    testGrantResolver(),
			wantUnrecognized: true,
		},
		{
			name: "unknown_issuer_is_unrecognized",
			token: func(t *testing.T) string {
				return idp.sign(t, defaultTokenClaims("https://not-enabled.example.test", testAudience), false)
			},
			grantResolver:    testGrantResolver(),
			wantUnrecognized: true,
		},
		{
			name: "expired_is_recognized",
			token: func(t *testing.T) string {
				claims := defaultTokenClaims(testIssuer, testAudience)
				claims.issuedAt = claims.expiry.Add(-2 * time.Hour)
				claims.expiry = claims.issuedAt.Add(time.Minute)
				return idp.sign(t, claims, false)
			},
			grantResolver:    testGrantResolver(),
			wantUnrecognized: false,
		},
		{
			name: "bad_signature_is_recognized",
			token: func(t *testing.T) string {
				return idp.sign(t, defaultTokenClaims(testIssuer, testAudience), true)
			},
			grantResolver:    testGrantResolver(),
			wantUnrecognized: false,
		},
		{
			name: "wrong_audience_is_recognized",
			token: func(t *testing.T) string {
				return idp.sign(t, defaultTokenClaims(testIssuer, "https://wrong.example.test"), false)
			},
			grantResolver:    testGrantResolver(),
			wantUnrecognized: false,
		},
		{
			name: "no_grants_is_recognized",
			token: func(t *testing.T) string {
				claims := defaultTokenClaims(testIssuer, testAudience)
				claims.groups = nil
				return idp.sign(t, claims, false)
			},
			grantResolver:    testGrantResolver(),
			wantUnrecognized: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resolver, _ := newTestResolver(t, idp, []BearerProvider{testProvider()}, tc.grantResolver, nil)

			_, ok, err := resolver.ResolveScopedToken(context.Background(), tc.token(t))
			if ok {
				t.Fatal("ResolveScopedToken() ok = true, want false (denied)")
			}
			if err == nil {
				t.Fatal("ResolveScopedToken() err = nil, want a denial error")
			}
			if got := errors.Is(err, query.ErrBearerCredentialUnrecognized); got != tc.wantUnrecognized {
				t.Fatalf("errors.Is(err, ErrBearerCredentialUnrecognized) = %v, want %v (err=%v)", got, tc.wantUnrecognized, err)
			}
		})
	}
}
