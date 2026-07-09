// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
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

// ParseCookieSecureMode normalizes an already-validated CookieSecureMode
// struct field (BrowserSessionHandler.CookieSecure and its siblings) at
// request time: empty (the Go zero value, e.g. in a test that never sets the
// field) defaults to CookieSecureAuto; any other value passes through
// unchanged. Callers holding raw operator input (an ESHU_AUTH_COOKIE_SECURE
// env value) MUST use ValidateCookieSecureMode instead, which fails closed
// on an unrecognized value rather than normalizing it.
func ParseCookieSecureMode(value string) CookieSecureMode {
	if strings.TrimSpace(value) == "" {
		return CookieSecureAuto
	}
	return CookieSecureMode(value)
}

// ValidateCookieSecureMode parses an ESHU_AUTH_COOKIE_SECURE value at
// startup, matching the documented cmd/api convention for constrained enum
// env vars (ParseQueryProfile, ParseGraphBackend): an unrecognized,
// non-empty value returns an error so wireAPI fails startup closed instead
// of silently guessing an operator's intent. Empty defaults to
// CookieSecureAuto, matching the documented default. Comparison is
// case-insensitive and trims surrounding whitespace.
func ValidateCookieSecureMode(value string) (CookieSecureMode, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	switch CookieSecureMode(trimmed) {
	case "":
		return CookieSecureAuto, nil
	case CookieSecureAuto:
		return CookieSecureAuto, nil
	case CookieSecureAlways:
		return CookieSecureAlways, nil
	default:
		return "", fmt.Errorf("%s: unrecognized value %q, want %q or %q", CookieSecureModeEnv, value, CookieSecureAuto, CookieSecureAlways)
	}
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

// browserSessionCookieNames returns the (session, csrf) cookie names to use
// for a Set-Cookie under secure: the __Host--prefixed names when secure is
// true, or the bare insecure names when secure is false. A __Host--prefixed
// cookie sent with Secure=false is invalid per RFC 6265bis and browsers
// reject it outright, so the relaxed CookieSecureAuto loopback path must
// never pair the __Host- name with Secure=false (#4964).
func browserSessionCookieNames(secure bool) (session, csrf string) {
	if secure {
		return BrowserSessionCookieName, BrowserSessionCSRFCookieName
	}
	return BrowserSessionCookieNameInsecure, BrowserSessionCSRFCookieNameInsecure
}

// browserSessionCookieValue returns the raw dashboard session cookie value
// from r, preferring the __Host--prefixed cookie (BrowserSessionCookieName,
// set for a Secure context) and falling back to the bare insecure name
// (BrowserSessionCookieNameInsecure, set only by CookieSecureAuto's
// plain-HTTP loopback relaxation). Exactly one of the two names is ever set
// for a given session (see browserSessionCookieNames), so trying both here
// is how every read path accepts a session regardless of which mode issued
// it. Returns false when neither cookie is present or both are empty.
func browserSessionCookieValue(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	for _, name := range []string{BrowserSessionCookieName, BrowserSessionCookieNameInsecure} {
		if cookie, err := r.Cookie(name); err == nil && strings.TrimSpace(cookie.Value) != "" {
			return cookie.Value, true
		}
	}
	return "", false
}
