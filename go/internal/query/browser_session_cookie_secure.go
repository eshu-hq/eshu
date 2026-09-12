// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
)

// CookieSecureMode selects how the browser session and CSRF cookies decide
// the Secure attribute across the BrowserSessionHandler, LocalIdentityHandler,
// and SAMLHandler login paths. It lives in queryauth (#6642) so a
// handler-family subpackage can name it without importing this package. See
// CookieSecureModeEnv for the operator switch and queryauth's unexported
// browserSessionCookieSecure for the per-request decision (#4964).
type CookieSecureMode = queryauth.CookieSecureMode

// Compatibility constants preserve this package's public contract. They
// moved to queryauth (#6642); see queryauth for the doc comments.
const (
	CookieSecureAuto    = queryauth.CookieSecureAuto
	CookieSecureAlways  = queryauth.CookieSecureAlways
	CookieSecureModeEnv = queryauth.CookieSecureModeEnv
)

// ParseCookieSecureMode forwards to queryauth.ParseCookieSecureMode. The
// implementation moved there for #6642 so a handler-family subpackage can
// normalize a CookieSecureMode field without importing this package.
func ParseCookieSecureMode(value string) CookieSecureMode {
	return queryauth.ParseCookieSecureMode(value)
}

// ValidateCookieSecureMode forwards to queryauth.ValidateCookieSecureMode.
// The implementation moved there for #6642.
func ValidateCookieSecureMode(value string) (CookieSecureMode, error) {
	return queryauth.ValidateCookieSecureMode(value)
}

// browserSessionCookieValue returns the raw dashboard session cookie value
// from r, preferring the __Host--prefixed cookie (BrowserSessionCookieName,
// set for a Secure context) and falling back to the bare insecure name
// (BrowserSessionCookieNameInsecure, set only by CookieSecureAuto's
// plain-HTTP loopback relaxation). Exactly one of the two names is ever set
// for a given session, so trying both here is how every read path accepts a
// session regardless of which mode issued it. Returns false when neither
// cookie is present or both are empty.
//
// Not part of the #6642 hoist: only this package's own tryBrowserSessionAuth
// (auth.go) and browserSessionHashFromCookie (browser_session_handler.go)
// call it.
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
