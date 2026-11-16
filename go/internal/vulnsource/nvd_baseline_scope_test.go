// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"testing"
	"time"
)

// TestNVDBaselineScopeIDMintsDeterministically is the R2 determinism cell:
// the same (run, year, start) mints the same scope id on every run, so a
// refusal, skip, or crash re-plans the same chunk scope and the commit path
// fences it as a same-scope successor instead of superseding it.
func TestNVDBaselineScopeIDMintsDeterministically(t *testing.T) {
	t.Parallel()

	start := time.Date(2002, 1, 1, 0, 0, 0, 0, time.UTC)
	first := NVDBaselineScopeID("baseline-2026-11", 2002, start)
	second := NVDBaselineScopeID("baseline-2026-11", 2002, start)
	if first == "" {
		t.Fatal("NVDBaselineScopeID = \"\", want a minted scope")
	}
	if first != second {
		t.Fatalf("scope ids differ: %q vs %q, want byte-identical", first, second)
	}
	want := "vuln-intel://nvd/_baseline/baseline-2026-11/2002/1009843200000000"
	if first != want {
		t.Fatalf("scope id = %q, want %q", first, want)
	}
}

// TestNVDBaselineScopeIDRefusesDegenerateInput pins the fail-closed mint:
// a blank or path-like run id, a pre-feed year, or a zero start mints
// nothing, so the coordinator can never plan a degenerate baseline chunk.
func TestNVDBaselineScopeIDRefusesDegenerateInput(t *testing.T) {
	t.Parallel()

	start := time.Date(2002, 6, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		runID string
		year  int
		start time.Time
	}{
		{name: "blank run", runID: "", year: 2002, start: start},
		{name: "run with slash", runID: "a/b", year: 2002, start: start},
		{name: "run with space", runID: "a b", year: 2002, start: start},
		{name: "pre-feed year", runID: "run-1", year: 2001, start: start},
		{name: "zero year", runID: "run-1", year: 0, start: start},
		{name: "zero start", runID: "run-1", year: 2002, start: time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NVDBaselineScopeID(tc.runID, tc.year, tc.start); got != "" {
				t.Fatalf("NVDBaselineScopeID(%q, %d, %v) = %q, want blank", tc.runID, tc.year, tc.start, got)
			}
		})
	}
}

// TestParseNVDBaselineScopeRoundTrips proves the parser recovers the exact
// minted (run, year, start), normalized to the NVD millisecond wire form.
func TestParseNVDBaselineScopeRoundTrips(t *testing.T) {
	t.Parallel()

	start := time.Date(2015, 3, 14, 9, 26, 53, 589000000, time.UTC)
	id := NVDBaselineScopeID("run-7", 2015, start)
	runID, year, parsed, ok := ParseNVDBaselineScope(id)
	if !ok {
		t.Fatalf("ParseNVDBaselineScope(%q) = not ok, want ok", id)
	}
	if runID != "run-7" || year != 2015 {
		t.Fatalf("parsed (run, year) = (%q, %d), want (run-7, 2015)", runID, year)
	}
	if !parsed.Equal(NormalizeNVDWindowTime(start)) {
		t.Fatalf("parsed start = %v, want %v", parsed, NormalizeNVDWindowTime(start))
	}
}

// TestParseNVDBaselineScopeRejectsForeignFamilies is the cursor-safety
// cell: window scopes, static NVD scopes, and GHSA scopes never parse as
// baseline, so the baseline commit policy can never mistake them and the
// window cursor CAS never sees a baseline candidate through this parser.
func TestParseNVDBaselineScopeRejectsForeignFamilies(t *testing.T) {
	t.Parallel()

	foreign := []string{
		"vuln-intel://nvd/_window/1767225600000000-1767398400000000",
		"vuln-intel://nvd/modified",
		"vuln-intel://nvd/cve/CVE-2024-1234",
		"vuln-intel://ghsa/reviewed/_window/1767225600000000-1767398400000000",
		"vuln-intel://nvd/_baseline/run-1/2002",
		"vuln-intel://nvd/_baseline/run-1/2002/not-a-number",
		"vuln-intel://nvd/_baseline/run-1/1999/1009843200000000",
		"vuln-intel://nvd/_baseline//2002/1009843200000000",
		"vuln-intel://nvd/_baseline/run-1/2002/0",
		"vuln-intel://nvd/_baseline/run-1/2002/1009843200000000?x=1",
		"",
	}
	for _, id := range foreign {
		if _, _, _, ok := ParseNVDBaselineScope(id); ok {
			t.Fatalf("ParseNVDBaselineScope(%q) = ok, want reject", id)
		}
		if IsNVDBaselineScope(id) {
			t.Fatalf("IsNVDBaselineScope(%q) = true, want false", id)
		}
	}
}

// TestIsNVDBaselineScopeAcceptsMintedScopes closes the loop: every minted
// scope passes the cheap prefix gate the commit path uses.
func TestIsNVDBaselineScopeAcceptsMintedScopes(t *testing.T) {
	t.Parallel()

	id := NVDBaselineScopeID("run-1", 2026, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if !IsNVDBaselineScope(id) {
		t.Fatalf("IsNVDBaselineScope(%q) = false, want true", id)
	}
	if IsNVDWindowScope(id) {
		t.Fatalf("IsNVDWindowScope(%q) = true for a baseline scope, want false", id)
	}
}
