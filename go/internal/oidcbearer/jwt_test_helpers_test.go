// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
)

// testIdP is a hermetic (no-network) stand-in for one IdP's signing key,
// used to build both signed access tokens and the VerifierFactory that
// validates them via oidc.StaticKeySet — the real go-oidc Verify path, with
// zero network access, per this package's TDD contract.
type testIdP struct {
	key *rsa.PrivateKey
}

func newTestIdP(t testing.TB) *testIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return &testIdP{key: key}
}

// verifierFactory returns a VerifierFactory that ignores issuerURL discovery
// entirely and always returns a verifier backed by this IdP's static public
// key, checking the given audience. It counts calls so tests can assert the
// zero-provider and verifier-reuse contracts.
func (idp *testIdP) verifierFactory(calls *int) VerifierFactory {
	return func(_ context.Context, issuerURL, audience string) (*oidc.IDTokenVerifier, error) {
		if calls != nil {
			*calls++
		}
		keySet := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&idp.key.PublicKey}}
		return oidc.NewVerifier(issuerURL, keySet, &oidc.Config{ClientID: audience}), nil
	}
}

// tokenClaims is the small set of claims these tests need to vary.
type tokenClaims struct {
	issuer   string
	audience string
	subject  string
	groups   []string
	issuedAt time.Time
	expiry   time.Time
}

func defaultTokenClaims(issuer, audience string) tokenClaims {
	now := time.Now()
	return tokenClaims{
		issuer:   issuer,
		audience: audience,
		subject:  "user-1",
		groups:   []string{"engineering"},
		issuedAt: now.Add(-time.Minute),
		expiry:   now.Add(time.Hour),
	}
}

// sign produces a compact RS256 JWT. badSignature, when true, signs with an
// unrelated key so the verifier's signature check must fail — the
// distinct-outcome case classifyVerifyError maps to bad_signature.
func (idp *testIdP) sign(t testing.TB, claims tokenClaims, badSignature bool) string {
	t.Helper()
	mapClaims := jwt.MapClaims{
		"iss": claims.issuer,
		"aud": claims.audience,
		"sub": claims.subject,
		"iat": claims.issuedAt.Unix(),
		"exp": claims.expiry.Unix(),
	}
	if claims.groups != nil {
		mapClaims["groups"] = claims.groups
	}
	signingKey := idp.key
	if badSignature {
		otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate mismatched rsa key: %v", err)
		}
		signingKey = otherKey
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, mapClaims).SignedString(signingKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}
