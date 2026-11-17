// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"strings"
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
	first := NVDBaselineScopeID("baseline-2026-11", 2002, start, "")
	second := NVDBaselineScopeID("baseline-2026-11", 2002, start, "")
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
			if got := NVDBaselineScopeID(tc.runID, tc.year, tc.start, ""); got != "" {
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
	id := NVDBaselineScopeID("run-7", 2015, start, "")
	runID, year, parsed, fromCVEID, ok := ParseNVDBaselineScope(id)
	if !ok {
		t.Fatalf("ParseNVDBaselineScope(%q) = not ok, want ok", id)
	}
	if runID != "run-7" || year != 2015 {
		t.Fatalf("parsed (run, year) = (%q, %d), want (run-7, 2015)", runID, year)
	}
	if !parsed.Equal(NormalizeNVDWindowTime(start)) {
		t.Fatalf("parsed start = %v, want %v", parsed, NormalizeNVDWindowTime(start))
	}
	if fromCVEID != "" {
		t.Fatalf("parsed from_cve_id = %q, want empty for a three-segment scope", fromCVEID)
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
		if _, _, _, _, ok := ParseNVDBaselineScope(id); ok {
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

	id := NVDBaselineScopeID("run-1", 2026, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "")
	if !IsNVDBaselineScope(id) {
		t.Fatalf("IsNVDBaselineScope(%q) = false, want true", id)
	}
	if IsNVDWindowScope(id) {
		t.Fatalf("IsNVDWindowScope(%q) = true for a baseline scope, want false", id)
	}
}

// TestNVDBaselineScopeIDMintsOneScopePerPosition pins the ruled position
// grammar (eshu-hq/eshu#10809): an empty from_cve_id mints the three-segment
// scope with no trailing segment, a non-empty one mints the four-segment
// scope, and both parse back to the exact position. One position has exactly
// one scope id.
func TestNVDBaselineScopeIDMintsOneScopePerPosition(t *testing.T) {
	t.Parallel()

	start := time.Date(2025, 10, 18, 21, 10, 0, 120000000, time.UTC)
	empty := NVDBaselineScopeID("run-7", 2025, start, "")
	if want := "vuln-intel://nvd/_baseline/run-7/2025/1760821800120000"; empty != want {
		t.Fatalf("empty-id scope = %q, want %q (no trailing segment)", empty, want)
	}
	withID := NVDBaselineScopeID("run-7", 2025, start, "CVE-2025-10000")
	if want := "vuln-intel://nvd/_baseline/run-7/2025/1760821800120000/CVE-2025-10000"; withID != want {
		t.Fatalf("scope = %q, want %q", withID, want)
	}
	runID, year, parsed, fromCVEID, ok := ParseNVDBaselineScope(withID)
	if !ok || runID != "run-7" || year != 2025 || !parsed.Equal(start) || fromCVEID != "CVE-2025-10000" {
		t.Fatalf("round trip = (%q, %d, %v, %q, %v), want the minted position", runID, year, parsed, fromCVEID, ok)
	}
	if !IsNVDBaselineScope(withID) || !IsNVDBaselineScope(empty) {
		t.Fatal("IsNVDBaselineScope rejected a minted position scope")
	}
}

// TestParseNVDBaselineScopeRejectsNonCanonicalPosition pins the canonical
// from_cve_id (eshu-hq/eshu#10809): trimmed, upper-cased, [A-Z0-9-]{1,64},
// never an empty trailing segment, never a fifth segment. A non-canonical id
// must not parse, because two spellings of one position would mint two scope
// ids and split a lane's fence.
func TestParseNVDBaselineScopeRejectsNonCanonicalPosition(t *testing.T) {
	t.Parallel()

	const base = "vuln-intel://nvd/_baseline/run-7/2025/1760821800120000"
	for _, tc := range []struct {
		name string
		id   string
	}{
		{"lowercase", base + "/cve-2025-10000"},
		{"mixed case", base + "/Cve-2025-10000"},
		{"empty trailing segment", base + "/"},
		{"leading space", base + "/%20CVE-2025-10000"},
		{"trailing space", base + "/CVE-2025-10000%20"},
		{"underscore", base + "/CVE_2025_10000"},
		{"dot", base + "/CVE-2025.10000"},
		{"over 64 characters", base + "/CVE-" + strings.Repeat("1", 61)},
		{"fifth segment", base + "/CVE-2025-10000/extra"},
		{"query", base + "/CVE-2025-10000?x=1"},
		{"fragment", base + "/CVE-2025-10000#x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, _, _, _, ok := ParseNVDBaselineScope(tc.id); ok {
				t.Fatalf("ParseNVDBaselineScope(%q) = ok, want reject", tc.id)
			}
		})
	}
	if _, _, _, _, ok := ParseNVDBaselineScope(base + "/CVE-" + strings.Repeat("1", 60)); !ok {
		t.Fatal("a 64-character canonical id was rejected, want accept at the bound")
	}
}

// TestNVDBaselineScopeIDRefusesNonCanonicalFromCVEID proves the mint is as
// strict as the parse: a lowercase or blank-padded id mints nothing instead
// of a scope the parser would then reject.
func TestNVDBaselineScopeIDRefusesNonCanonicalFromCVEID(t *testing.T) {
	t.Parallel()

	start := time.Date(2025, 10, 18, 21, 10, 0, 120000000, time.UTC)
	for _, id := range []string{"cve-2025-1", " CVE-2025-1", "CVE-2025-1 ", "CVE/2025", "CVE-2025-1\n"} {
		if got := NVDBaselineScopeID("run-7", 2025, start, id); got != "" {
			t.Fatalf("NVDBaselineScopeID(from=%q) = %q, want blank", id, got)
		}
	}
}

// TestCanonicalNVDBaselineCVEID pins the shared canonical-id predicate the
// commit policy and the checkpoint writer both use.
func TestCanonicalNVDBaselineCVEID(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"CVE-2025-10000", "CVE-2025-9999", "A", strings.Repeat("A", 64)} {
		if !IsCanonicalNVDBaselineCVEID(id) {
			t.Fatalf("IsCanonicalNVDBaselineCVEID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"", "cve-2025-1", " CVE-2025-1", "CVE-2025-1\t", strings.Repeat("A", 65), "CVE 2025"} {
		if IsCanonicalNVDBaselineCVEID(id) {
			t.Fatalf("IsCanonicalNVDBaselineCVEID(%q) = true, want false", id)
		}
	}
}
