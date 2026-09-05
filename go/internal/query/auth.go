// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
)

// publicHTTPPaths lists routes that bypass authentication.
var publicHTTPPaths = map[string]bool{
	"/health":                                true,
	"/healthz":                               true,
	"/readyz":                                true,
	"/metrics":                               true,
	"/admin/status":                          true,
	"/api/v0/health":                         true,
	"/api/v0/docs":                           true,
	"/api/v0/openapi.json":                   true,
	"/api/v0/redoc":                          true,
	"/api/v0/auth/local/login":               true,
	"/api/v0/auth/local/invitations/accept":  true,
	"/api/v0/auth/local/break-glass/session": true,
	// Self-service forced password rotation (issue #4976): a
	// must-change-password credential (the ESHU_ADMIN_USERNAME/PASSWORD
	// [_FILE]-seeded bootstrap admin) never has a session, so this route must
	// bypass AuthMiddleware and rely entirely on RotateLocalIdentityPassword's
	// own current-password (and MFA, when the account has an active factor)
	// re-proof, exactly like /api/v0/auth/local/login above.
	"/api/v0/auth/local/password/rotate": true,
	// First-run setup wizard (#4965): a fresh deployment has no session,
	// bearer token, or prior credential, so these routes must bypass
	// AuthMiddleware and rely entirely on their own bootstrap-credential
	// proof (SetupStore.VerifyBootstrapCredential) plus the permanent
	// SetupStore.SetupNeeded seal check every mutating route re-runs.
	"/api/v0/auth/setup-state": true,
	"/api/v0/auth/setup/claim": true,
	"/api/v0/auth/setup/admin": true,
	"/api/v0/auth/setup/mfa":   true,
}

// AuthMode names the source of an authenticated request context. The type and
// its constants live in queryauth so a handler-family subpackage can read the
// auth context without importing this package. AuthMode has no methods, so this
// alias costs callers nothing.
type AuthMode = queryauth.AuthMode

// Compatibility constants preserve this package's public contract.
const (
	// AuthModeShared identifies the legacy shared bearer-token path.
	AuthModeShared = queryauth.AuthModeShared
	// AuthModeScoped identifies a token resolved through the scoped registry.
	AuthModeScoped = queryauth.AuthModeScoped
	// AuthModeBrowserSession identifies a server-managed dashboard session.
	AuthModeBrowserSession = queryauth.AuthModeBrowserSession
)

const (
	// BrowserSessionCookieName is the host-scoped HttpOnly dashboard session
	// cookie, set only when the Secure attribute is applied. The __Host-
	// prefix (RFC 6265bis) requires Secure, no Domain attribute, and Path=/;
	// browsers reject the cookie outright if Secure is missing.
	BrowserSessionCookieName = "__Host-eshu_session"
	// BrowserSessionCSRFCookieName is the readable host-scoped CSRF cookie,
	// set only when the Secure attribute is applied. See BrowserSessionCookieName.
	BrowserSessionCSRFCookieName = "__Host-eshu_csrf"
	// BrowserSessionCookieNameInsecure is the dashboard session cookie name
	// used only when CookieSecureAuto relaxes Secure for a plain-HTTP
	// loopback origin (#4964). It cannot use the __Host- prefix: a
	// __Host--prefixed cookie sent with Secure=false is invalid per RFC
	// 6265bis and browsers silently drop it, which would reintroduce the
	// exact silent session-loss bug #4964 fixes. Readers must check both
	// this name and BrowserSessionCookieName.
	BrowserSessionCookieNameInsecure = "eshu_session"
	// BrowserSessionCSRFCookieNameInsecure is the readable CSRF cookie name
	// used alongside BrowserSessionCookieNameInsecure. See its doc comment.
	BrowserSessionCSRFCookieNameInsecure = "eshu_csrf"
	// BrowserSessionCSRFHeaderName is required on unsafe dashboard session requests.
	BrowserSessionCSRFHeaderName = "X-Eshu-CSRF"
)

// ErrBrowserSessionCSRFInvalid identifies a failed CSRF proof for a browser
// session. It lets middleware return 403 instead of treating the caller as
// unauthenticated when a session exists but the request is unsafe.
var ErrBrowserSessionCSRFInvalid = errors.New("browser session csrf token invalid")

// ErrBrowserSessionRefreshRequired identifies an OIDC-backed browser session
// whose external-provider proof exceeded the configured staleness window.
var ErrBrowserSessionRefreshRequired = errors.New("browser session refresh required")

// AuthContext carries request-scoped authorization bounds for query handlers.
// It lives in queryauth; this alias keeps every existing reference working,
// including the ones in internal/oidcbearer, internal/scopedtoken and
// internal/ask/engine that name it as query.AuthContext. AuthContext has no
// methods, so the alias is complete.
type AuthContext = queryauth.AuthContext

// ScopedTokenResolver resolves a presented bearer credential into an auth
// context without exposing raw token values to handlers.
type ScopedTokenResolver interface {
	ResolveScopedToken(context.Context, string) (AuthContext, bool, error)
}

// BrowserSessionResolver resolves a session-cookie credential into an auth
// context using only server-side hashes. Raw session and CSRF values are hashed
// by middleware before this interface is called.
type BrowserSessionResolver interface {
	ResolveBrowserSession(
		context.Context,
		string,
		string,
		bool,
		time.Time,
	) (AuthContext, bool, error)
}

// GovernanceAuditAppender records validation-safe governance audit events.
type GovernanceAuditAppender interface {
	Append(context.Context, []governanceaudit.Event) error
}

// AuthContextFromContext returns the authenticated request context, if any.
func AuthContextFromContext(ctx context.Context) (AuthContext, bool) {
	return queryauth.AuthContextFromContext(ctx)
}

// ContextWithAuthContext returns a child context carrying authorization bounds.
//
// This forwards rather than storing under a local key, and that is the whole
// point: the key has exactly one definition, in queryauth, so middleware here
// and a handler family there read and write the same context slot.
func ContextWithAuthContext(ctx context.Context, auth AuthContext) context.Context {
	return queryauth.ContextWithAuthContext(ctx, auth)
}

func authMiddleware(
	token string,
	resolver ScopedTokenResolver,
	sessionResolver BrowserSessionResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
	authEnforcementConfigured bool,
	oauthChallenge OAuthChallengePolicy,
) http.Handler {
	return authMiddlewareWithAllowedReadAudit(
		token, resolver, sessionResolver, next, audit,
		BrowserSessionRoutePolicy{}, authEnforcementConfigured, oauthChallenge, nil,
	)
}

func authMiddlewareWithRoutePolicy(
	token string,
	resolver ScopedTokenResolver,
	sessionResolver BrowserSessionResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
	policy BrowserSessionRoutePolicy,
	authEnforcementConfigured bool,
	oauthChallenge OAuthChallengePolicy,
	allowedAudit GovernanceAuditAppender,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Public paths: skip auth.
		if publicHTTPRoute(r) {
			next.ServeHTTP(w, r)
			return
		}

		authorization := r.Header.Get("Authorization")
		if strings.TrimSpace(authorization) == "" {
			if sessionResolver != nil {
				if tryBrowserSessionAuth(w, r, sessionResolver, next, audit, policy) {
					return
				}
			}
			// Dev-mode open reads apply only when NO explicit auth source is
			// configured. authEnforcementConfigured is the wiring-time
			// predicate (shared key OR scoped-token file OR OIDC bearer
			// audience); it deliberately EXCLUDES the always-wired Postgres
			// identity resolver and the browser-session resolver, both
			// unconditional in production. Counting either would make this
			// constant-true and 401 the documented demo-open reads. The
			// cookie path above self-enforces before this branch, so a
			// cookieless headerless request in the open posture stays open.
			// See the *AndEnforcement constructors and cmd/api +
			// cmd/mcp-server wiring.
			if !authEnforcementConfigured {
				next.ServeHTTP(w, r)
				return
			}
			// Row 1 (issue #5163 decision table): a credential-less request
			// denied under an enforcing posture is exactly the client that
			// should be steered to the RFC 9728 discovery document, so attach
			// the OAuth challenge (a nil policy leaves r unchanged and the 401
			// byte-identical to today).
			recordReadAuthorizationDenied(r, audit)
			unauthorizedResponse(w, requestWithOAuthChallenge(r, oauthChallenge))
			return
		}

		scheme, credentials, found := strings.Cut(authorization, " ")
		if !found || strings.ToLower(strings.TrimSpace(scheme)) != "bearer" {
			// Row 8: a non-Bearer scheme is not a recognized issued token, so
			// augment (a browser sending a cookie never reaches here — the
			// cookie path self-enforces above).
			recordReadAuthorizationDenied(r, audit)
			unauthorizedResponse(w, requestWithOAuthChallenge(r, oauthChallenge))
			return
		}

		credentials = strings.TrimSpace(credentials)
		if credentials == "" {
			// Row 8: an empty Bearer credential is not a recognized issued
			// token, so augment.
			recordReadAuthorizationDenied(r, audit)
			unauthorizedResponse(w, requestWithOAuthChallenge(r, oauthChallenge))
			return
		}

		if resolver != nil {
			auth, ok, err := resolver.ResolveScopedToken(r.Context(), credentials)
			if err != nil {
				// Rows 5/6/7/11: augment ONLY when the resolver signals the
				// credential was never a recognized issued token
				// (ErrBearerCredentialUnrecognized — a JWT whose issuer is not
				// in the active snapshot, or a pre-verify unparseable JWT). A
				// post-match denial (expired, bad signature, wrong audience,
				// malformed verified claims, no grants) or an infra error stays
				// bare: that credential WAS understood, so pointing it at
				// discovery is noise, and a bare 401 on an infra error is the
				// fail-safe against anthropics/claude-code#59467.
				recordBearerResolutionDenied(r, audit, err)
				if errors.Is(err, ErrBearerCredentialUnrecognized) {
					r = requestWithOAuthChallenge(r, oauthChallenge)
				}
				unauthorizedResponse(w, r)
				return
			}
			if ok {
				auth = normalizeAuthContext(auth)
				// #6450 residual item 1: allowlist membership used to be the
				// whole bearer gate, so a bearer carrying AllScopes entered
				// every grant-bound route with its grant predicate inert and
				// read every tenant's rows. scopedBearerRouteDenialReason now
				// holds it to the same route policy the cookie session branch
				// below applies, and says which of the two refusals it is so
				// the audit row is actionable.
				if auth.Mode == AuthModeScoped {
					if reason := scopedBearerRouteDenialReason(r, auth, policy); reason != "" {
						recordScopedRouteAuthorizationDeniedWithReason(r, audit, auth, reason)
						scopedRouteDeniedResponse(w, r)
						return
					}
				}
				// F-9 (#5170): record the ALLOWED decision for this scoped-token
				// or OIDC-bearer read, immediately before dispatch, mirroring the
				// denial recording above. recordScopedReadAuthorized no-ops when
				// allowedAudit is nil (every caller except the mcp-server
				// transport middleware), so this is byte-identical to today for
				// every other constructor.
				recordScopedReadAuthorized(r, allowedAudit, auth)
				next.ServeHTTP(w, r.WithContext(ContextWithAuthContext(r.Context(), auth)))
				return
			}
		}

		if token == "" || !constantTimeEqual(credentials, token) {
			// Row 9: the credential matched no resolver and is not the shared
			// token — an unrecognized opaque (or IdP-less JWT-shaped)
			// credential — so augment. A correct shared token never reaches
			// here; it matches and serves below. (Documented residual
			// #59467 risk: a client that presents a genuinely invalid token
			// still gets steered to discovery here, but that client had no
			// working credential anyway.)
			recordReadAuthorizationDenied(r, audit)
			unauthorizedResponse(w, requestWithOAuthChallenge(r, oauthChallenge))
			return
		}

		next.ServeHTTP(w, r.WithContext(ContextWithAuthContext(r.Context(), sharedAuthContext())))
	})
}

func tryBrowserSessionAuth(
	w http.ResponseWriter,
	r *http.Request,
	resolver BrowserSessionResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
	policy BrowserSessionRoutePolicy,
) bool {
	sessionValue, ok := browserSessionCookieValue(r)
	if !ok {
		return false
	}
	requireCSRF := browserSessionRequiresCSRF(r.Method)
	csrfToken := strings.TrimSpace(r.Header.Get(BrowserSessionCSRFHeaderName))
	auth, ok, err := resolver.ResolveBrowserSession(
		r.Context(),
		BrowserSessionSecretHash(sessionValue),
		BrowserSessionSecretHash(csrfToken),
		requireCSRF,
		time.Now().UTC(),
	)
	if errors.Is(err, ErrBrowserSessionCSRFInvalid) {
		recordReadAuthorizationDenied(r, audit)
		csrfDeniedResponse(w, r)
		return true
	}
	if errors.Is(err, ErrBrowserSessionRefreshRequired) {
		recordReadAuthorizationDeniedWithReason(r, audit, "oidc_session_reauth_required")
		unauthorizedResponse(w, r)
		return true
	}
	if err != nil || !ok {
		recordReadAuthorizationDenied(r, audit)
		unauthorizedResponse(w, r)
		return true
	}
	auth = normalizeBrowserSessionAuthContext(auth)
	if auth.Mode == AuthModeBrowserSession {
		if reason := browserSessionRouteDenialReason(r, auth, policy); reason != "" {
			recordScopedRouteAuthorizationDeniedWithReason(r, audit, auth, reason)
			scopedRouteDeniedResponse(w, r)
			return true
		}
	}
	next.ServeHTTP(w, r.WithContext(ContextWithAuthContext(r.Context(), auth)))
	return true
}

func browserSessionRequiresCSRF(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

// BrowserSessionSecretHash returns the durable hash for a session or CSRF
// secret. It returns an empty string for blank input so missing CSRF headers
// cannot hash into a meaningful value.
func BrowserSessionSecretHash(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeBrowserSessionAuthContext(auth AuthContext) AuthContext {
	auth = normalizeAuthContext(auth)
	if auth.Mode == AuthModeScoped {
		auth.Mode = AuthModeBrowserSession
	}
	return auth
}

func sharedAuthContext() AuthContext {
	return AuthContext{
		Mode:         AuthModeShared,
		SubjectClass: "shared_token",
		AllScopes:    true,
	}
}

// normalizeAuthContext forwards to queryauth.NormalizeAuthContext. The
// implementation moved there for #6060 lane A so the supply-chain hub can
// normalize without importing this package; every existing caller keeps its
// exact behavior through this wrapper.
func normalizeAuthContext(auth AuthContext) AuthContext {
	return queryauth.NormalizeAuthContext(auth)
}

func cleanedAuthStrings(values []string) []string {
	return queryauth.CleanedStrings(values)
}

// constantTimeEqual compares two strings in constant time to prevent timing attacks.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// unauthorizedResponse writes a 401 JSON error response. Its WWW-Authenticate
// header is the bare "Bearer" challenge unless an OAuthChallengePolicy was
// attached to the request context by requestWithOAuthChallenge at a genuine
// bearer-credential denial site (issue #5163, F-2); the ~20 handler-level call
// sites that build their own plain *http.Request never carry that context and
// so always get the bare challenge. See auth_oauth_challenge_context.go.
func unauthorizedResponse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", oauthWWWAuthenticateChallengeForRequest(r.Context()))
	if acceptsEnvelope(r) {
		WriteJSON(w, http.StatusUnauthorized, ResponseEnvelope{Error: &ErrorEnvelope{
			Code:          ErrorCodeUnauthenticated,
			Message:       "authentication is required",
			CorrelationID: documentationCorrelationID(r),
		}})
		return
	}
	WriteJSON(w, http.StatusUnauthorized, map[string]string{
		"error_code":     string(ErrorCodeUnauthenticated),
		"message":        "authentication is required",
		"correlation_id": documentationCorrelationID(r),
	})
}

// scopedRouteDeniedResponse writes the route-admission 403 for both refusal
// paths -- the scoped-bearer branch and the browser-session branch of
// authMiddlewareWithRoutePolicy. It marks the route-denial signal first, so an
// observer that installed one (go/internal/mcp's transport wrapper, via
// WithScopedRouteDenialSignal) can tell this authorization refusal apart from
// an authentication failure carrying the same status code. Marking here rather
// than at each call site is deliberate: the mark and the response cannot then
// disagree about what the caller received.
func scopedRouteDeniedResponse(w http.ResponseWriter, r *http.Request) {
	markScopedRouteDenied(r.Context())
	const message = "scoped authorization is not yet enabled for this route"
	if acceptsEnvelope(r) {
		WriteJSON(w, http.StatusForbidden, ResponseEnvelope{Error: &ErrorEnvelope{
			Code:          ErrorCodePermissionDenied,
			Message:       message,
			CorrelationID: documentationCorrelationID(r),
		}})
		return
	}
	WriteJSON(w, http.StatusForbidden, map[string]string{
		"error_code":     string(ErrorCodePermissionDenied),
		"message":        message,
		"correlation_id": documentationCorrelationID(r),
	})
}

func csrfDeniedResponse(w http.ResponseWriter, r *http.Request) {
	const message = "csrf token is required for browser session requests"
	if acceptsEnvelope(r) {
		WriteJSON(w, http.StatusForbidden, ResponseEnvelope{Error: &ErrorEnvelope{
			Code:          ErrorCodePermissionDenied,
			Message:       message,
			CorrelationID: documentationCorrelationID(r),
		}})
		return
	}
	WriteJSON(w, http.StatusForbidden, map[string]string{
		"error_code":     string(ErrorCodePermissionDenied),
		"message":        message,
		"correlation_id": documentationCorrelationID(r),
	})
}
