// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryauth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultBrowserSessionIdleTimeout is the dashboard browser session idle
	// window. Moved from root package query's browser_session_handler.go
	// (#6642) so a handler-family subpackage can name it without importing
	// root.
	DefaultBrowserSessionIdleTimeout = 30 * time.Minute
	// DefaultBrowserSessionAbsoluteTimeout is the maximum browser session
	// lifetime. Moved alongside DefaultBrowserSessionIdleTimeout.
	DefaultBrowserSessionAbsoluteTimeout = 12 * time.Hour
)

const (
	// BrowserSessionCookieName is the host-scoped HttpOnly dashboard session
	// cookie, set only when the Secure attribute is applied. The __Host-
	// prefix (RFC 6265bis) requires Secure, no Domain attribute, and Path=/;
	// browsers reject the cookie outright if Secure is missing. Moved from
	// root package query's auth.go (#6642).
	BrowserSessionCookieName = "__Host-eshu_session"
	// BrowserSessionCSRFCookieName is the readable host-scoped CSRF cookie,
	// set only when the Secure attribute is applied. See
	// BrowserSessionCookieName.
	BrowserSessionCSRFCookieName = "__Host-eshu_csrf"
	// BrowserSessionCookieNameInsecure is the dashboard session cookie name
	// used only when CookieSecureAuto relaxes Secure for a plain-HTTP
	// loopback origin (#4964). It cannot use the __Host- prefix: a
	// __Host--prefixed cookie sent with Secure=false is invalid per RFC
	// 6265bis and browsers silently drop it, which would reintroduce the
	// exact silent session-loss bug #4964 fixes. Readers must check both
	// this name and BrowserSessionCookieName.
	BrowserSessionCookieNameInsecure = "eshu_session"
	// BrowserSessionCSRFCookieNameInsecure is the readable CSRF cookie name
	// used alongside BrowserSessionCookieNameInsecure. See its doc comment.
	BrowserSessionCSRFCookieNameInsecure = "eshu_csrf"
)

// CookieSecureMode selects how the browser session and CSRF cookies decide
// the Secure attribute across the BrowserSessionHandler, LocalIdentityHandler,
// and SAMLHandler login paths in root package query. Moved from root's
// browser_session_cookie_secure.go (#6642) so a handler-family subpackage can
// read and validate it without importing root. See CookieSecureModeEnv for
// the operator switch and the unexported browserSessionCookieSecure for the
// per-request decision (#4964).
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

// BrowserSessionSecretHash returns the durable hash for a session or CSRF
// secret. It returns an empty string for blank input so missing CSRF headers
// cannot hash into a meaningful value. Moved from root package query's
// auth.go (#6642).
func BrowserSessionSecretHash(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return "sha256:" + hex.EncodeToString(sum[:])
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

// WriteBrowserSessionCookies writes (or, when maxAge <= 0, clears) the
// dashboard session and CSRF cookie pair. Moved from root package query's
// browser_session_handler.go (writeBrowserSessionCookies, #6642) so a
// handler-family subpackage can issue and clear browser session cookies
// without importing root.
func WriteBrowserSessionCookies(
	w http.ResponseWriter,
	r *http.Request,
	mode CookieSecureMode,
	sessionSecret string,
	csrfSecret string,
	expiresAt time.Time,
	maxAge int,
) {
	if maxAge <= 0 {
		clearBrowserSessionCookies(w)
		return
	}
	secure := browserSessionCookieSecure(r, mode)
	sessionName, csrfName := browserSessionCookieNames(secure)
	expires := expiresAt.UTC()
	// #nosec G124 -- HttpOnly and SameSite=Strict are set unconditionally below; Secure is browserSessionCookieSecure(r, mode) so plain-HTTP local development works, and the insecure variant carries its own cookie name so it cannot be mistaken for the HTTPS cookie
	http.SetCookie(w, &http.Cookie{
		Name:     sessionName,
		Value:    sessionSecret,
		Path:     "/",
		MaxAge:   maxAge,
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
	// #nosec G124 -- same attributes as the session cookie above; Secure follows the resolved cookie mode and the insecure variant has its own CSRF cookie name
	http.SetCookie(w, &http.Cookie{
		Name:     csrfName,
		Value:    csrfSecret,
		Path:     "/",
		MaxAge:   maxAge,
		Expires:  expires,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearBrowserSessionCookies expires both the __Host--prefixed cookie
// variant and the bare insecure variant used by CookieSecureAuto's loopback
// relaxation (#4964), so logout removes whichever variant the browser
// actually holds regardless of which mode issued it -- the handler has no
// durable record of which name a given browser used. The __Host- clear must
// keep Secure=true: RFC 6265bis applies the same __Host- validity criteria
// to a deleting Set-Cookie as to a creating one, and browsers additionally
// ignore any Secure Set-Cookie (creating or deleting) received over a
// non-HTTPS connection. The bare-name clear must use Secure=false for the
// mirror-image reason: it is the variant a plain-HTTP loopback browser can
// actually hold, and a Secure Set-Cookie sent back over that same
// connection would be ignored, leaving the cookie stuck.
func clearBrowserSessionCookies(w http.ResponseWriter) {
	expired := time.Unix(0, 0).UTC()
	sessionVariants := []struct {
		name   string
		secure bool
	}{
		{BrowserSessionCookieName, true},
		{BrowserSessionCookieNameInsecure, false},
	}
	for _, v := range sessionVariants {
		// #nosec G124 -- expiry write (MaxAge -1, empty value) for both the secure and the insecure-named session cookie
		http.SetCookie(w, &http.Cookie{
			Name:     v.name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			Expires:  expired,
			HttpOnly: true,
			Secure:   v.secure,
			SameSite: http.SameSiteStrictMode,
		})
	}
	csrfVariants := []struct {
		name   string
		secure bool
	}{
		{BrowserSessionCSRFCookieName, true},
		{BrowserSessionCSRFCookieNameInsecure, false},
	}
	for _, v := range csrfVariants {
		// #nosec G124 -- expiry write (MaxAge -1, empty value) for both the secure and the insecure-named CSRF cookie
		http.SetCookie(w, &http.Cookie{
			Name:     v.name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			Expires:  expired,
			HttpOnly: false,
			Secure:   v.secure,
			SameSite: http.SameSiteStrictMode,
		})
	}
}
