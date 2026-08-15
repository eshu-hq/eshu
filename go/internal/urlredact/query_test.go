// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package urlredact

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestQueryPreservesTheSeparatorItRead is the property the whole package exists
// for. Split-then-Join on one chosen separator passes an equality test built
// from "&"-only fixtures and silently rewrites every other endpoint, which is
// how the two walks drifted in the first place.
func TestQueryPreservesTheSeparatorItRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "ampersand", raw: "a=1&token=" + Sentinel, want: "a=1&token=redacted"},
		{name: "semicolon", raw: "a=1;token=" + Sentinel, want: "a=1;token=redacted"},
		{name: "nested question mark", raw: "next=/v0/y?api_key=" + Sentinel, want: "next=/v0/y?api_key=redacted"},
		{name: "mixed separators", raw: "a=1;b=2&token=" + Sentinel + "?c=3", want: "a=1;b=2&token=redacted?c=3"},
		{name: "trailing separator", raw: "token=" + Sentinel + "&", want: "token=redacted&"},
		{name: "leading separator", raw: "&token=" + Sentinel, want: "&token=redacted"},
		{name: "empty pair between separators", raw: "a=1&&token=" + Sentinel, want: "a=1&&token=redacted"},
		{name: "percent-encoded name is decoded before matching", raw: "api%5Fkey=" + Sentinel, want: "api%5Fkey=redacted"},
		{name: "undecodable name is asked about as it arrived", raw: "api%ZZkey=" + Sentinel, want: "api%ZZkey=" + Sentinel},
		{name: "percent-encoded equals sign", raw: "token%3D" + Sentinel, want: "token%3Dredacted"},
		{name: "percent-encoded ampersand splits the pairs", raw: "a=1%26token%3D" + Sentinel + "%26b=2", want: "a=1%26token%3Dredacted%26b=2"},
		{name: "percent-encoded question mark opens a nested query", raw: "next=/v0/y%3Fapi_key%3D" + Sentinel, want: "next=/v0/y%3Fapi_key%3Dredacted"},
		{name: "lowercase hex in the escapes", raw: "a=1%26token%3d" + Sentinel, want: "a=1%26token%3dredacted"},
		// The depth rule. A pair joined by a literal "=" was typed at the
		// surface, so an escaped separator inside its value is part of the
		// value and the whole tail goes. Reading it as a boundary shipped
		// everything after the escape.
		{name: "escaped ampersand inside a literal pair is value content", raw: "token=AAAA%26" + Sentinel, want: "token=redacted"},
		{name: "escaped semicolon inside a literal pair is value content", raw: "password=AAAA%3B" + Sentinel, want: "password=redacted"},
		{name: "escaped question mark inside a literal pair is value content", raw: "token=AAAA%3F" + Sentinel, want: "token=redacted"},
		// The same depth rule at the one position none of the rows above reach:
		// the escape is the FIRST byte of the value, so the text pending at it
		// is exactly "token=". Asking "does this pair carry a value?" there
		// answered no, the escape was honoured as a separator, and the whole
		// credential came back untouched.
		{name: "escaped ampersand opening a literal value is value content", raw: "token=%26" + Sentinel, want: "token=redacted"},
		{name: "escaped semicolon opening a literal value is value content", raw: "password=%3B" + Sentinel, want: "password=redacted"},
		{name: "escaped question mark opening a literal value is value content", raw: "api_key=%3F" + Sentinel, want: "api_key=redacted"},
		{name: "escaped separator opening a literal value away from the query start", raw: "x=1&token=%26" + Sentinel, want: "x=1&token=redacted"},
		{name: "escaped ampersand closing a literal value", raw: "token=" + Sentinel + "%26&repo=demo", want: "token=redacted&repo=demo"},
		{name: "a benign pair keeps an escape that opens its value", raw: "q=%26bb&repo=demo", want: "q=%26bb&repo=demo"},
		{name: "a literal separator still ends a literal pair", raw: "token=AAAA%26" + Sentinel + "&repo=demo", want: "token=redacted&repo=demo"},
		{name: "a benign literal pair keeps its escaped content", raw: "q=aa%26bb&repo=demo", want: "q=aa%26bb&repo=demo"},
		{name: "an escaped separator still ends an escaped pair", raw: "a=1%26token%3D" + Sentinel + "%26repo%3Ddemo", want: "a=1%26token%3Dredacted%26repo%3Ddemo"},
		{name: "encoded separator with no value after it", raw: "token%3D", want: "token%3D"},
		{name: "double-encoded stays one layer out of reach", raw: "next=%253Ftoken%253D" + Sentinel, want: "next=%253Ftoken%253D" + Sentinel},
		{name: "a percent that starts no escape is literal text", raw: "a=100%&token=" + Sentinel, want: "a=100%&token=redacted"},
		{name: "no value to remove", raw: "token", want: "token"},
		{name: "empty value", raw: "token=", want: "token="},
		{name: "benign parameters survive", raw: "repo=demo&page=2", want: "repo=demo&page=2"},
		{name: "empty query", raw: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Query(tt.raw, "redacted")
			if got != tt.want {
				t.Fatalf("Query(%q)\n got %q\nwant %q", tt.raw, got, tt.want)
			}
			if again := Query(got, "redacted"); again != got {
				t.Fatalf("Query is not idempotent: second pass on %q gave %q", got, again)
			}
		})
	}
}

// TestQueryUsesTheCallerMarker pins that the marker is the caller's. The two
// consumers write different text — cmd/eshu keeps the parameter name and writes
// a bare "redacted" as its value, reportbundle removes the name as well — so a
// marker baked in here would force one of them to change what its artifact says.
func TestQueryUsesTheCallerMarker(t *testing.T) {
	t.Parallel()

	got := Query("token="+Sentinel, "XX")
	if got != "token=XX" {
		t.Fatalf("Query with marker %q = %q, want %q", "XX", got, "token=XX")
	}
}

// TestBoundaryCasesAreSelfConsistent checks the corpus itself, not a walk. A row
// whose expected output still holds the credential it declares, without a
// recorded reason, would let both consumer tests pass while asserting a leak.
func TestBoundaryCasesAreSelfConsistent(t *testing.T) {
	t.Parallel()

	cases := BoundaryCases()
	if len(cases) == 0 {
		t.Fatal("the corpus is empty, so both consumer tests would pass vacuously")
	}

	seen := make(map[string]struct{}, len(cases))
	for _, tc := range cases {
		if _, dup := seen[tc.Name]; dup {
			t.Errorf("duplicate row name %q", tc.Name)
		}
		seen[tc.Name] = struct{}{}

		if tc.Secret == "" {
			if tc.EndpointKeepsSecret != "" || tc.FreeTextKeepsSecret != "" {
				t.Errorf("row %q records a keeps-secret reason but carries no secret", tc.Name)
			}
			continue
		}
		if !strings.Contains(tc.Input, tc.Secret) {
			t.Errorf("row %q declares a secret that is not in its input", tc.Name)
		}
		if err := tc.CheckEndpointSecret(tc.WantEndpoint); err != nil {
			t.Errorf("row %q: %v", tc.Name, err)
		}
		if err := tc.CheckFreeTextSecret(tc.WantFreeText); err != nil {
			t.Errorf("row %q: %v", tc.Name, err)
		}
	}
}

// TestBoundaryCasesCoverEverySeparator keeps the corpus honest about the axis it
// claims to pin. The fixture this replaced named the query rule and then used a
// single "&" in all five of its cases, so the axis was never varied. This fails
// if a separator stops being represented.
func TestBoundaryCasesCoverEverySeparator(t *testing.T) {
	t.Parallel()

	for _, sep := range PairSeparators {
		found := false
		for _, tc := range BoundaryCases() {
			// The leading "?" of a plain URL query does not count as a pair
			// boundary; look past it.
			query := tc.Input
			if _, rest, ok := strings.Cut(query, "?"); ok {
				query = rest
			}
			if strings.ContainsRune(query, sep) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no corpus row exercises the %q separator", string(sep))
		}
	}
}

// TestBoundaryCasesCoverTheEncodedSpelling is the same guard on the axis that
// broke next. Every separator had a row, and every row spelled it literally, so
// "?a=1;token=…" was covered and "?a=1%3Btoken%3D…" — the same URL as an HTTP
// client writes it — was not, in a corpus whose whole job is boundaries.
//
// It asks for the encoded spelling of each separator AND of the "=" that joins a
// pair, because the "=" is the one a nested URL always hides.
func TestBoundaryCasesCoverTheEncodedSpelling(t *testing.T) {
	t.Parallel()

	for _, want := range append([]rune(PairSeparators), '=') {
		escape := fmt.Sprintf("%%%02X", want)
		found := false
		for _, tc := range BoundaryCases() {
			if tc.ProvesRemoval() && strings.Contains(strings.ToUpper(tc.Input), escape) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no corpus row writes %q as %s", string(want), escape)
		}
	}
}

// TestBoundaryCasesOpenAValueWithAnEscape pins the axis every encoded row
// missed. All of them put at least one value byte in front of the escape, so
// all of them landed in the same cell — the pair already carries a value — and
// the cell where the escape is the value's FIRST byte had no row on any
// separator. That cell is where the endpoint walk asked "does this pair carry a
// value?", got "no" for the pending text "token=", honoured the escape as a
// separator, and returned the credential verbatim.
func TestBoundaryCasesOpenAValueWithAnEscape(t *testing.T) {
	t.Parallel()

	for _, sep := range PairSeparators {
		// A literal "=" with the escape straight after it: the pair was written
		// at the surface depth and the escape opens its value.
		opening := fmt.Sprintf("=%%%02X", sep)
		found := false
		for _, tc := range BoundaryCases() {
			if tc.ProvesRemoval() && strings.Contains(strings.ToUpper(tc.Input), opening) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no corpus row opens a literal value with %q, the position that shipped a whole credential", opening)
		}
	}
}

// escapePattern matches exactly one percent-escape and captures its two hex
// digits. Anchoring it to two digits is the whole point: an unanchored
// `%[0-9a-fA-F]*[a-f][0-9a-fA-F]*` runs on past the escape into ordinary text,
// so an uppercase "%2Fcb" satisfied a lowercase-hex guard through the "cb" that
// follows it and the guard could not fail.
var escapePattern = regexp.MustCompile(`%([0-9a-fA-F]{2})`)

// TestBoundaryCasesVaryTheHexCase pins the other half of the encoding axis. RFC
// 3986 allows either case in an escape and real clients emit both, so a hex
// reader written with only "0-9A-F" would pass every row above.
func TestBoundaryCasesVaryTheHexCase(t *testing.T) {
	t.Parallel()

	for _, tc := range BoundaryCases() {
		if !tc.ProvesRemoval() {
			continue
		}
		for _, escape := range escapePattern.FindAllStringSubmatch(tc.Input, -1) {
			if strings.ContainsAny(escape[1], "abcdef") {
				return
			}
		}
	}
	t.Error("no credential-carrying corpus row spells an escape with lowercase hex")
}
