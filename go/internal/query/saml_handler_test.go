// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/samlauth"
)

func TestSAMLHandlerMetadataServesServiceProviderXML(t *testing.T) {
	t.Parallel()

	provider := testSAMLProvider()
	handler := &SAMLHandler{Store: &fakeSAMLStore{provider: provider}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/saml/providers/provider_a/metadata", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/samlmetadata+xml") {
		t.Fatalf("content type = %q, want SAML metadata XML", got)
	}
	if !strings.Contains(rec.Body.String(), provider.ServiceProvider.EntityID) ||
		!strings.Contains(rec.Body.String(), provider.ServiceProvider.ACSURL) {
		t.Fatalf("metadata body missing SP endpoints: %s", rec.Body.String())
	}
}

func TestSAMLHandlerLoginRedirectsAndStoresRelayStateHash(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 15, 45, 0, 0, time.UTC)
	store := &fakeSAMLStore{provider: testSAMLProvider()}
	handler := &SAMLHandler{
		Store:          store,
		RequestBuilder: fakeSAMLRequestBuilder{},
		NewSecret:      sequenceSecrets("relay-secret"),
		Now:            func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/saml/providers/provider_a/login", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "https://idp.example.test/sso?SAMLRequest=request&RelayState=relay-secret" {
		t.Fatalf("redirect location = %q, want IdP redirect", got)
	}
	if got, want := store.createdRequest.RelayStateHash, BrowserSessionSecretHash("relay-secret"); got != want {
		t.Fatalf("relay hash = %q, want %q", got, want)
	}
	if store.createdRequest.RelayStateHash == "relay-secret" {
		t.Fatalf("stored raw relay state: %#v", store.createdRequest)
	}
	if got, want := store.createdRequest.RequestIDHash, BrowserSessionSecretHash("request-1"); got != want {
		t.Fatalf("request id hash = %q, want %q", got, want)
	}
	if store.createdRequest.RequestIDHash == "request-1" {
		t.Fatalf("stored raw request id: %#v", store.createdRequest)
	}
	if got, want := store.createdRequest.ExpiresAt, now.Add(DefaultSAMLRequestTTL); !got.Equal(want) {
		t.Fatalf("request expiry = %v, want %v", got, want)
	}
}

func TestSAMLHandlerACSRequiresRelayStateAndSAMLResponse(t *testing.T) {
	t.Parallel()

	store := &fakeSAMLStore{provider: testSAMLProvider()}
	handler := &SAMLHandler{
		Store:    store,
		Sessions: store,
		Verifier: fakeSAMLVerifier{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/saml/providers/provider_a/acs", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSAMLHandlerACSCreatesHashOnlyBrowserSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC)
	requestIDHash := BrowserSessionSecretHash("request-1")
	seenRequestIDHashes := []string{}
	store := &fakeSAMLStore{
		provider:  testSAMLProvider(),
		requestOK: true,
		resolveOK: true,
		replayOK:  true,
		sessionAuth: AuthContext{
			Mode:                 AuthModeBrowserSession,
			TenantID:             "tenant_a",
			WorkspaceID:          "workspace_a",
			SubjectClass:         "external_saml",
			SubjectIDHash:        "sha256:subject",
			PolicyRevisionHash:   "sha256:policy",
			AllowedScopeIDs:      []string{"scope_a"},
			AllowedRepositoryIDs: []string{"repo_a"},
		},
	}
	handler := &SAMLHandler{
		Store:     store,
		Sessions:  store,
		Verifier:  fakeSAMLVerifier{requestIDHashes: &seenRequestIDHashes},
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	form := url.Values{}
	form.Set("RelayState", "relay-secret")
	form.Set("SAMLResponse", testSAMLResponseForRequest("request-1"))
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/saml/providers/provider_a/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got, want := store.consumedRelayStateHash, BrowserSessionSecretHash("relay-secret"); got != want {
		t.Fatalf("relay hash = %q, want %q", got, want)
	}
	if got := store.consumedRequestIDHash; got != requestIDHash {
		t.Fatalf("request id hash = %q, want %q", got, requestIDHash)
	}
	if store.reservedReplayHash == "" || strings.Contains(store.reservedReplayHash, "assertion-1") {
		t.Fatalf("reserved replay hash leaked raw assertion ID: %q", store.reservedReplayHash)
	}
	if got, want := seenRequestIDHashes, []string{requestIDHash}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("verifier request id hashes = %#v, want %#v", got, want)
	}
	if len(store.created) != 1 {
		t.Fatalf("created sessions = %d, want 1", len(store.created))
	}
	created := store.created[0]
	if got, want := created.SessionHash, BrowserSessionSecretHash("session-secret"); got != want {
		t.Fatalf("session hash = %q, want %q", got, want)
	}
	if got, want := created.CSRFTokenHash, BrowserSessionSecretHash("csrf-secret"); got != want {
		t.Fatalf("csrf hash = %q, want %q", got, want)
	}
	if created.SessionHash == "session-secret" || created.CSRFTokenHash == "csrf-secret" {
		t.Fatalf("created session leaked raw secrets: %#v", created)
	}
	if created.SubjectClass != "external_saml" || created.SubjectIDHash != "sha256:subject" {
		t.Fatalf("created auth subject = %q/%q, want resolved SAML subject", created.SubjectClass, created.SubjectIDHash)
	}

	sessionCookie := requireCookie(t, rec.Result(), BrowserSessionCookieName)
	if sessionCookie.Value != "session-secret" || !sessionCookie.HttpOnly || !sessionCookie.Secure {
		t.Fatalf("session cookie attrs = %#v, want secure HttpOnly cookie", sessionCookie)
	}
	if strings.Contains(rec.Body.String(), "request-1") || strings.Contains(rec.Body.String(), "session-secret") {
		t.Fatalf("ACS response leaked raw SAML or session data: %s", rec.Body.String())
	}

	var body BrowserSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if body.Auth.Mode != AuthModeBrowserSession || body.Auth.TenantID != "tenant_a" {
		t.Fatalf("auth response = %#v, want browser session tenant", body.Auth)
	}
}

func TestSAMLHandlerACSRejectsReplayBeforeSessionCreation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 16, 30, 0, 0, time.UTC)
	store := &fakeSAMLStore{
		provider:  testSAMLProvider(),
		requestOK: true,
		replayOK:  false,
	}
	handler := &SAMLHandler{
		Store:     store,
		Sessions:  store,
		Verifier:  fakeSAMLVerifier{},
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	form := url.Values{}
	form.Set("RelayState", "relay-secret")
	form.Set("SAMLResponse", testSAMLResponseForRequest("request-1"))
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/saml/providers/provider_a/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(store.created) != 0 {
		t.Fatalf("created sessions = %d, want none after replay", len(store.created))
	}
}

func testSAMLProvider() SAMLProviderConfig {
	return SAMLProviderConfig{
		ProviderConfigID: "provider_a",
		ServiceProvider: samlauth.ServiceProviderConfig{
			EntityID: "https://api.example.test/api/v0/auth/saml/providers/provider_a/metadata",
			ACSURL:   "https://api.example.test/api/v0/auth/saml/providers/provider_a/acs",
		},
		GroupMapping: samlauth.ClaimMapping{
			GroupAttributeNames: []string{"groups"},
			RequireGroups:       true,
			HashScope:           "tenant_a/provider_a",
		},
	}
}

func testSAMLResponseForRequest(requestID string) string {
	responseXML := `<Response xmlns="urn:oasis:names:tc:SAML:2.0:protocol" ID="response-1" InResponseTo="` + requestID + `"></Response>`
	return base64.StdEncoding.EncodeToString([]byte(responseXML))
}

type fakeSAMLVerifier struct {
	requestIDHashes *[]string
}

func (v fakeSAMLVerifier) VerifySAMLResponse(
	_ context.Context,
	_ SAMLProviderConfig,
	_ string,
	requestIDHashes []string,
) (SAMLAssertion, error) {
	if v.requestIDHashes != nil {
		*v.requestIDHashes = append(*v.requestIDHashes, requestIDHashes...)
	}
	return SAMLAssertion{
		ResponseID:  "response-1",
		AssertionID: "assertion-1",
		Claims: samlauth.AssertionClaims{
			NameID: "user@example.test",
			Attributes: map[string][]string{
				"groups": {"eshu-admins"},
			},
		},
		Window: samlauth.AssertionWindow{
			NotBefore:    time.Date(2026, 6, 22, 15, 59, 0, 0, time.UTC),
			NotOnOrAfter: time.Date(2026, 6, 22, 16, 5, 0, 0, time.UTC),
			ClockSkew:    time.Minute,
		},
	}, nil
}

func TestSAMLHandlerACSRedirectsToReturnToPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC)
	store := &fakeSAMLStore{
		provider:     testSAMLProvider(),
		requestOK:    true,
		resolveOK:    true,
		replayOK:     true,
		returnToPath: "/dashboard/projects",
		sessionAuth: AuthContext{
			Mode:         AuthModeBrowserSession,
			TenantID:     "tenant_a",
			WorkspaceID:  "workspace_a",
			SubjectClass: "external_saml",
		},
	}
	handler := &SAMLHandler{
		Store:     store,
		Sessions:  store,
		Verifier:  fakeSAMLVerifier{},
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	form := url.Values{}
	form.Set("RelayState", "relay-secret")
	form.Set("SAMLResponse", testSAMLResponseForRequest("request-1"))
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/saml/providers/provider_a/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d (303 SeeOther redirect)", rec.Code, http.StatusSeeOther)
	}
	location := rec.Header().Get("Location")
	if location != "/dashboard/projects" {
		t.Fatalf("Location = %q, want %q", location, "/dashboard/projects")
	}
	// Session cookie must be set even when redirecting.
	sessionCookie := requireCookie(t, rec.Result(), BrowserSessionCookieName)
	if sessionCookie.Value != "session-secret" || !sessionCookie.HttpOnly || !sessionCookie.Secure {
		t.Fatalf("session cookie attrs = %#v, want secure HttpOnly cookie", sessionCookie)
	}
}

func TestSAMLHandlerLoginStoresReturnToPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC)
	store := &fakeSAMLStore{
		provider:   testSAMLProvider(),
		providerOK: true,
	}
	handler := &SAMLHandler{
		Store:          store,
		Sessions:       store,
		Verifier:       fakeSAMLVerifier{},
		RequestBuilder: fakeSAMLRequestBuilder{},
		NewSecret:      sequenceSecrets("relay-secret"),
		Now:            func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/saml/providers/provider_a/login?return_to=/dashboard", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if store.createdRequest.ReturnToPath != "/dashboard" {
		t.Fatalf("stored ReturnToPath = %q, want %q", store.createdRequest.ReturnToPath, "/dashboard")
	}
}

func TestSAMLHandlerLoginRejectsMaliciousReturnTo(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 16, 0, 0, 0, time.UTC)

	for _, badPath := range []string{
		"//evil.example.com/steal",
		"http://evil.example.com/steal",
		"/path\r\nX-Injected: header",
		"\\UNC\\path",
	} {
		badPath := badPath // capture loop var
		store := &fakeSAMLStore{
			provider:   testSAMLProvider(),
			providerOK: true,
		}
		handler := &SAMLHandler{
			Store:          store,
			Sessions:       store,
			Verifier:       fakeSAMLVerifier{},
			RequestBuilder: fakeSAMLRequestBuilder{},
			NewSecret:      sequenceSecrets("relay-secret"),
			Now:            func() time.Time { return now },
		}
		mux := http.NewServeMux()
		handler.Mount(mux)

		req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/saml/providers/provider_a/login?return_to="+url.QueryEscape(badPath), nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		// Login should still redirect to IdP; the malicious return_to is silently
		// dropped (stored as empty string, not as an error response).
		if rec.Code != http.StatusFound {
			t.Fatalf("path %q: status = %d, want %d", badPath, rec.Code, http.StatusFound)
		}
		if store.createdRequest.ReturnToPath != "" {
			t.Fatalf("path %q: stored ReturnToPath = %q, want empty (rejected)", badPath, store.createdRequest.ReturnToPath)
		}
	}
}

type fakeSAMLRequestBuilder struct{}

func (fakeSAMLRequestBuilder) BuildSAMLRedirect(
	context.Context,
	SAMLProviderConfig,
	string,
) (SAMLAuthnRequest, error) {
	return SAMLAuthnRequest{
		RequestID:   "request-1",
		RedirectURL: "https://idp.example.test/sso?SAMLRequest=request&RelayState=relay-secret",
	}, nil
}
