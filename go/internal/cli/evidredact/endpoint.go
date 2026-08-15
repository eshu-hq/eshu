// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidredact

import (
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/mcpsetup"
	"github.com/eshu-hq/eshu/go/internal/urlredact"
)

// Marker is the placeholder this package substitutes for a removed credential,
// in userinfo and in a query value alike. It carries no separator, so a redacted
// endpoint stays a fixed point when the report is re-rendered from a saved
// envelope.
//
// It is deliberately not urlredact.FreeTextMarker. That one replaces a whole
// span of prose and reads as prose; this one stands in the value half of a pair
// an operator still has to read as a URL.
const Marker = "redacted"

// Endpoint returns a display-safe form of an endpoint URL. A URL can carry a
// credential in three places and all three are closed here: embedded userinfo
// (user:password@), a query parameter with a credential-shaped name, and the
// fragment. The scheme, host, path, and every other query parameter remain so
// the operator can still recognize the target. A value that does not parse as a
// URL is masked through mcpsetup.RedactToken so a credential-looking string
// never survives verbatim.
//
// Each place was open until measured, and the count in this comment was wrong
// before the fragment was added to it:
//
//   - Query: "could not reach http://127.0.0.1:8080/x?api_key=<credential>"
//     came out verbatim while the same credential in userinfo was removed.
//     Nothing upstream could help, because every composed-text path ends up
//     back in this function.
//   - Fragment: url.Parse lifts "#…" into Fragment and String() re-emits it
//     untouched, so the canonical OAuth implicit-grant callback
//     "https://app.example.com/cb#access_token=<credential>" survived whole.
//
// The whole fragment goes, not just its credential-named pairs. A fragment is
// client-side and never reaches the server, so it contributes nothing to the
// target recognition that is the stated reason the rest of the URL is kept.
func Endpoint(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return mcpsetup.RedactToken(trimmed)
	}
	if parsed.User != nil {
		parsed.User = url.User(Marker)
	}
	// urlredact owns the pair boundary for this walk and for
	// internal/reportbundle's. Splitting a query string here by hand is how the
	// two drifted: this one knew only "&", so "?a=1;token=<credential>" and
	// "?next=/v0/y?api_key=<credential>" reached the artifact whole.
	parsed.RawQuery = urlredact.Query(parsed.RawQuery, Marker)
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String()
}

// Path returns a display-safe form of a filesystem path target. Absolute host
// paths can leak a username or private layout, so only the final path element is
// kept with a leading ellipsis. Relative paths and bare names are returned
// unchanged because they carry no host-specific secret.
func Path(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	base := trimmed[strings.LastIndex(trimmed, "/")+1:]
	if base == "" {
		return ".../"
	}
	return ".../" + base
}
