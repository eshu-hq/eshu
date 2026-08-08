// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package authsafe

import "strings"

// ReturnPath returns path when it is safe to redirect a browser to after a
// sign-in, and "" when it is not. Callers treat "" as "use the configured
// default landing path" — a rejected value must never be redirected to.
//
// A sign-in flow takes the post-login destination from the request, so an
// unchecked value is an open redirect: an attacker sends a victim to the real
// login page with a return path pointing at their own host, the victim
// authenticates for real, and the redirect lands them somewhere hostile with
// the login flow's own credibility behind it.
//
// Four rejections, each closing a different way to leave this origin:
//
//   - empty or whitespace-only, which carries no destination at all;
//   - anything not starting with "/", which covers absolute URLs
//     ("https://evil.test") and scheme-relative or opaque targets
//     ("javascript:...", "mailto:...");
//   - a leading "//", which a browser reads as protocol-relative — "//evil.test"
//     is a different host, not a path on this one;
//   - CR, LF or TAB anywhere, which are the header-injection characters: a
//     value carrying them can split the Location header and inject a second
//     response.
//
// What it deliberately does NOT do is resolve "..". "/app/../../etc" stays
// inside this origin — the browser normalises it before the request, and the
// server routes whatever results — so traversal is the router's concern rather
// than the redirect's. #5388 raised blocking ".." as a possible future
// tightening; if that lands, it lands here once instead of in three places.
//
// This is the single copy. It previously existed three times, byte-identical:
// safeGitHubReturnPath in internal/query, and safeReturnPath in each of
// internal/githublogin and internal/oidclogin. Three copies of an
// open-redirect check is the shape where one gets tightened and the others do
// not (#5388).
func ReturnPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return ""
	}
	if strings.ContainsAny(path, "\r\n\t") {
		return ""
	}
	return path
}
