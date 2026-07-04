// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/api/idtoken"
)

// gcpPushOIDCValidator verifies a Google-signed Pub/Sub push OIDC ID token and
// returns the claims verifyGCPPushOIDC needs. Implementations MUST verify the
// token's cryptographic signature against Google's public certs and the
// audience claim; they MUST NOT accept an unsigned or unverified token.
//
// The interface exists so tests can inject a fake that never makes a network
// call to Google — googleOIDCValidator (the real, production implementation)
// is the only implementation that talks to Google.
type gcpPushOIDCValidator interface {
	// ValidateGCPPushToken validates idToken against audience and returns the
	// token's email and email_verified claims on success. It MUST fail closed:
	// any verification error (bad signature, expired, wrong audience,
	// malformed) returns a non-nil error and the caller must treat that as
	// unauthenticated.
	ValidateGCPPushToken(ctx context.Context, idToken string, audience string) (email string, emailVerified bool, err error)
}

// googleOIDCValidator is the production gcpPushOIDCValidator. It delegates to
// google.golang.org/api/idtoken, which verifies the token's signature against
// Google's published JWKS (fetched and cached over HTTPS) and checks the
// audience claim. It never logs or persists the raw token.
type googleOIDCValidator struct{}

// googleOIDCIssuers is the closed set of issuer claim values Google mints
// service-account-signed OIDC tokens under (Pub/Sub push, IAM, and other
// Google-signed ID tokens all use one of these two forms). idtoken.Validate
// verifies signature, audience, and expiry, but NOT the issuer — Google's own
// Pub/Sub authenticated-push sample explicitly checks payload.Issuer after
// validation, because a syntactically valid Google-signed token minted for an
// unrelated purpose would otherwise pass every other check. Rejecting any
// other issuer keeps this verification scoped to genuine Google-minted tokens.
var googleOIDCIssuers = map[string]bool{
	"accounts.google.com":         true,
	"https://accounts.google.com": true,
}

// ValidateGCPPushToken implements gcpPushOIDCValidator using Google's official
// idtoken verifier. See idtoken.Validate for the exact verification the
// upstream library performs (RS256/ES256 signature check against Google's
// certs, audience match, expiry); the issuer claim is checked afterward via
// validateGoogleOIDCPayload since idtoken.Validate does not check it.
func (googleOIDCValidator) ValidateGCPPushToken(
	ctx context.Context,
	idToken string,
	audience string,
) (string, bool, error) {
	payload, err := idtoken.Validate(ctx, idToken, audience)
	if err != nil {
		return "", false, fmt.Errorf("validate GCP push OIDC token: %w", err)
	}
	return validateGoogleOIDCPayload(payload)
}

// validateGoogleOIDCPayload checks the issuer claim on an already
// signature/audience/expiry-verified idtoken.Payload and extracts the
// email/email_verified claims. Split out from ValidateGCPPushToken so the
// issuer check has direct unit-test coverage without requiring a live,
// Google-signed token (idtoken.Validate itself always calls out to Google's
// cert endpoints, so it cannot be exercised hermetically).
func validateGoogleOIDCPayload(payload *idtoken.Payload) (string, bool, error) {
	if payload == nil || !googleOIDCIssuers[payload.Issuer] {
		return "", false, fmt.Errorf("validate GCP push OIDC token: unexpected issuer")
	}
	email, _ := payload.Claims["email"].(string)
	emailVerified, _ := payload.Claims["email_verified"].(bool)
	return email, emailVerified, nil
}

// verifyGCPPushOIDC reports whether r carries a Google-signed Pub/Sub push
// OIDC token that verifies against audience and whose email claim matches
// allowedServiceAccountEmail with email_verified=true. It fails closed: a
// missing token, a validator error, a wrong audience, a non-matching or
// unverified email, or a nil validator/unconfigured audience/allowlist all
// return false. The token itself is never logged.
func verifyGCPPushOIDC(
	ctx context.Context,
	r *http.Request,
	validator gcpPushOIDCValidator,
	audience string,
	allowedServiceAccountEmail string,
) bool {
	audience = strings.TrimSpace(audience)
	allowedServiceAccountEmail = strings.TrimSpace(allowedServiceAccountEmail)
	if validator == nil || audience == "" || allowedServiceAccountEmail == "" {
		return false
	}
	token := gcpFreshnessBearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return false
	}
	email, emailVerified, err := validator.ValidateGCPPushToken(ctx, token, audience)
	if err != nil {
		return false
	}
	if !emailVerified {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(email), allowedServiceAccountEmail)
}
