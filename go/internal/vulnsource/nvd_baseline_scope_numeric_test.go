// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import "testing"

// TestParseNVDBaselineScopeRejectsNonCanonicalNumerics pins that the year and
// the start stamp parse only in the one spelling the mint writes (eshu-hq/eshu#10809):
// a signed, zero-padded, or space-padded number, a start that is not a whole
// UTC millisecond, and an upper-case scheme or host are rejected, so one
// position never has two scope ids and the commit policy and the scope
// predicate agree.
func TestParseNVDBaselineScopeRejectsNonCanonicalNumerics(t *testing.T) {
	t.Parallel()

	const good = "vuln-intel://nvd/_baseline/run-7/2025/1760821800120000"
	if _, _, _, _, ok := ParseNVDBaselineScope(good); !ok {
		t.Fatalf("ParseNVDBaselineScope(%q) rejected the canonical form", good)
	}
	for _, tc := range []struct {
		name string
		id   string
	}{
		{"plus-signed year", "vuln-intel://nvd/_baseline/run-7/+2025/1760821800120000"},
		{"zero-padded year", "vuln-intel://nvd/_baseline/run-7/02025/1760821800120000"},
		{"space-padded year", "vuln-intel://nvd/_baseline/run-7/%202025/1760821800120000"},
		{"five-digit year", "vuln-intel://nvd/_baseline/run-7/20250/1760821800120000"},
		{"plus-signed start", "vuln-intel://nvd/_baseline/run-7/2025/+1760821800120000"},
		{"zero-padded start", "vuln-intel://nvd/_baseline/run-7/2025/01760821800120000"},
		{"space-padded start", "vuln-intel://nvd/_baseline/run-7/2025/%201760821800120000"},
		{"sub-millisecond start", "vuln-intel://nvd/_baseline/run-7/2025/1760821800120001"},
		{"upper-case scheme", "VULN-INTEL://nvd/_baseline/run-7/2025/1760821800120000"},
		{"upper-case host", "vuln-intel://NVD/_baseline/run-7/2025/1760821800120000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, _, _, _, ok := ParseNVDBaselineScope(tc.id); ok {
				t.Fatalf("ParseNVDBaselineScope(%q) = ok, want reject", tc.id)
			}
			if IsNVDBaselineScope(tc.id) {
				t.Fatalf("IsNVDBaselineScope(%q) = true, want false: the predicate and the parser must agree", tc.id)
			}
		})
	}
}
