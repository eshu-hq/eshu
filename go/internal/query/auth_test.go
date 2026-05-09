package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockHandler returns 200 with "ok" body when called
func mockHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	token := "valid-secret-token"
	handler := AuthMiddleware(token, mockHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer valid-secret-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got %q", rec.Body.String())
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	token := "valid-secret-token"
	handler := AuthMiddleware(token, mockHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_MissingAuthHeader(t *testing.T) {
	token := "valid-secret-token"
	handler := AuthMiddleware(token, mockHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_MalformedAuthHeader(t *testing.T) {
	token := "valid-secret-token"
	handler := AuthMiddleware(token, mockHandler())

	tests := []struct {
		name   string
		header string
	}{
		{"empty value", "Bearer "},
		{"no bearer prefix", "valid-secret-token"},
		{"wrong scheme", "Basic dXNlcjpwYXNz"},
		{"only whitespace", "Bearer   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
			req.Header.Set("Authorization", tt.header)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected status 401, got %d", rec.Code)
			}
		})
	}
}

func TestAuthMiddleware_PublicPaths(t *testing.T) {
	token := "valid-secret-token"
	handler := AuthMiddleware(token, mockHandler())

	publicPaths := []string{
		"/health",
		"/healthz",
		"/readyz",
		"/metrics",
		"/admin/status",
		"/api/v0/health",
		"/api/v0/docs",
		"/api/v0/openapi.json",
		"/api/v0/redoc",
	}

	for _, path := range publicPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			// No Authorization header
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected status 200 for public path %s, got %d", path, rec.Code)
			}
		})
	}
}

func TestAuthMiddleware_DevMode_EmptyToken(t *testing.T) {
	// Empty token means dev mode: skip auth
	handler := AuthMiddleware("", mockHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	// No Authorization header
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 in dev mode, got %d", rec.Code)
	}
}

func TestAuthMiddleware_UnauthorizedResponse(t *testing.T) {
	token := "valid-secret-token"
	handler := AuthMiddleware(token, mockHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Check status
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}

	// Check WWW-Authenticate header
	wwwAuth := rec.Header().Get("WWW-Authenticate")
	if wwwAuth != "Bearer" {
		t.Errorf("expected WWW-Authenticate: Bearer, got %q", wwwAuth)
	}

	// Check JSON content type
	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Errorf("expected JSON content type, got %q", contentType)
	}

	// Check body contains "detail"
	body := rec.Body.String()
	if body == "" {
		t.Error("expected non-empty JSON body")
	}
}

func TestAuthMiddleware_UnauthorizedEnvelopeResponse(t *testing.T) {
	token := "valid-secret-token"
	handler := AuthMiddleware(token, mockHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.Header.Set("X-Correlation-ID", "corr-auth-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d; body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("expected WWW-Authenticate: Bearer, got %q", got)
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Error == nil {
		t.Fatalf("envelope.Error = nil, want unauthenticated error; body = %s", rec.Body.String())
	}
	if got, want := envelope.Error.Code, ErrorCodeUnauthenticated; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if got, want := envelope.Error.CorrelationID, "corr-auth-123"; got != want {
		t.Fatalf("correlation_id = %q, want %q", got, want)
	}
}

func TestAuthMiddleware_CaseSensitiveScheme(t *testing.T) {
	token := "valid-secret-token"
	handler := AuthMiddleware(token, mockHandler())

	schemes := []string{
		"bearer valid-secret-token",
		"BEARER valid-secret-token",
		"Bearer valid-secret-token",
	}

	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
			req.Header.Set("Authorization", scheme)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected status 200 for scheme %q, got %d", scheme, rec.Code)
			}
		})
	}
}
