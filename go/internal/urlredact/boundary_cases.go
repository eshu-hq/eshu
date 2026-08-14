// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package urlredact

import (
	"fmt"
	"strings"
)

// Sentinel is the synthetic credential every BoundaryCase carries. It is not a
// real secret; it exists so a walk that leaves a credential behind fails with
// the offending row named, instead of passing an assertion one field over.
const Sentinel = "URLREDACT-SENTINEL-9f2a"

// BoundaryCase is one row of the shared conformance corpus: the same bytes
// handed to both redaction walks, with the exact output each must produce.
//
// The two walks replace a credential with different text on purpose —
// cmd/eshu's redactEndpoint keeps the parameter name and writes "redacted" as
// the value so an operator can still see WHICH parameter was dropped, while
// internal/reportbundle's free-text scan removes the name too, because leaving
// "api_key=" standing would be re-found on the next pass and Capture runs
// Validate over its own output. So WantEndpoint and WantFreeText differ on most
// rows and that difference is not itself interesting.
//
// What must hold on every row is narrower and is the thing that actually broke:
// the credential is gone from both outputs, and the text around it survives.
type BoundaryCase struct {
	// Name identifies the row in test output.
	Name string
	// Input is the identical byte sequence given to both walks.
	Input string
	// Secret is the credential that must not survive. Empty on a row that
	// carries none — those rows pin over-redaction, which is the failure that
	// costs a maintainer the parameter they needed.
	Secret string
	// WantEndpoint is the exact output of cmd/eshu's redactEndpoint.
	WantEndpoint string
	// WantFreeText is the exact output of internal/reportbundle's
	// redactFreeText.
	WantFreeText string
	// EndpointKeepsSecret records why redactEndpoint provably cannot remove
	// Secret on this row. Empty means it must remove it. A test asserts both
	// directions, so an exemption that stops being true fails rather than
	// quietly widening what the corpus permits.
	EndpointKeepsSecret string
	// FreeTextKeepsSecret is the same record for redactFreeText.
	FreeTextKeepsSecret string
}

// CheckEndpointSecret reports whether cmd/eshu's redactEndpoint output honours
// this row's leak invariant. It returns nil when it does.
func (c BoundaryCase) CheckEndpointSecret(got string) error {
	return c.checkSecret("the endpoint walk", c.EndpointKeepsSecret, got)
}

// CheckFreeTextSecret is the same check for internal/reportbundle's
// redactFreeText output.
func (c BoundaryCase) CheckFreeTextSecret(got string) error {
	return c.checkSecret("the free-text walk", c.FreeTextKeepsSecret, got)
}

// checkSecret is the single implementation both walks are held to, so the two
// test packages cannot drift on what the invariant is.
//
// It checks BOTH directions. An empty keepsReason means the walk must remove
// the credential. A non-empty one means the walk provably cannot, so the
// credential must still be present — a walk that started removing it makes the
// recorded exemption a lie, and that has to be a red test rather than a corpus
// that quietly permits more than it says.
//
// The redacted body is never echoed, so a failure message cannot itself become
// the leak it reports.
func (c BoundaryCase) checkSecret(walk, keepsReason, got string) error {
	if c.Secret == "" {
		return nil
	}
	survived := strings.Contains(got, c.Secret)
	switch {
	case keepsReason == "" && survived:
		return fmt.Errorf("%s leaked the credential verbatim", walk)
	case keepsReason != "" && !survived:
		return fmt.Errorf("%s now removes the credential, so the recorded exemption is stale: %s", walk, keepsReason)
	}
	return nil
}

// BoundaryCases returns the shared corpus. Both cmd/eshu and
// internal/reportbundle drive their own walk through it, so a boundary the two
// disagree on cannot be reintroduced without a row here changing.
//
// It is a function rather than a package-level slice so no caller can mutate
// the corpus another test then reads.
func BoundaryCases() []BoundaryCase {
	return []BoundaryCase{
		{
			Name:         "semicolon separates pairs",
			Input:        "http://127.0.0.1:8080/x?a=1;token=" + Sentinel,
			Secret:       Sentinel,
			WantEndpoint: "http://127.0.0.1:8080/x?a=1;token=redacted",
			WantFreeText: "http://127.0.0.1:8080/x?a=1;[redacted]",
		},
		{
			Name:         "nested question mark opens a second query",
			Input:        "http://127.0.0.1:8080/x?next=/v0/y?api_key=" + Sentinel,
			Secret:       Sentinel,
			WantEndpoint: "http://127.0.0.1:8080/x?next=/v0/y?api_key=redacted",
			WantFreeText: "http://127.0.0.1:8080/x?next=/v0/y?[redacted]",
		},
		{
			// The OAuth implicit-grant callback. The credential never touches
			// the query string at all.
			Name:         "fragment only",
			Input:        "https://app.example.com/cb#access_token=" + Sentinel,
			Secret:       Sentinel,
			WantEndpoint: "https://app.example.com/cb",
			WantFreeText: "https://app.example.com/cb#[redacted]",
		},
		{
			Name:         "query and fragment together",
			Input:        "http://h/x?repo=demo#api_key=" + Sentinel,
			Secret:       Sentinel,
			WantEndpoint: "http://h/x?repo=demo",
			WantFreeText: "http://h/x?repo=demo#[redacted]",
		},
		{
			Name:         "repeated sensitive key",
			Input:        "http://h/x?token=" + Sentinel + "&token=" + Sentinel,
			Secret:       Sentinel,
			WantEndpoint: "http://h/x?token=redacted&token=redacted",
			WantFreeText: "http://h/x?[redacted]&[redacted]",
		},
		{
			// No "=", so there is no value. Writing "token=redacted" here would
			// report a value the URL never carried, which is the opposite of
			// what hand-rolling the split was for.
			Name:         "sensitive key with no equals sign",
			Input:        "http://h/x?token&repo=demo",
			WantEndpoint: "http://h/x?token&repo=demo",
			WantFreeText: "http://h/x?token&repo=demo",
		},
		{
			// Same reasoning, and here the two walks genuinely diverge: the
			// endpoint walk leaves the empty pair alone, the free-text walk
			// removes the whole "token=" span because its value simply ends at
			// the next terminator. Neither invents a value; nothing leaks
			// either way.
			Name:         "sensitive key with an empty value",
			Input:        "http://h/x?token=&repo=demo",
			WantEndpoint: "http://h/x?token=&repo=demo",
			WantFreeText: "http://h/x?[redacted]&repo=demo",
		},
		{
			Name:         "sensitive key last with no trailing separator",
			Input:        "http://h/x?repo=demo&api_key=" + Sentinel,
			Secret:       Sentinel,
			WantEndpoint: "http://h/x?repo=demo&api_key=redacted",
			WantFreeText: "http://h/x?repo=demo&[redacted]",
		},
		{
			// A credential in LAST position hides which separator a walk
			// actually knows, because the value runs to the end either way.
			// This row and the next put text after the credential, so the
			// separator has to end the value for the row to pass. Every other
			// row in this corpus put the credential last, which is how a
			// terminator set that had drifted still looked correct.
			Name:         "semicolon ends a value with text after it",
			Input:        "http://h/x?token=" + Sentinel + ";repo=demo",
			Secret:       Sentinel,
			WantEndpoint: "http://h/x?token=redacted;repo=demo",
			WantFreeText: "http://h/x?[redacted];repo=demo",
		},
		{
			Name:         "nested question mark ends a value with text after it",
			Input:        "http://h/x?api_key=" + Sentinel + "?next=/v0/y",
			Secret:       Sentinel,
			WantEndpoint: "http://h/x?api_key=redacted?next=/v0/y",
			WantFreeText: "http://h/x?[redacted]?next=/v0/y",
		},
		{
			// %5F is "_". The endpoint walk decodes the name before asking the
			// predicate; the free-text walk cannot, because walking back from
			// "=" over key runes stops at "%" and yields "5Fkey".
			Name:                "percent-encoded key name",
			Input:               "http://h/x?api%5Fkey=" + Sentinel,
			Secret:              Sentinel,
			WantEndpoint:        "http://h/x?api%5Fkey=redacted",
			WantFreeText:        "http://h/x?api%5Fkey=" + Sentinel,
			FreeTextKeepsSecret: "walking back from \"=\" over key runes stops at \"%\", so the name reads as \"5Fkey\" and no sensitive-key rule matches it",
		},
		{
			Name:         "key differing only in case",
			Input:        "http://h/x?API_KEY=" + Sentinel,
			Secret:       Sentinel,
			WantEndpoint: "http://h/x?API_KEY=redacted",
			WantFreeText: "http://h/x?[redacted]",
		},
		{
			// The free-text walk asks about the token left of a separator. In
			// userinfo that token is the username, which no sensitive-key rule
			// matches, so only the endpoint walk closes this one.
			Name:                "credential in userinfo",
			Input:               "http://u:" + Sentinel + "@h/x",
			Secret:              Sentinel,
			WantEndpoint:        "http://redacted@h/x",
			WantFreeText:        "http://u:" + Sentinel + "@h/x",
			FreeTextKeepsSecret: "the token left of the \":\" is the username, so no sensitive-key rule matches and URL userinfo is invisible to a free-text scan",
		},
		{
			Name:                "userinfo and query on the same URL",
			Input:               "http://u:" + Sentinel + "@127.0.0.1:8080/x?access_token=" + Sentinel,
			Secret:              Sentinel,
			WantEndpoint:        "http://redacted@127.0.0.1:8080/x?access_token=redacted",
			WantFreeText:        "http://u:" + Sentinel + "@127.0.0.1:8080/x?[redacted]",
			FreeTextKeepsSecret: "the query half is removed, but the userinfo copy stays for the reason the row above records",
		},
		{
			Name:         "benign query is untouched",
			Input:        "http://127.0.0.1:8080/x?repo=demo&page=2",
			WantEndpoint: "http://127.0.0.1:8080/x?repo=demo&page=2",
			WantFreeText: "http://127.0.0.1:8080/x?repo=demo&page=2",
		},
		{
			Name:         "no query at all",
			Input:        "http://127.0.0.1:8080/api/v0",
			WantEndpoint: "http://127.0.0.1:8080/api/v0",
			WantFreeText: "http://127.0.0.1:8080/api/v0",
		},
	}
}
