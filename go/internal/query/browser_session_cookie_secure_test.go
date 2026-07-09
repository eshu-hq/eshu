// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestParseCookieSecureMode proves the request-time normalizer's narrow
// contract: it only defaults the Go zero value (an empty struct field, e.g.
// a test's &BrowserSessionHandler{} that never sets CookieSecure) to
// CookieSecureAuto, and otherwise passes an already-validated mode through
// unchanged. Raw, potentially-malformed operator input (an env var value)
// goes through ValidateCookieSecureMode instead, which fails closed.
func TestParseCookieSecureMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  CookieSecureMode
	}{
		{name: "empty defaults to auto", value: "", want: CookieSecureAuto},
		{name: "already-validated auto passes through", value: string(CookieSecureAuto), want: CookieSecureAuto},
		{name: "already-validated always passes through", value: string(CookieSecureAlways), want: CookieSecureAlways},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ParseCookieSecureMode(tt.value); got != tt.want {
				t.Fatalf("ParseCookieSecureMode(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

// TestValidateCookieSecureMode proves ESHU_AUTH_COOKIE_SECURE fails closed at
// startup on an unrecognized value, matching the documented cmd/api
// convention for constrained enum env vars (ParseQueryProfile,
// ParseGraphBackend both return an error that aborts startup via wireAPI).
// Unlike ParseCookieSecureMode (used for the already-validated struct field
// at request time), this is the operator-input boundary and must not
// silently normalize a typo.
func TestValidateCookieSecureMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    CookieSecureMode
		wantErr bool
	}{
		{name: "empty defaults to auto", value: "", want: CookieSecureAuto},
		{name: "explicit auto", value: "auto", want: CookieSecureAuto},
		{name: "explicit always", value: "always", want: CookieSecureAlways},
		{name: "case insensitive always", value: "ALWAYS", want: CookieSecureAlways},
		{name: "surrounding whitespace", value: "  always  ", want: CookieSecureAlways},
		{name: "unrecognized value fails closed", value: "sometimes", wantErr: true},
		{name: "typo of always fails closed", value: "alway", wantErr: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateCookieSecureMode(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateCookieSecureMode(%q) error = nil, want error", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateCookieSecureMode(%q) error = %v, want nil", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("ValidateCookieSecureMode(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestBrowserSessionCookieSecure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		host  string
		tls   bool
		proto string
		mode  CookieSecureMode
		want  bool
	}{
		{
			name: "tls request always secure under auto",
			host: "eshu.example.com",
			tls:  true,
			mode: CookieSecureAuto,
			want: true,
		},
		{
			name: "tls request always secure under always",
			host: "127.0.0.1:8443",
			tls:  true,
			mode: CookieSecureAlways,
			want: true,
		},
		{
			// Proves the TLS check is not dead code relative to the loopback
			// check: a genuinely TLS connection to a loopback host (e.g. a
			// local dev server with a self-signed cert on https://localhost)
			// must keep Secure=true, not relax it just because the Host is
			// loopback. Only a non-TLS loopback request relaxes.
			name: "tls request to loopback host stays secure under auto",
			host: "localhost:8443",
			tls:  true,
			mode: CookieSecureAuto,
			want: true,
		},
		{
			name: "plain http localhost relaxes under auto",
			host: "localhost:8080",
			mode: CookieSecureAuto,
			want: false,
		},
		{
			name: "plain http 127.0.0.1 relaxes under auto",
			host: "127.0.0.1:8080",
			mode: CookieSecureAuto,
			want: false,
		},
		{
			name: "plain http ipv6 loopback relaxes under auto",
			host: "[::1]:8080",
			mode: CookieSecureAuto,
			want: false,
		},
		{
			name: "plain http non-loopback host stays secure under auto",
			host: "console.internal.example.com",
			mode: CookieSecureAuto,
			want: true,
		},
		{
			name: "plain http localhost stays secure under always",
			host: "localhost:8080",
			mode: CookieSecureAlways,
			want: true,
		},
		{
			name:  "forwarded https header treated as tls under auto",
			host:  "console.internal.example.com",
			proto: "https",
			mode:  CookieSecureAuto,
			want:  true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/browser-session", nil)
			req.Host = tt.host
			if tt.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if tt.proto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.proto)
			}
			if got := browserSessionCookieSecure(req, tt.mode); got != tt.want {
				t.Fatalf("browserSessionCookieSecure(host=%q, tls=%v, proto=%q, mode=%q) = %v, want %v",
					tt.host, tt.tls, tt.proto, tt.mode, got, tt.want)
			}
		})
	}
}

// TestBrowserSessionHandlerCreateRelaxesSecureOnPlainHTTPLoopback proves the
// end-to-end #4964 regression through the real handler path (not a
// reimplementation): a plain-HTTP request to a loopback Host gets a
// non-Secure cookie under the default auto mode, so the console can persist
// a session when reached over http://localhost without TLS.
func TestBrowserSessionHandlerCreateRelaxesSecureOnPlainHTTPLoopback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionStore{}
	handler := &BrowserSessionHandler{
		Store:     store,
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/browser-session", nil)
	req.Host = "localhost:8080"
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant_a",
		WorkspaceID: "workspace_a",
		AllScopes:   true,
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	// The __Host- prefix (RFC 6265bis) requires Secure=true or the browser
	// rejects the cookie outright, so a relaxed Secure=false cookie must NOT
	// use the __Host- name — otherwise the browser silently drops it and
	// #4964's bounce-to-login bug comes right back under auto mode itself.
	if _, ok := findCookie(rec.Result(), BrowserSessionCookieName); ok {
		t.Fatalf("__Host- session cookie present with relaxed Secure=false; browsers will reject it (RFC 6265bis)")
	}
	if _, ok := findCookie(rec.Result(), BrowserSessionCSRFCookieName); ok {
		t.Fatalf("__Host- csrf cookie present with relaxed Secure=false; browsers will reject it (RFC 6265bis)")
	}
	sessionCookie := requireCookie(t, rec.Result(), BrowserSessionCookieNameInsecure)
	if sessionCookie.Secure {
		t.Fatalf("session cookie Secure = true over plain-HTTP loopback, want relaxed to false")
	}
	csrfCookie := requireCookie(t, rec.Result(), BrowserSessionCSRFCookieNameInsecure)
	if csrfCookie.Secure {
		t.Fatalf("csrf cookie Secure = true over plain-HTTP loopback, want relaxed to false")
	}
}

// findCookie returns the named cookie from res if present, without failing
// the test — used to assert a cookie is ABSENT (unlike requireCookie, which
// fails when the cookie is missing).
func findCookie(res *http.Response, name string) (*http.Cookie, bool) {
	for _, cookie := range res.Cookies() {
		if cookie.Name == name {
			return cookie, true
		}
	}
	return nil, false
}

// TestBrowserSessionHandlerRoundTripsRelaxedInsecureCookie proves the other
// half of the __Host- fix: a session created under the relaxed, bare-named
// cookie (BrowserSessionCookieNameInsecure) must still authenticate on a
// later request through the real auth middleware — the fix is not just
// "the write side stopped using __Host-", the read side must accept the
// fallback name too, or the session would silently never re-authenticate.
func TestBrowserSessionHandlerRoundTripsRelaxedInsecureCookie(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionStore{}
	handler := &BrowserSessionHandler{
		Store:     store,
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	createReq := httptest.NewRequest(http.MethodPost, "/api/v0/auth/browser-session", nil)
	createReq.Host = "localhost:8080"
	createReq = createReq.WithContext(ContextWithAuthContext(createReq.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant_a",
		WorkspaceID: "workspace_a",
		AllScopes:   true,
	}))
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	issuedCookie := requireCookie(t, createRec.Result(), BrowserSessionCookieNameInsecure)

	resolver := &fakeBrowserSessionResolver{
		context: AuthContext{
			Mode:        AuthModeBrowserSession,
			TenantID:    "tenant_a",
			WorkspaceID: "workspace_a",
		},
		ok: true,
	}
	authed := AuthMiddlewareWithBrowserSessionsAndScopedTokens("", nil, resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	readReq := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	readReq.AddCookie(&http.Cookie{Name: BrowserSessionCookieNameInsecure, Value: issuedCookie.Value})
	readRec := httptest.NewRecorder()
	authed.ServeHTTP(readRec, readReq)

	if readRec.Code != http.StatusOK {
		t.Fatalf("authed status = %d, want %d: %s — the relaxed cookie name did not authenticate", readRec.Code, http.StatusOK, readRec.Body.String())
	}
	if got, want := resolver.sessionHash, BrowserSessionSecretHash(issuedCookie.Value); got != want {
		t.Fatalf("resolver session hash = %q, want %q", got, want)
	}
}

// TestBrowserSessionHandlerLogoutClearsBothCookieNameVariants proves logout
// clears both the __Host- and bare-name cookie variants unconditionally, so
// a session's cookie is removed regardless of which mode issued it.
func TestBrowserSessionHandlerLogoutClearsBothCookieNameVariants(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 8, 13, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionStore{}
	handler := &BrowserSessionHandler{
		Store: store,
		Now:   func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/v0/auth/browser-session", nil)
	req.AddCookie(&http.Cookie{Name: BrowserSessionCookieNameInsecure, Value: "session-secret"})
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode: AuthModeBrowserSession,
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	for _, name := range []string{
		BrowserSessionCookieName,
		BrowserSessionCookieNameInsecure,
		BrowserSessionCSRFCookieName,
		BrowserSessionCSRFCookieNameInsecure,
	} {
		cookie := requireCookie(t, rec.Result(), name)
		if cookie.MaxAge != -1 {
			t.Fatalf("cookie %q was not cleared: MaxAge = %d, want -1", name, cookie.MaxAge)
		}
	}
	hostSession := requireCookie(t, rec.Result(), BrowserSessionCookieName)
	if !hostSession.Secure {
		t.Fatalf("__Host- session clear must keep Secure=true or browsers ignore it")
	}
	bareSession := requireCookie(t, rec.Result(), BrowserSessionCookieNameInsecure)
	if bareSession.Secure {
		t.Fatalf("bare session clear must use Secure=false or a plain-HTTP browser ignores it")
	}
}

// TestBrowserSessionHandlerCreateKeepsSecureOnPlainHTTPNonLoopback proves the
// security-sensitive half of #4964: a plain-HTTP request to any non-loopback
// Host must never receive a non-Secure cookie under the default auto mode,
// even though the request is not TLS. The browser will discard such a
// cookie rather than persist a session without Secure outside loopback.
func TestBrowserSessionHandlerCreateKeepsSecureOnPlainHTTPNonLoopback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionStore{}
	handler := &BrowserSessionHandler{
		Store:     store,
		NewSecret: sequenceSecrets("session-secret", "csrf-secret"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/browser-session", nil)
	req.Host = "console.internal.example.com"
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant_a",
		WorkspaceID: "workspace_a",
		AllScopes:   true,
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	sessionCookie := requireCookie(t, rec.Result(), BrowserSessionCookieName)
	if !sessionCookie.Secure {
		t.Fatalf("session cookie Secure = false over plain-HTTP non-loopback, want Secure to stay true")
	}
}

// TestBrowserSessionHandlerCreateAlwaysModeIgnoresLoopback proves
// ESHU_AUTH_COOKIE_SECURE=always restores the pre-#4964 behavior: even a
// plain-HTTP loopback request keeps Secure set.
func TestBrowserSessionHandlerCreateAlwaysModeIgnoresLoopback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	store := &fakeBrowserSessionStore{}
	handler := &BrowserSessionHandler{
		Store:        store,
		NewSecret:    sequenceSecrets("session-secret", "csrf-secret"),
		Now:          func() time.Time { return now },
		CookieSecure: CookieSecureAlways,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/browser-session", nil)
	req.Host = "localhost:8080"
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant_a",
		WorkspaceID: "workspace_a",
		AllScopes:   true,
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	sessionCookie := requireCookie(t, rec.Result(), BrowserSessionCookieName)
	if !sessionCookie.Secure {
		t.Fatalf("session cookie Secure = false under always mode, want Secure to stay true")
	}
}
