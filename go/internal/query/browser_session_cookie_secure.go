// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net"
	"net/http"
	"strings"
)

// CookieSecureMode selects how the browser session and CSRF cookies decide
// the Secure attribute across the BrowserSessionHandler, LocalIdentityHandler,
// and SAMLHandler login paths. See CookieSecureModeEnv for the operator
// switch and browserSessionCookieSecure for the per-request decision (#4964).
type CookieSecureMode string

const (
	// CookieSecureAuto is the default. Secure stays set for every request
	// except a plain-HTTP loopback origin (localhost, 127.0.0.1, ::1), where
	// it relaxes to false so a session cookie persists for local development
	// without TLS. Every other plain-HTTP origin still gets Secure=true, so
	// the browser silently discards the cookie rather than this server ever
	// issuing a non-Secure cookie outside loopback.
	CookieSecureAuto CookieSecureMode = "auto"
	// CookieSecureAlways restores the pre-#4964 behavior: Secure is always
	// set on the session and CSRF cookies, regardless of request origin.
	CookieSecureAlways CookieSecureMode = "always"
)

// CookieSecureModeEnv is the operator-facing gate that selects
// CookieSecureMode. Unset or "auto" keeps the loopback-only relaxation;
// "always" disables it and restores the pre-#4964 always-Secure behavior.
const CookieSecureModeEnv = "ESHU_AUTH_COOKIE_SECURE"

// ParseCookieSecureMode parses an ESHU_AUTH_COOKIE_SECURE value. Empty or
// unrecognized values fall back to CookieSecureAuto: that keeps the
// loopback-only relaxation as the safe default rather than silently
// widening or narrowing it on a typo.
func ParseCookieSecureMode(value string) CookieSecureMode {
	if CookieSecureMode(strings.ToLower(strings.TrimSpace(value))) == CookieSecureAlways {
		return CookieSecureAlways
	}
	return CookieSecureAuto
}

// browserSessionCookieSecure reports whether the Secure attribute should be
// set on the browser session and CSRF cookies for r under mode. Any
// non-CookieSecureAuto mode (currently only CookieSecureAlways) always
// returns true, matching the pre-#4964 behavior exactly. Under
// CookieSecureAuto, a TLS request (r.TLS set directly, or a reverse proxy
// asserting X-Forwarded-Proto: https) always returns true; a plain-HTTP
// request returns true unless its Host is a loopback address (localhost,
// 127.0.0.1, ::1), in which case it relaxes to false so local development
// without TLS keeps a persistent session cookie. A plain-HTTP request to any
// non-loopback Host always keeps Secure=true, so the browser drops the
// cookie rather than this server ever issuing a non-Secure cookie to a
// deployment reached over plain HTTP outside loopback.
func browserSessionCookieSecure(r *http.Request, mode CookieSecureMode) bool {
	if mode != CookieSecureAuto {
		return true
	}
	if requestIsTLS(r) {
		return true
	}
	return !requestHostIsLoopback(r)
}

// requestIsTLS reports whether r arrived over TLS: either terminated
// directly by this process (r.TLS != nil) or by a reverse proxy that
// asserted X-Forwarded-Proto: https.
func requestIsTLS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

// requestHostIsLoopback reports whether r.Host names a loopback address
// (127.0.0.0/8 or ::1) or the literal hostname "localhost", ignoring any
// port and IPv6 brackets.
func requestHostIsLoopback(r *http.Request) bool {
	if r == nil {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
