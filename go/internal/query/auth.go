package query

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// publicHTTPPaths lists routes that bypass authentication.
var publicHTTPPaths = map[string]bool{
	"/health":              true,
	"/healthz":             true,
	"/readyz":              true,
	"/metrics":             true,
	"/admin/status":        true,
	"/api/v0/health":       true,
	"/api/v0/docs":         true,
	"/api/v0/openapi.json": true,
	"/api/v0/redoc":        true,
}

const governanceAuditAppendTimeout = 500 * time.Millisecond

type authContextKey struct{}

// AuthMode names the source of an authenticated request context.
type AuthMode string

const (
	// AuthModeShared identifies the legacy shared bearer-token path.
	AuthModeShared AuthMode = "shared"
	// AuthModeScoped identifies a token resolved through the scoped registry.
	AuthModeScoped AuthMode = "scoped"
)

// AuthContext carries request-scoped authorization bounds for query handlers.
type AuthContext struct {
	Mode                 AuthMode
	TenantID             string
	WorkspaceID          string
	SubjectClass         string
	SubjectIDHash        string
	PolicyRevisionHash   string
	AllScopes            bool
	AllowedScopeIDs      []string
	AllowedRepositoryIDs []string
}

// ScopedTokenResolver resolves a presented bearer credential into an auth
// context without exposing raw token values to handlers.
type ScopedTokenResolver interface {
	ResolveScopedToken(context.Context, string) (AuthContext, bool, error)
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

// AuthMiddleware wraps an HTTP handler with bearer token authentication.
//
// If token is empty, authentication is disabled (dev mode).
// If the request path is in publicHTTPPaths, authentication is skipped.
// Otherwise, the Authorization header must contain "Bearer <token>" with
// a token that matches the configured value using constant-time comparison.
//
// Returns 401 Unauthorized with a JSON error body if authentication fails.
func AuthMiddleware(token string, next http.Handler) http.Handler {
	return authMiddleware(token, nil, next, nil)
}

// AuthMiddlewareWithGovernanceAudit wraps an HTTP handler with bearer token
// authentication and records denied read-authorization events when a private
// audit sink is available.
func AuthMiddlewareWithGovernanceAudit(
	token string,
	next http.Handler,
	audit GovernanceAuditAppender,
) http.Handler {
	return authMiddleware(token, nil, next, audit)
}

// AuthMiddlewareWithScopedTokens wraps an HTTP handler with shared-token
// compatibility plus optional scoped-token resolution.
func AuthMiddlewareWithScopedTokens(
	token string,
	resolver ScopedTokenResolver,
	next http.Handler,
) http.Handler {
	return authMiddleware(token, resolver, next, nil)
}

// AuthMiddlewareWithScopedTokensAndGovernanceAudit wraps an HTTP handler with
// shared-token compatibility, optional scoped-token resolution, and denied
// read-authorization audit events. A nil resolver disables scoped-token
// resolution, leaving shared-token (or dev-mode when token is empty) behavior
// unchanged.
func AuthMiddlewareWithScopedTokensAndGovernanceAudit(
	token string,
	resolver ScopedTokenResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
) http.Handler {
	return authMiddleware(token, resolver, next, audit)
}

func authMiddleware(
	token string,
	resolver ScopedTokenResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Dev mode: skip auth when token is empty.
		if token == "" && resolver == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Public paths: skip auth.
		if publicHTTPPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		authorization := r.Header.Get("Authorization")
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

func recordReadAuthorizationDenied(r *http.Request, audit GovernanceAuditAppender) {
	if audit == nil {
		return
	}
	event := governanceaudit.Event{
		Type:          governanceaudit.EventTypeReadAuthorization,
		ActorClass:    governanceaudit.ActorClassAnonymous,
		ScopeClass:    governanceaudit.ScopeClassAdmin,
		Decision:      governanceaudit.DecisionDenied,
		ReasonCode:    "authentication_required",
		CorrelationID: safeAuditCorrelationID(documentationCorrelationID(r)),
		OccurredAt:    time.Now().UTC(),
	}
	ctx, cancel := context.WithTimeout(r.Context(), governanceAuditAppendTimeout)
	defer cancel()
	_ = audit.Append(ctx, []governanceaudit.Event{event})
}

func recordScopedRouteAuthorizationDenied(
	r *http.Request,
	audit GovernanceAuditAppender,
	auth AuthContext,
) {
	if audit == nil {
		return
	}
	actorClass := governanceaudit.ActorClassScopedToken
	if auth.SubjectIDHash == "" {
		actorClass = governanceaudit.ActorClassAnonymous
	}
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeReadAuthorization,
		ActorClass:         actorClass,
		ActorIDHash:        auth.SubjectIDHash,
		ScopeClass:         governanceaudit.ScopeClassAdmin,
		Decision:           governanceaudit.DecisionDenied,
		ReasonCode:         "scoped_route_not_enabled",
		CorrelationID:      safeAuditCorrelationID(documentationCorrelationID(r)),
		PolicyRevisionHash: auth.PolicyRevisionHash,
		OccurredAt:         time.Now().UTC(),
	}
	ctx, cancel := context.WithTimeout(r.Context(), governanceAuditAppendTimeout)
	defer cancel()
	_ = audit.Append(ctx, []governanceaudit.Event{event})
}

func safeAuditCorrelationID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 96 {
		return ""
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') &&
			r != '_' && r != '-' && r != ':' {
			return ""
		}
	}
	return value
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
	auth.AllowedScopeIDs = cleanedAuthStrings(auth.AllowedScopeIDs)
	auth.AllowedRepositoryIDs = cleanedAuthStrings(auth.AllowedRepositoryIDs)
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
