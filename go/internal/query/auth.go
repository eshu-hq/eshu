package query

import (
	"crypto/subtle"
	"net/http"
	"strings"
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

// AuthMiddleware wraps an HTTP handler with bearer token authentication.
//
// If token is empty, authentication is disabled (dev mode).
// If the request path is in publicHTTPPaths, authentication is skipped.
// Otherwise, the Authorization header must contain "Bearer <token>" with
// a token that matches the configured value using constant-time comparison.
//
// Returns 401 Unauthorized with a JSON error body if authentication fails.
func AuthMiddleware(token string, next http.Handler) http.Handler {
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
			unauthorizedResponse(w, r)
			return
		}

		// Trim whitespace from credentials
		credentials = strings.TrimSpace(credentials)
		if credentials == "" {
			unauthorizedResponse(w, r)
			return
		}

		// Compare tokens using constant-time comparison
		if !constantTimeEqual(credentials, token) {
			unauthorizedResponse(w, r)
			return
		}

		// Auth succeeded
		next.ServeHTTP(w, r)
	})
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
