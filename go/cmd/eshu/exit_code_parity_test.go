// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/freshness"
)

// TestExitCodeTableParity holds both copies of the error-code-to-exit-code
// table to the same answers: traceExitCode in this package and
// freshness.ExitCodeForErrorCode.
//
// The two bodies are identical today and nothing was keeping them that way.
// cmd/eshu is package main, so the freshness family could not import the
// original and took a copy, the same trade the transport classifier and the
// envelope readers next door made. What the exit-code table lacked is the pin:
// each copy had its own test with its own hard-coded numbers -- the freshness
// package's TestExitCodeForErrorCodeTable, and the traceExitCode assertions in
// change_impact_test.go -- so an edit to one table left the other answering the
// old numbers with every test in the tree still green.
//
// That silence is worse here than for the other two copies. An exit code is
// what an operator's script branches on, so the drift would first show up as a
// pipeline taking the wrong branch on a machine nobody is watching, not as a
// failure anyone could trace back to the edit.
//
// This test compares behaviour rather than source text, which is where it
// differs from TestEnvelopeReaderParity. Both functions are reachable from one
// test package: traceExitCode is declared here, and ExitCodeForErrorCode is
// exported. The readers are unexported in all four of their packages, which is
// the only reason that test compares declarations instead of calling them.
//
// A failure here means one copy no longer answers what the other does. That is
// either an edit that reached one copy and missed the other -- fix the one it
// missed -- or a deliberate split, in which case the comment above
// ExitCodeForErrorCode stops being true and needs rewriting along with this
// test.
func TestExitCodeTableParity(t *testing.T) {
	t.Parallel()

	// Every copy of the table belongs here. A copy left out of this slice is a
	// copy nothing pins, which is the hole this test exists to close, so the
	// count is asserted below.
	subjects := []struct {
		name string
		fn   func(string) int
	}{
		{name: "traceExitCode", fn: traceExitCode},
		{name: "freshness.ExitCodeForErrorCode", fn: freshness.ExitCodeForErrorCode},
	}

	cases := []struct {
		code string
		want int
	}{
		{code: "ambiguous", want: 3},
		{code: "index_building", want: 4},
		{code: "stale", want: 4},
		{code: "capability_degraded", want: 5},
		{code: "partial", want: 5},
		{code: "unsupported_capability", want: 6},
		{code: "invalid_argument", want: 2},
		{code: "not_found", want: 2},
		{code: "scope_not_found", want: 2},
		{code: "backend_unavailable", want: 1},
		{code: "api_error", want: 1},
		{code: "weird_code", want: 1},
		{code: "", want: 1},
		// TrimSpace runs before the switch, so a padded code still matches.
		{code: "  index_building  ", want: 4},
		// "building" and "truncated" are freshness *states*, never error codes,
		// and both copies must keep falling through to 1 for them. The change
		// family's exit codes turn on that gap: see change_impact.go's comment
		// and freshness's TestExitCodeForErrorCodeRejectsBuildingSpelling.
		{code: "building", want: 1},
		{code: "truncated", want: 1},
	}

	// Every code the switch names needs a row, and every number it can return
	// needs one too. Without both checks a divergence in an arm no row visits
	// would pass unnoticed, which is the failure this test exists to catch.
	covered := make(map[string]bool, len(cases))
	answered := make(map[int]bool, len(cases))
	for _, tc := range cases {
		covered[tc.code] = true
		answered[tc.want] = true
	}
	for _, code := range []string{
		"ambiguous", "index_building", "stale", "capability_degraded", "partial",
		"unsupported_capability", "invalid_argument", "not_found", "scope_not_found",
	} {
		if !covered[code] {
			t.Fatalf("no row for %q; the table no longer names every code the switch matches", code)
		}
	}
	for exit := 1; exit <= 6; exit++ {
		if !answered[exit] {
			t.Fatalf("no row expects exit %d; the table no longer reaches every arm", exit)
		}
	}
	if len(cases) != 16 {
		t.Fatalf("parity table has %d rows, want 16; move this number only alongside a row you added, and say which behavior the new row pins", len(cases))
	}
	if len(subjects) != 2 {
		t.Fatalf("parity covers %d copies, want 2; a copy dropped from this slice is a copy nothing pins, so move this number only alongside the copy you added or deleted", len(subjects))
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.code), func(t *testing.T) {
			t.Parallel()

			all := make([]string, 0, len(subjects))
			disagreed := make([]string, 0, len(subjects))
			for _, subject := range subjects {
				answer := subject.fn(tc.code)
				all = append(all, fmt.Sprintf("%s=%d", subject.name, answer))
				if answer != tc.want {
					disagreed = append(disagreed, fmt.Sprintf("%s=%d", subject.name, answer))
				}
			}
			// Naming the copies that disagreed separates the two failures that
			// land here. One of them means the copies have drifted apart and the
			// named one is what an edit missed. Both of them means the copies
			// still agree and the row above is what no longer matches them.
			if len(disagreed) > 0 {
				t.Fatalf("code %q: want exit %d, but %s answered otherwise (every copy: %s)",
					tc.code, tc.want, strings.Join(disagreed, ", "), strings.Join(all, ", "))
			}
		})
	}
}
