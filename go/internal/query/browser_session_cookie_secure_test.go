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

func TestParseCookieSecureMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  CookieSecureMode
	}{
		{name: "empty defaults to auto", value: "", want: CookieSecureAuto},
		{name: "explicit auto", value: "auto", want: CookieSecureAuto},
		{name: "explicit always", value: "always", want: CookieSecureAlways},
		{name: "case insensitive always", value: "ALWAYS", want: CookieSecureAlways},
		{name: "surrounding whitespace", value: "  always  ", want: CookieSecureAlways},
		{name: "unrecognized value defaults to auto", value: "sometimes", want: CookieSecureAuto},
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
	sessionCookie := requireCookie(t, rec.Result(), BrowserSessionCookieName)
	if sessionCookie.Secure {
		t.Fatalf("session cookie Secure = true over plain-HTTP loopback, want relaxed to false")
	}
	csrfCookie := requireCookie(t, rec.Result(), BrowserSessionCSRFCookieName)
	if csrfCookie.Secure {
		t.Fatalf("csrf cookie Secure = true over plain-HTTP loopback, want relaxed to false")
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
