package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordingAsker captures the *http.Request passed to Ask so tests can inspect
// the Authorization header that the handler forwarded.
type recordingAsker struct {
	capturedHeader string
	answer         AskAnswer
	err            error
}

func (a *recordingAsker) Ask(r *http.Request, _ string) (AskAnswer, error) {
	a.capturedHeader = r.Header.Get("Authorization")
	return a.answer, a.err
}

// TestScopedTokenAskRouteNotDenied asserts that a scoped token is permitted to
// reach POST /api/v0/ask (status is not 403 permission_denied). The AskHandler
// is disabled (nil Asker) so the response is 503, proving the request passed
// the auth middleware.
func TestScopedTokenAskRouteNotDenied(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-1"},
		},
		ok: true,
	}
	// Mount AskHandler (nil Asker → 503 unavailable) on a mux so the middleware
	// has an http.Handler to wrap.
	mux := http.NewServeMux()
	h := &AskHandler{Asker: nil}
	h.Mount(mux)
	handler := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask",
		bytes.NewBufferString(`{"question":"what repos do I have?"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer scoped-token-xyz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("scoped token got 403 on POST /api/v0/ask; route must be in the scoped allowlist; body = %s", rec.Body.String())
	}
	// The disabled handler returns 503 — confirm we reached the handler, not the
	// auth middleware denial path.
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (ask disabled), got %d; body = %s", rec.Code, rec.Body.String())
	}
	var resp askUnavailableResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode 503 body: %v", err)
	}
	if resp.State != "unavailable" {
		t.Errorf("state = %q, want unavailable", resp.State)
	}
}

// TestScopedTokenAskRoutePreservesAuthHeader asserts that the AskHandler passes
// the caller's Authorization header through to the Asker unchanged. This is the
// scope-preservation proof: the inner runner receives the same token the caller
// presented, so inner tool calls are dispatched under the caller's scope and
// cannot exceed the caller's grant.
func TestScopedTokenAskRoutePreservesAuthHeader(t *testing.T) {
	t.Parallel()

	const callerToken = "Bearer scoped-caller-token-abc123"

	recorder := &recordingAsker{answer: AskAnswer{}}
	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-b",
			WorkspaceID:          "workspace-b",
			AllowedRepositoryIDs: []string{"repo-2"},
		},
		ok: true,
	}
	mux := http.NewServeMux()
	h := &AskHandler{Asker: recorder}
	h.Mount(mux)
	handler := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask",
		bytes.NewBufferString(`{"question":"list my services"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", callerToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body = %s", rec.Code, rec.Body.String())
	}
	if recorder.capturedHeader != callerToken {
		t.Errorf("Asker received Authorization = %q, want %q — scope was NOT preserved end-to-end",
			recorder.capturedHeader, callerToken)
	}
}

// TestScopedTokenAskRouteScopePreservation asserts that a scoped token with a
// restricted repository grant does NOT have that grant widened: the Asker
// receives the exact Authorization header presented by the caller, not a shared
// admin token. A fake "admin" token is baked in as the shared token; the test
// confirms the Asker never sees that value when a scoped token is used.
func TestScopedTokenAskRouteScopePreservation(t *testing.T) {
	t.Parallel()

	const sharedAdminToken = "Bearer shared-admin-secret"
	const callerScopedToken = "Bearer scoped-caller-restricted"

	recorder := &recordingAsker{answer: AskAnswer{}}
	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-c",
			WorkspaceID:          "workspace-c",
			AllowedRepositoryIDs: []string{"repo-3"},
		},
		ok: true,
	}
	mux := http.NewServeMux()
	h := &AskHandler{Asker: recorder}
	h.Mount(mux)
	// Wire both the shared token and the scoped resolver, as production does.
	handler := AuthMiddlewareWithScopedTokens(
		strings.TrimPrefix(sharedAdminToken, "Bearer "),
		resolver,
		mux,
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask",
		bytes.NewBufferString(`{"question":"what services do I have?"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", callerScopedToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body = %s", rec.Code, rec.Body.String())
	}
	// The Asker must NOT receive the shared admin token — that would widen scope.
	if recorder.capturedHeader == sharedAdminToken {
		t.Errorf("Asker received the shared admin token instead of the caller's scoped token — scope was WIDENED")
	}
	// The Asker MUST receive the caller's own scoped token.
	if recorder.capturedHeader != callerScopedToken {
		t.Errorf("Asker received Authorization = %q, want %q", recorder.capturedHeader, callerScopedToken)
	}
}

// TestSharedTokenAskRouteStillWorks asserts that the existing shared-token
// path continues to work after the scoped-token allowlist addition.
func TestSharedTokenAskRouteStillWorks(t *testing.T) {
	t.Parallel()

	recorder := &recordingAsker{answer: AskAnswer{Narrated: true, Prose: "You have 2 services."}}
	mux := http.NewServeMux()
	h := &AskHandler{Asker: recorder}
	h.Mount(mux)
	// Shared-token only (no resolver).
	handler := AuthMiddlewareWithScopedTokens("shared-api-key", nil, mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask",
		bytes.NewBufferString(`{"question":"what services?"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer shared-api-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body = %s", rec.Code, rec.Body.String())
	}
}

// TestScopedAskRouteDeniedWhenNotInAllowlist verifies the gate function
// directly: a scoped token on an unregistered route still returns false.
func TestScopedAskRouteInAllowlist(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask", nil)
	if !scopedHTTPRouteSupportsTenantFilter(req) {
		t.Error("scopedHTTPRouteSupportsTenantFilter(POST /api/v0/ask) = false, want true")
	}
}
