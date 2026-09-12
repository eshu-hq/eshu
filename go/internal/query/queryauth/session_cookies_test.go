// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryauth

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBrowserSessionCookieSecure was moved from root package query's
// browser_session_cookie_secure_test.go under the same name (#6642): it is a
// white-box test of the unexported browserSessionCookieSecure, which moved
// here alongside CookieSecureMode and WriteBrowserSessionCookies and has no
// root forwarder (nothing outside this package calls it directly).
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
