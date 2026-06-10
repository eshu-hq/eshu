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

// GovernanceAuditAppender records validation-safe governance audit events.
type GovernanceAuditAppender interface {
	Append(context.Context, []governanceaudit.Event) error
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
	return AuthMiddlewareWithGovernanceAudit(token, next, nil)
}

// AuthMiddlewareWithGovernanceAudit wraps an HTTP handler with bearer token
// authentication and records denied read-authorization events when a private
// audit sink is available.
func AuthMiddlewareWithGovernanceAudit(
	token string,
	next http.Handler,
	audit GovernanceAuditAppender,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Dev mode: skip auth when token is empty
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Public paths: skip auth
		if publicHTTPPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		// Extract Authorization header
		authorization := r.Header.Get("Authorization")
		scheme, credentials, found := strings.Cut(authorization, " ")

		// Validate scheme and credentials
		if !found || strings.ToLower(strings.TrimSpace(scheme)) != "bearer" {
			recordReadAuthorizationDenied(r, audit)
			unauthorizedResponse(w, r)
			return
		}

		// Trim whitespace from credentials
		credentials = strings.TrimSpace(credentials)
		if credentials == "" {
			recordReadAuthorizationDenied(r, audit)
			unauthorizedResponse(w, r)
			return
		}

		// Compare tokens using constant-time comparison
		if !constantTimeEqual(credentials, token) {
			recordReadAuthorizationDenied(r, audit)
			unauthorizedResponse(w, r)
			return
		}

		// Auth succeeded
		next.ServeHTTP(w, r)
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
