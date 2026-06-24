package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// askSearchEntitledSemanticIndex returns an index store wired to succeed so an
// entitled caller reaches a 200 with one bounded result.
func askSearchEntitledSemanticIndex() *fakeSemanticSearchIndexStore {
	return &fakeSemanticSearchIndexStore{
		result: semanticSearchIndexResult{
			IndexedDocumentCount: 1,
			Candidates: []searchretrieval.Candidate{
				{
					Document: semanticSearchDocumentFixture(
						"searchdoc:payments",
						"repo-payments",
						"Payments runbook",
						"payment runbook ownership escalation",
					),
					Score:    2.5,
					Metadata: map[string]string{"search_method": "bm25"},
				},
			},
		},
	}
}

// askSearchRequest builds a keyword semantic-search request for the entitled
// repository.
func askSearchRequest(t *testing.T) *http.Request {
	t.Helper()
	return semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      1,
		"timeout_ms": 250,
	})
}

// entitledAskSearchAuth returns the grant snapshot a caller with the ask_search
// family fully granted would carry, parameterized only by auth mode so the same
// grants can be presented as a scoped token or a browser session.
func entitledAskSearchAuth(mode AuthMode) AuthContext {
	return AuthContext{
		Mode:                         mode,
		TenantID:                     "tenant-a",
		WorkspaceID:                  "workspace-a",
		PermissionCatalogEnforced:    true,
		AllowedRepositoryIDs:         []string{"repo-payments"},
		AllowedPermissionFeatures:    []string{"ask_search"},
		AllowedPermissionDataClasses: []string{"ask_reasoning", "source_content", "documentation_semantic"},
	}
}

// unentitledAskSearchAuth returns a grant snapshot that lacks the ask_search
// family entirely (only repository_content is granted).
func unentitledAskSearchAuth(mode AuthMode) AuthContext {
	return AuthContext{
		Mode:                         mode,
		TenantID:                     "tenant-a",
		WorkspaceID:                  "workspace-a",
		PermissionCatalogEnforced:    true,
		AllowedRepositoryIDs:         []string{"repo-payments"},
		AllowedPermissionFeatures:    []string{"repository_content"},
		AllowedPermissionDataClasses: []string{"source"},
	}
}

func runSemanticSearch(t *testing.T, auth AuthContext) (*httptest.ResponseRecorder, *fakeSemanticSearchIndexStore) {
	t.Helper()
	index := askSearchEntitledSemanticIndex()
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction}
	req := askSearchRequest(t)
	req = req.WithContext(ContextWithAuthContext(req.Context(), auth))
	rec := httptest.NewRecorder()
	handler.search(rec, req)
	return rec, index
}

// TestBrowserSessionEnforcesPermissionCatalogDeniesUnentitledFeature proves the
// enforcement now activates for cookie sessions (not just scoped tokens): a
// browser session whose derived features lack the route's family is denied.
func TestBrowserSessionEnforcesPermissionCatalogDeniesUnentitledFeature(t *testing.T) {
	t.Parallel()

	rec, index := runSemanticSearch(t, unentitledAskSearchAuth(AuthModeBrowserSession))

	if got, want := rec.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if index.calls != 0 {
		t.Fatalf("index calls = %d, want 0 before feature authorization", index.calls)
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != ErrorCodePermissionDenied {
		t.Fatalf("error = %#v, want permission_denied", envelope.Error)
	}
}

// TestBrowserSessionEnforcesPermissionCatalogAllowsEntitledFeature proves an
// entitled cookie session reaches the handler and succeeds.
func TestBrowserSessionEnforcesPermissionCatalogAllowsEntitledFeature(t *testing.T) {
	t.Parallel()

	rec, index := runSemanticSearch(t, entitledAskSearchAuth(AuthModeBrowserSession))

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if index.calls != 1 {
		t.Fatalf("index calls = %d, want 1 for entitled session", index.calls)
	}
}

// TestSessionAndScopedTokenAuthorizeIdentically proves parity: the same derived
// grant snapshot yields the same authorization decision whether presented as a
// scoped token or as a browser session, for both the denied and allowed cases.
func TestSessionAndScopedTokenAuthorizeIdentically(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		auth     func(AuthMode) AuthContext
		wantCode int
	}{
		{"unentitled denied", unentitledAskSearchAuth, http.StatusForbidden},
		{"entitled allowed", entitledAskSearchAuth, http.StatusOK},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scopedRec, _ := runSemanticSearch(t, tc.auth(AuthModeScoped))
			sessionRec, _ := runSemanticSearch(t, tc.auth(AuthModeBrowserSession))

			if scopedRec.Code != tc.wantCode {
				t.Fatalf("scoped status = %d, want %d; body = %s", scopedRec.Code, tc.wantCode, scopedRec.Body.String())
			}
			if sessionRec.Code != scopedRec.Code {
				t.Fatalf("session status = %d, scoped status = %d; want identical (session and token must authorize the same)", sessionRec.Code, scopedRec.Code)
			}
		})
	}
}

// TestAllScopesSessionBypassesPermissionCatalog proves an all-scopes (admin)
// session is unaffected: enforcement is bypassed exactly as before, even with
// empty derived features, because PermissionCatalogEnforced stays false.
func TestAllScopesSessionBypassesPermissionCatalog(t *testing.T) {
	t.Parallel()

	rec, index := runSemanticSearch(t, AuthContext{
		Mode:                      AuthModeBrowserSession,
		TenantID:                  "tenant-a",
		WorkspaceID:               "workspace-a",
		AllScopes:                 true,
		PermissionCatalogEnforced: false,
		AllowedRepositoryIDs:      []string{"repo-payments"},
	})

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if index.calls != 1 {
		t.Fatalf("index calls = %d, want 1 for all-scopes session (enforcement must stay bypassed)", index.calls)
	}
}
