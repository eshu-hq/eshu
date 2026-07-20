// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"strings"
)

// oauthChallengeContextKey is the unexported context key carrying an
// OAuthChallengePolicy across exactly one call boundary: from
// authMiddlewareWithRoutePolicy's genuine bearer-credential-denial paths to
// unauthorizedResponse (issue #5163, F-2). Using context here — instead of
// adding an OAuthChallengePolicy parameter to unauthorizedResponse itself —
// keeps that function's signature, and therefore its ~20 other call sites
// across browser_session_handler.go, saml_handler.go, profile_handler.go,
// local_identity_api_tokens*.go, browser_session_list.go, and
// local_identity_totp.go, completely unchanged. Those call sites build their
// own plain *http.Request (never wrapped by requestWithOAuthChallenge), so
// this key is structurally absent there and their 401s can never carry the
// OAuth bearer challenge — a cookie/console 401 is not the resource this
// challenge targets.
type oauthChallengeContextKey struct{}

// requestWithOAuthChallenge returns r wrapped with policy attached to its
// context, or r unchanged when policy is nil (avoiding a pointless context
// allocation on every 401 in the common today's-behavior case: no
// OAuthChallengePolicy wired at all).
func requestWithOAuthChallenge(r *http.Request, policy OAuthChallengePolicy) *http.Request {
	if policy == nil {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), oauthChallengeContextKey{}, policy))
}

// oauthChallengePolicyFromContext returns the OAuthChallengePolicy
// requestWithOAuthChallenge attached to ctx, if any.
func oauthChallengePolicyFromContext(ctx context.Context) (OAuthChallengePolicy, bool) {
	policy, ok := ctx.Value(oauthChallengeContextKey{}).(OAuthChallengePolicy)
	return policy, ok
}

// oauthWWWAuthenticateChallenge builds the WWW-Authenticate header value for a
// 401 (issue #5163, F-2). It returns the bare "Bearer" challenge — byte-for-byte
// today's value — for a nil policy, a policy that reports OAuth is not enabled
// (ok=false), or a policy that reports enabled but supplies no metadata URL.
// Only when the policy returns ok=true with a non-empty metadata URL does it
// append the RFC 9728 resource_metadata directive (and, when non-empty, the RFC
// 6750 scope directive) so a discovery-capable client is steered to the
// protected-resource document. metadataURL and scope are supplied by wiring
// from operator config (ESHU_AUTH_RESOURCE URI-derived), validated for quote
// and control characters at wiring time, so they are safe to embed in the
// quoted-string header value here.
func oauthWWWAuthenticateChallenge(ctx context.Context, policy OAuthChallengePolicy) string {
	const bare = "Bearer"
	if policy == nil {
		return bare
	}
	metadataURL, scope, ok := policy.OAuthChallenge(ctx)
	if !ok || strings.TrimSpace(metadataURL) == "" {
		return bare
	}
	challenge := bare + ` resource_metadata="` + metadataURL + `"`
	if strings.TrimSpace(scope) != "" {
		challenge += `, scope="` + scope + `"`
	}
	return challenge
}

// oauthWWWAuthenticateChallengeForRequest resolves the OAuthChallengePolicy
// (if any) attached to ctx and delegates to oauthWWWAuthenticateChallenge to
// build the WWW-Authenticate header value. Absent context (the ~20 non-bearer
// call sites; see oauthChallengeContextKey's doc comment) behaves identically
// to a nil policy: the bare "Bearer" challenge.
func oauthWWWAuthenticateChallengeForRequest(ctx context.Context) string {
	policy, ok := oauthChallengePolicyFromContext(ctx)
	if !ok {
		return oauthWWWAuthenticateChallenge(ctx, nil)
	}
	return oauthWWWAuthenticateChallenge(ctx, policy)
}
