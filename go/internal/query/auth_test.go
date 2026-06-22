package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
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

	publicRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/health"},
		{http.MethodGet, "/healthz"},
		{http.MethodGet, "/readyz"},
		{http.MethodGet, "/metrics"},
		{http.MethodGet, "/admin/status"},
		{http.MethodGet, "/api/v0/health"},
		{http.MethodGet, "/api/v0/docs"},
		{http.MethodGet, "/api/v0/openapi.json"},
		{http.MethodGet, "/api/v0/redoc"},
		{http.MethodGet, "/api/v0/auth/saml/providers/provider_a/metadata"},
		{http.MethodGet, "/api/v0/auth/saml/providers/provider_a/login"},
		{http.MethodPost, "/api/v0/auth/saml/providers/provider_a/acs"},
	}

	for _, route := range publicRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			// No Authorization header
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected status 200 for public route %s %s, got %d", route.method, route.path, rec.Code)
			}
		})
	}
}

func TestAuthMiddleware_SAMLACSPublicOnlyForPOST(t *testing.T) {
	t.Parallel()

	handler := AuthMiddleware("valid-secret-token", mockHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/saml/providers/provider_a/acs", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET SAML ACS status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_GovernanceStatusRequiresAuth(t *testing.T) {
	t.Parallel()

	handler := AuthMiddleware("valid-secret-token", mockHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/governance", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeUnauthenticated {
		t.Fatalf("envelope.Error = %#v, want unauthenticated", envelope.Error)
	}
}

func TestAuthMiddlewareWithGovernanceAuditRecordsDeniedReadAuthorization(t *testing.T) {
	t.Parallel()

	audit := &fakeGovernanceAuditAppender{}
	handler := AuthMiddlewareWithGovernanceAudit("valid-secret-token", mockHandler(), audit)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/governance", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.Header.Set("X-Correlation-ID", "corr-auth-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if strings.Contains(rec.Body.String(), "valid-secret-token") {
		t.Fatalf("unauthorized body leaked token: %s", rec.Body.String())
	}
	if got, want := len(audit.events), 1; got != want {
		t.Fatalf("len(audit.events) = %d, want %d", got, want)
	}
	event := audit.events[0]
	if got, want := event.Type, governanceaudit.EventTypeReadAuthorization; got != want {
		t.Fatalf("event.Type = %q, want %q", got, want)
	}
	if got, want := event.ActorClass, governanceaudit.ActorClassAnonymous; got != want {
		t.Fatalf("event.ActorClass = %q, want %q", got, want)
	}
	if got, want := event.ScopeClass, governanceaudit.ScopeClassAdmin; got != want {
		t.Fatalf("event.ScopeClass = %q, want %q", got, want)
	}
	if got, want := event.Decision, governanceaudit.DecisionDenied; got != want {
		t.Fatalf("event.Decision = %q, want %q", got, want)
	}
	if got, want := event.ReasonCode, "authentication_required"; got != want {
		t.Fatalf("event.ReasonCode = %q, want %q", got, want)
	}
	if got, want := event.CorrelationID, "corr-auth-123"; got != want {
		t.Fatalf("event.CorrelationID = %q, want %q", got, want)
	}
	if event.OccurredAt.IsZero() {
		t.Fatal("event.OccurredAt is zero, want wall clock timestamp")
	}
}

func TestAuthMiddlewareWithGovernanceAuditDropsUnsafeCorrelationID(t *testing.T) {
	t.Parallel()

	audit := &fakeGovernanceAuditAppender{}
	handler := AuthMiddlewareWithGovernanceAudit("valid-secret-token", mockHandler(), audit)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/governance", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.Header.Set("X-Correlation-ID", "operator.person@example.invalid")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := len(audit.events), 1; got != want {
		t.Fatalf("len(audit.events) = %d, want %d", got, want)
	}
	if got := audit.events[0].CorrelationID; got != "" {
		t.Fatalf("event.CorrelationID = %q, want empty safe value", got)
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

type fakeGovernanceAuditAppender struct {
	events []governanceaudit.Event
}

func (f *fakeGovernanceAuditAppender) Append(_ context.Context, events []governanceaudit.Event) error {
	f.events = append(f.events, events...)
	return nil
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

func TestAuthMiddlewareWithScopedTokensAttachesAuthContext(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant_a",
			WorkspaceID:          "workspace_a",
			SubjectClass:         "team",
			SubjectIDHash:        "sha256:subject",
			PolicyRevisionHash:   "sha256:policy",
			AllowedScopeIDs:      []string{"scope_a"},
			AllowedRepositoryIDs: []string{"repo_a"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, ok := AuthContextFromContext(r.Context())
		if !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		if auth.Mode != AuthModeScoped || auth.TenantID != "tenant_a" ||
			auth.WorkspaceID != "workspace_a" || auth.AllScopes {
			t.Fatalf("auth context = %#v, want scoped tenant/workspace", auth)
		}
		if len(auth.AllowedScopeIDs) != 1 || auth.AllowedScopeIDs[0] != "scope_a" {
			t.Fatalf("AllowedScopeIDs = %#v, want scope_a", auth.AllowedScopeIDs)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if resolver.token != "scoped-token" {
		t.Fatalf("resolver token = %q, want scoped-token", resolver.token)
	}
}

func TestAuthMiddlewareWithScopedTokensFallsBackToSharedTokenContext(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{}
	handler := AuthMiddlewareWithScopedTokens("shared-token", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, ok := AuthContextFromContext(r.Context())
		if !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		if auth.Mode != AuthModeShared || !auth.AllScopes {
			t.Fatalf("auth context = %#v, want shared all-scope context", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer shared-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
}

func TestAuthMiddlewareWithScopedTokensRejectsUnknownScopedToken(t *testing.T) {
	t.Parallel()

	handler := AuthMiddlewareWithScopedTokens("", &fakeScopedTokenResolver{}, mockHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer missing-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthMiddlewareWithScopedTokensResolverError(t *testing.T) {
	t.Parallel()

	handler := AuthMiddlewareWithScopedTokens("", &fakeScopedTokenResolver{
		err: errors.New("lookup failed"),
	}, mockHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthMiddlewareWithScopedTokensPublicPathSkipsResolver(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{err: errors.New("must not be called")}
	handler := AuthMiddlewareWithScopedTokens("", resolver, mockHandler())
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if resolver.called {
		t.Fatal("resolver called for public path")
	}
}

type fakeScopedTokenResolver struct {
	context AuthContext
	ok      bool
	err     error

	// mu guards the capture fields because a single resolver instance is
	// shared across parallel subtests (e.g. the package-registry adjacent-route
	// table), which call ResolveScopedToken concurrently. Without it the shared
	// fake data-races under -race and aborts unrelated tests in the package.
	mu     sync.Mutex
	token  string
	called bool
}

func (f *fakeScopedTokenResolver) ResolveScopedToken(
	_ context.Context,
	token string,
) (AuthContext, bool, error) {
	f.mu.Lock()
	f.called = true
	f.token = token
	f.mu.Unlock()
	return f.context, f.ok, f.err
}
