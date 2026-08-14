// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package urlredact

import (
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
