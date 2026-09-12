// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// OAuthChallengePolicy supplies the per-request RFC 9728/RFC 6750 OAuth
// challenge parameters a 401's WWW-Authenticate header adds (issue #5163,
// F-2): the protected-resource-metadata document URL and the scope string a
// client should request. Implementations must derive "is OAuth enabled" from
// the SAME posture root package query's DeriveAuthPosture computes (provider
// rows + sign-in policy) so the challenge and OAuthProtectedResourceHandler's
// own enablement gate never disagree -- see root's PostureOAuthChallengePolicy,
// which stays in root and implements this interface unchanged.
// ok=false (or an empty metadataURL) leaves the challenge exactly the
// pre-#5163 bare "Bearer" -- the safe default for a nil policy, a
// posture-derivation error, and a token-only deployment alike.
//
// Moved from root package query's auth_oauth_discovery.go (#6642) as a
// types-only hoist alongside WriteUnauthorized, which needs it: only the
// interface moved. PostureOAuthChallengePolicy, DeriveAuthPosture, and
// OAuthProtectedResourceHandler all stay in root untouched; root keeps a
// type alias here so cmd/mcp-server and auth_constructors.go compile
// unchanged.
type OAuthChallengePolicy interface {
	OAuthChallenge(ctx context.Context) (metadataURL, scope string, ok bool)
}

// oauthChallengeContextKey is the unexported context key carrying an
// OAuthChallengePolicy across exactly one call boundary: from root package
// query's authMiddlewareWithRoutePolicy's genuine bearer-credential-denial
// paths (via RequestWithOAuthChallenge) to WriteUnauthorized (issue #5163,
// F-2). Using context here -- instead of adding an OAuthChallengePolicy
// parameter to WriteUnauthorized itself -- keeps that function's signature,
// and therefore its root call sites across browser_session_handler.go,
// saml_handler.go, profile_handler.go, local_identity_api_tokens*.go,
// browser_session_list.go, and local_identity_totp.go, completely
// unchanged. Those call sites build their own plain *http.Request (never
// wrapped by RequestWithOAuthChallenge), so this key is structurally absent
// there and their 401s can never carry the OAuth bearer challenge -- a
// cookie/console 401 is not the resource this challenge targets.
type oauthChallengeContextKey struct{}

// RequestWithOAuthChallenge returns r wrapped with policy attached to its
// context, or r unchanged when policy is nil (avoiding a pointless context
// allocation on every 401 in the common today's-behavior case: no
// OAuthChallengePolicy wired at all). Moved from root package query's
// auth_oauth_challenge_context.go (requestWithOAuthChallenge, #6642); root
// keeps a thin forwarder at the original call site.
func RequestWithOAuthChallenge(r *http.Request, policy OAuthChallengePolicy) *http.Request {
	if policy == nil {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), oauthChallengeContextKey{}, policy))
}

// oauthChallengePolicyFromContext returns the OAuthChallengePolicy
// RequestWithOAuthChallenge attached to ctx, if any.
func oauthChallengePolicyFromContext(ctx context.Context) (OAuthChallengePolicy, bool) {
	policy, ok := ctx.Value(oauthChallengeContextKey{}).(OAuthChallengePolicy)
	return policy, ok
}

// oauthWWWAuthenticateChallenge builds the WWW-Authenticate header value for
// a 401 (issue #5163, F-2). It returns the bare "Bearer" challenge --
// byte-for-byte today's value -- for a nil policy, a policy that reports
// OAuth is not enabled (ok=false), or a policy that reports enabled but
// supplies no metadata URL. Only when the policy returns ok=true with a
// non-empty metadata URL does it append the RFC 9728 resource_metadata
// directive (and, when non-empty, the RFC 6750 scope directive) so a
// discovery-capable client is steered to the protected-resource document.
// metadataURL and scope are supplied by root's wiring from operator config
// (ESHU_AUTH_RESOURCE URI-derived), validated for quote and control
// characters at wiring time, so they are safe to embed in the quoted-string
// header value here.
func oauthWWWAuthenticateChallenge(ctx context.Context, policy OAuthChallengePolicy) string {
	const bare = "Bearer"
	if policy == nil {
		return bare
	}
	metadataURL, scope, ok := policy.OAuthChallenge(ctx)
	if !ok || strings.TrimSpace(metadataURL) == "" {
		return bare
	}
	// Defense in depth: the metadata URL is already validated at wiring time
	// (root's oauthMetadataURL rejects quotes and control chars, including
	// percent-encoded ones). Re-check here so a future policy that mints an
	// unvalidated URL can never inject a quote/CRLF into this header value; on
	// any delimiter, degrade to a bare challenge rather than emit a broken one.
	if headerValueHasDelimiter(metadataURL) || headerValueHasDelimiter(scope) {
		return bare
	}
	challenge := bare + ` resource_metadata="` + metadataURL + `"`
	if strings.TrimSpace(scope) != "" {
		challenge += `, scope="` + scope + `"`
	}
	return challenge
}

// headerValueHasDelimiter reports whether s contains a double quote or an
// ASCII control character (CR, LF, NUL, DEL, etc.) that would break out of a
// quoted HTTP header parameter value or split the header.
func headerValueHasDelimiter(s string) bool {
	for _, r := range s {
		if r == '"' || r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// OAuthWWWAuthenticateChallengeForRequest resolves the OAuthChallengePolicy
// (if any) attached to ctx and delegates to oauthWWWAuthenticateChallenge to
// build the WWW-Authenticate header value. Absent context (the ~20 non-bearer
// call sites; see oauthChallengeContextKey's doc comment) behaves identically
// to a nil policy: the bare "Bearer" challenge. Moved from root package
// query's auth_oauth_challenge_context.go
// (oauthWWWAuthenticateChallengeForRequest, #6642); its only caller,
// WriteUnauthorized, moved with it, so root keeps no forwarder.
func OAuthWWWAuthenticateChallengeForRequest(ctx context.Context) string {
	policy, ok := oauthChallengePolicyFromContext(ctx)
	if !ok {
		return oauthWWWAuthenticateChallenge(ctx, nil)
	}
	return oauthWWWAuthenticateChallenge(ctx, policy)
}

// DocumentationCorrelationID returns a correlation ID for r: the
// X-Correlation-ID or X-Request-ID request header when present, or a random
// 16-byte hex identifier otherwise (falling back to a nanosecond timestamp
// if the random read itself fails). Moved from root package query's
// documentation.go (documentationCorrelationID, #6642) as a stdlib-only leaf
// dependency of WriteUnauthorized; root keeps a thin forwarder at the
// original call site for its other callers (auth_audit.go,
// sign_in_policy_mutations.go, documentation.go itself).
func DocumentationCorrelationID(r *http.Request) string {
	for _, header := range []string{"X-Correlation-ID", "X-Request-ID"} {
		if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
			return value
		}
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	}
	return hex.EncodeToString(raw[:])
}

// WriteUnauthorized writes a 401 JSON error response. Its WWW-Authenticate
// header is the bare "Bearer" challenge unless an OAuthChallengePolicy was
// attached to the request context by RequestWithOAuthChallenge at a genuine
// bearer-credential denial site (issue #5163, F-2) in root package query's
// authMiddlewareWithRoutePolicy; the ~20 handler-level call sites that build
// their own plain *http.Request never carry that context and so always get
// the bare challenge.
//
// Moved from root package query's auth.go (unauthorizedResponse, #6642),
// body byte-for-byte preserved: the OAuth-challenge lookup and
// DocumentationCorrelationID moved with it, since both are internal to this
// function's behavior. Root keeps a one-line forwarder at the original call
// site so its ~20 existing callers are unchanged.
func WriteUnauthorized(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", OAuthWWWAuthenticateChallengeForRequest(r.Context()))
	if AcceptsEnvelope(r) {
		WriteJSON(w, http.StatusUnauthorized, ResponseEnvelope{Error: &ErrorEnvelope{
			Code:          ErrorCodeUnauthenticated,
			Message:       "authentication is required",
			CorrelationID: DocumentationCorrelationID(r),
		}})
		return
	}
	WriteJSON(w, http.StatusUnauthorized, map[string]string{
		"error_code":     string(ErrorCodeUnauthenticated),
		"message":        "authentication is required",
		"correlation_id": DocumentationCorrelationID(r),
	})
}
