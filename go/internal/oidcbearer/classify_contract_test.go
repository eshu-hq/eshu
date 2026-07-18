// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"testing"
	"time"
)

// TestClassifyVerifyErrorMatchesGoOIDCContract feeds classifyVerifyError the
// actual errors the pinned go-oidc version (v3.19.0) returns from Verify,
// rather than hand-built error strings, so a go-oidc bump that changes those
// strings fails here instead of silently skewing the
// eshu_dp_oidc_bearer_validation_total{outcome} telemetry. classifyVerifyError
// only labels an already-failed verification, so a wrong label never weakens
// enforcement (denied stays denied), but an operator relies on these outcomes
// to tell an expired token from a wrong-audience or bad-signature one.
//
// The expired case is matched structurally (errors.As on
// *oidc.TokenExpiredError) and is robust to string changes; the audience,
// issuer, and signature cases are the substring-matched ones this test exists
// to protect.
func TestClassifyVerifyErrorMatchesGoOIDCContract(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	factory := idp.verifierFactory(nil)
	verifier, err := factory(context.Background(), testIssuer, testAudience)
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	ctx := context.Background()

	cases := []struct {
		name  string
		token string
		want  string
	}{
		{
			name: "expired",
			token: idp.sign(t, func() tokenClaims {
				c := defaultTokenClaims(testIssuer, testAudience)
				c.issuedAt = time.Now().Add(-2 * time.Hour)
				c.expiry = time.Now().Add(-time.Hour)
				return c
			}(), false),
			want: outcomeExpired,
		},
		{
			name:  "wrong_audience",
			token: idp.sign(t, defaultTokenClaims(testIssuer, "https://someone-elses-resource.example"), false),
			want:  outcomeWrongAudience,
		},
		{
			name:  "unknown_issuer",
			token: idp.sign(t, defaultTokenClaims("https://a-different-issuer.example", testAudience), false),
			want:  outcomeUnknownIssuer,
		},
		{
			name:  "bad_signature",
			token: idp.sign(t, defaultTokenClaims(testIssuer, testAudience), true),
			want:  outcomeBadSignature,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, verr := verifier.Verify(ctx, tc.token)
			if verr == nil {
				t.Fatalf("Verify() unexpectedly succeeded for the %s case", tc.name)
			}
			if got := classifyVerifyError(verr); got != tc.want {
				t.Fatalf("classifyVerifyError(%q) = %q, want %q — go-oidc's error text for this case may have changed", verr, got, tc.want)
			}
		})
	}
}
