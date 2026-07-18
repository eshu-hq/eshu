// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
)

// isJWTShaped reports whether credential has the compact JWT shape (exactly
// three non-empty, dot-separated segments). It never allocates: Eshu-issued
// scoped tokens are opaque hex/base64 strings with no dots, so this is the
// cheap, allocation-free gate that lets a token-only deployment's request
// path skip every other check in this package (the zero-provider fast path
// in cache.go handles the "no bearer IdP configured" case; this handles "the
// presented credential was never a JWT at all").
func isJWTShaped(credential string) bool {
	segments := 0
	segmentLen := 0
	for i := 0; i < len(credential); i++ {
		if credential[i] == '.' {
			if segmentLen == 0 {
				return false
			}
			segments++
			segmentLen = 0
			continue
		}
		segmentLen++
	}
	if segmentLen == 0 {
		return false
	}
	return segments == 2
}

// unverifiedIssuer struct-decodes only the "iss" field.
type unverifiedIssuer struct {
	Issuer string `json:"iss"`
}

// peekUnverifiedIssuer base64url-decodes a JWT's middle (payload) segment
// and reads its "iss" claim WITHOUT verifying the token's signature. This is
// routing only: it decides which enabled provider's real verifier to call,
// exactly as the locked design requires (step 3). Nothing about the returned
// issuer is trusted until entry.verifier.Verify succeeds.
func peekUnverifiedIssuer(credential string) (string, error) {
	parts := strings.Split(credential, ".")
	if len(parts) != 3 {
		return "", errors.New("oidcbearer: credential is not JWT-shaped")
	}
	payload, err := decodeSegment(parts[1])
	if err != nil {
		return "", fmt.Errorf("oidcbearer: decode unverified payload segment: %w", err)
	}
	var claims unverifiedIssuer
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("oidcbearer: decode unverified issuer claim: %w", err)
	}
	return strings.TrimSpace(claims.Issuer), nil
}

// decodeSegment decodes one JWT compact-serialization segment, trying the
// unpadded base64url encoding real-world JWTs use before falling back to the
// padded form for interoperability.
func decodeSegment(segment string) ([]byte, error) {
	if decoded, err := base64.RawURLEncoding.DecodeString(segment); err == nil {
		return decoded, nil
	}
	decoded, err := base64.URLEncoding.DecodeString(segment)
	if err != nil {
		return nil, fmt.Errorf("oidcbearer: decode base64url segment: %w", err)
	}
	return decoded, nil
}

// verifiedClaims is the subset of verified claims the resolver needs to map
// to an AuthContext.
type verifiedClaims struct {
	Subject string
	Groups  []string
}

// extractVerifiedClaims decodes a verified *oidc.IDToken's claims into the
// fields the resolver maps to an AuthContext, honoring provider-configured
// subject/groups claim names exactly like oidclogin's connector does for
// interactive login.
func extractVerifiedClaims(token *oidc.IDToken, subjectClaim, groupsClaim string) (verifiedClaims, error) {
	raw := map[string]any{}
	if err := token.Claims(&raw); err != nil {
		return verifiedClaims{}, fmt.Errorf("oidcbearer: decode verified claims: %w", err)
	}
	subject := token.Subject
	if subjectClaim != "" && subjectClaim != "sub" {
		subject = stringClaim(raw, subjectClaim)
	}
	claimName := groupsClaim
	if claimName == "" {
		claimName = "groups"
	}
	return verifiedClaims{
		Subject: strings.TrimSpace(subject),
		Groups:  stringSliceClaim(raw, claimName),
	}, nil
}

func stringClaim(claims map[string]any, name string) string {
	value, ok := claims[strings.TrimSpace(name)]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func stringSliceClaim(claims map[string]any, name string) []string {
	value, ok := claims[strings.TrimSpace(name)]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return cleanStrings(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		return cleanStrings(values)
	case string:
		return cleanStrings(strings.Split(typed, ","))
	default:
		return nil
	}
}

func cleanStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	sort.Strings(cleaned)
	return cleaned
}

// hashStrings maps oidclogin.SHA256Hash over the cleaned, deduplicated,
// sorted value set, matching oidclogin's own unexported hashStrings exactly
// (group hashes must be stable and order-independent for
// GrantQuery.GroupHashes to be a reliable cache/lookup key downstream, and
// must match the hash function the interactive login path uses for the same
// groups so AC #3's grant equivalence holds).
func hashStrings(values []string) []string {
	cleaned := cleanStrings(values)
	hashes := make([]string, 0, len(cleaned))
	for _, value := range cleaned {
		hashes = append(hashes, oidclogin.SHA256Hash(value))
	}
	return hashes
}

// classifyVerifyError maps a go-oidc (*oidc.IDTokenVerifier).Verify error to
// one of this package's bounded telemetry outcomes. Classification is based
// on go-oidc v3.19.0's actual error shapes (verified against its source
// under go/pkg/mod/github.com/coreos/go-oidc/v3@v3.19.0/oidc/verify.go and
// jwks.go, not guessed):
//
//   - *oidc.TokenExpiredError                                -> expired
//   - "oidc: expected audience ..."                          -> wrong_audience
//   - "oidc: id token issued by a different provider ..."     -> unknown_issuer
//   - a JWKS-fetch error ("fetching keys", "get keys failed") -> jwks_fetch_failure
//   - any other signature failure                            -> bad_signature
//   - anything else (malformed JWT, unmarshal failure, etc.)  -> malformed
func classifyVerifyError(err error) string {
	if err == nil {
		return outcomeValid
	}
	var expired *oidc.TokenExpiredError
	if errors.As(err, &expired) {
		return outcomeExpired
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "expected audience"):
		return outcomeWrongAudience
	case strings.Contains(msg, "issued by a different provider"):
		return outcomeUnknownIssuer
	case strings.Contains(msg, "fetching keys"), strings.Contains(msg, "get keys failed"):
		return outcomeJWKSFetchFailure
	case strings.Contains(msg, "failed to verify signature"), strings.Contains(msg, "failed to verify id token signature"):
		return outcomeBadSignature
	default:
		return outcomeMalformed
	}
}
