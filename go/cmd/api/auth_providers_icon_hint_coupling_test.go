// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestIconHintCoupledToDisplayLabelForLoginFacingKinds is a coupling guard
// between displayLabelForKind and iconHintForKind: for every provider_kind
// candidate that displayLabelForKind treats as login-facing (returns a
// non-empty label — the console only renders a login picker entry for
// those), iconHintForKind must return a value in the OpenAPI icon_hint enum
// {"oidc", "saml", "github"}, never "". See PR #5365 review thread on
// go/internal/query/openapi_paths_auth.go:62 (linuxdynasty, P2): the two
// switches are hand-maintained in parallel and nothing else enforces they
// stay in sync.
//
// This deliberately does not adopt the reviewer's suggested fallback (making
// iconHintForKind's default case return a fixed value like "oidc" for every
// unrecognized kind) — that would silently render the wrong glyph for a
// genuinely new, distinct provider family instead of failing loudly here
// first. Issue #5166 (F-5) is the case this guarded against: adding
// "external_github"/"github" to displayLabelForKind's login-facing switch
// required extending iconHintForKind's case AND this test's validIconHints
// set AND the OpenAPI icon_hint/provider_kind enums
// (openapi_paths_auth.go) AND the console TS type
// (apps/console/src/api/authSession.ts) in the same change — this test is
// the guard that would have failed loudly had any one of those four been
// missed.
//
// The candidate list intentionally includes kind strings NOT YET handled by
// either function (e.g. "gitlab", "bitbucket"). Today those are non-login
// -facing (displayLabelForKind returns ""), so the guard is vacuously
// satisfied for them and the test passes. The moment a future change adds
// such a kind to displayLabelForKind's login-facing switch without a
// matching iconHintForKind case, this same test — unchanged — starts
// failing, because the kind flips to login-facing while its icon hint stays
// "".
func TestIconHintCoupledToDisplayLabelForLoginFacingKinds(t *testing.T) {
	t.Parallel()

	candidates := []string{
		"external_oidc", "oidc",
		"external_saml", "saml",
		"external_github", "github",
		"local", "",
		// Kinds not currently recognized by either function — see the doc
		// comment above for why these are included now.
		"gitlab", "bitbucket", "google", "microsoft", "okta", "ldap",
	}
	validIconHints := map[string]bool{"oidc": true, "saml": true, "github": true}

	for _, kind := range candidates {
		label := displayLabelForKind(kind)
		if label == "" {
			// Not login-facing per displayLabelForKind — no icon_hint
			// constraint applies to this kind.
			continue
		}
		hint := iconHintForKind(kind)
		if !validIconHints[hint] {
			t.Errorf("displayLabelForKind(%q) = %q (login-facing) but iconHintForKind(%q) = %q, want one of {\"oidc\", \"saml\"}", kind, label, kind, hint)
		}
	}
}
