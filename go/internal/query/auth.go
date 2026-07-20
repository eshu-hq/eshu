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

type authContextKey struct{}

// AuthMode names the source of an authenticated request context.
type AuthMode string

const (
	// AuthModeShared identifies the legacy shared bearer-token path.
	AuthModeShared AuthMode = "shared"
	// AuthModeScoped identifies a token resolved through the scoped registry.
	AuthModeScoped AuthMode = "scoped"
	// AuthModeBrowserSession identifies a server-managed dashboard session.
	AuthModeBrowserSession AuthMode = "browser_session"
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
type AuthContext struct {
	Mode                         AuthMode
	TenantID                     string
	WorkspaceID                  string
	SubjectClass                 string
	SubjectIDHash                string
	PolicyRevisionHash           string
	RoleIDs                      []string
	PermissionCatalogEnforced    bool
	AllowedPermissionFeatures    []string
	AllowedPermissionDataClasses []string
	AllScopes                    bool
	AllowedScopeIDs              []string
	AllowedRepositoryIDs         []string
	// ExternalProviderConfigID is the stored OIDC/SAML config ID for sessions
	// that were established via an external identity provider. Empty for local
	// password sessions.
	ExternalProviderConfigID string
}

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
	if ctx == nil {
		return AuthContext{}, false
	}
	auth, ok := ctx.Value(authContextKey{}).(AuthContext)
	return auth, ok
}

// ContextWithAuthContext returns a child context carrying authorization bounds.
func ContextWithAuthContext(ctx context.Context, auth AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey{}, auth)
}

func authMiddleware(
	token string,
	resolver ScopedTokenResolver,
	sessionResolver BrowserSessionResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
	authEnforcementConfigured bool,
) http.Handler {
	return authMiddlewareWithRoutePolicy(
		token,
		resolver,
		sessionResolver,
		next,
		audit,
		BrowserSessionRoutePolicy{},
		authEnforcementConfigured,
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
			recordReadAuthorizationDenied(r, audit)
			unauthorizedResponse(w, r)
			return
		}

		scheme, credentials, found := strings.Cut(authorization, " ")
		if !found || strings.ToLower(strings.TrimSpace(scheme)) != "bearer" {
			recordReadAuthorizationDenied(r, audit)
			unauthorizedResponse(w, r)
			return
		}

		credentials = strings.TrimSpace(credentials)
		if credentials == "" {
			recordReadAuthorizationDenied(r, audit)
			unauthorizedResponse(w, r)
			return
		}

		if resolver != nil {
			auth, ok, err := resolver.ResolveScopedToken(r.Context(), credentials)
			if err != nil {
				recordReadAuthorizationDenied(r, audit)
				unauthorizedResponse(w, r)
				return
			}
			if ok {
				auth = normalizeAuthContext(auth)
				if auth.Mode == AuthModeScoped && !scopedHTTPRouteSupportsTenantFilter(r) {
					recordScopedRouteAuthorizationDenied(r, audit, auth)
					scopedRouteDeniedResponse(w, r)
					return
				}
				next.ServeHTTP(w, r.WithContext(ContextWithAuthContext(r.Context(), auth)))
				return
			}
		}

		if token == "" || !constantTimeEqual(credentials, token) {
			recordReadAuthorizationDenied(r, audit)
			unauthorizedResponse(w, r)
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
	if auth.Mode == AuthModeBrowserSession && !browserSessionRouteAllowed(r, auth, policy) {
		recordScopedRouteAuthorizationDenied(r, audit, auth)
		scopedRouteDeniedResponse(w, r)
		return true
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

func normalizeAuthContext(auth AuthContext) AuthContext {
	if auth.Mode == "" {
		auth.Mode = AuthModeScoped
	}
	auth.TenantID = strings.TrimSpace(auth.TenantID)
	auth.WorkspaceID = strings.TrimSpace(auth.WorkspaceID)
	auth.SubjectClass = strings.TrimSpace(auth.SubjectClass)
	auth.SubjectIDHash = strings.TrimSpace(auth.SubjectIDHash)
	auth.PolicyRevisionHash = strings.TrimSpace(auth.PolicyRevisionHash)
	auth.RoleIDs = cleanedAuthStrings(auth.RoleIDs)
	auth.AllowedPermissionFeatures = cleanedAuthStrings(auth.AllowedPermissionFeatures)
	auth.AllowedPermissionDataClasses = cleanedAuthStrings(auth.AllowedPermissionDataClasses)
	auth.AllowedScopeIDs = cleanedAuthStrings(auth.AllowedScopeIDs)
	auth.AllowedRepositoryIDs = cleanedAuthStrings(auth.AllowedRepositoryIDs)
	auth.ExternalProviderConfigID = strings.TrimSpace(auth.ExternalProviderConfigID)
	return auth
}

func cleanedAuthStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
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
	return cleaned
}

// constantTimeEqual compares two strings in constant time to prevent timing attacks.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// unauthorizedResponse writes a 401 JSON error response.
func unauthorizedResponse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", "Bearer")
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

func scopedRouteDeniedResponse(w http.ResponseWriter, r *http.Request) {
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
