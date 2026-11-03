// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestListActiveFindingCVEIDsQueryShape pins the executable contract of the
// findings-derived EPSS read (eshu-hq/eshu#9876): distinct non-blank ids from
// the canonical winners, active suppressions only, in a stable order. The
// coordinator normalizes CVE shape and counts non-CVE exclusions, so the
// store must not filter schemes itself. The live proof lives in
// supply_chain_impact_finding_cves_live_test.go.
func TestListActiveFindingCVEIDsQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"SELECT DISTINCT cve_id",
		"FROM supply_chain_impact_canonical_winners",
		"suppression_state = 'active'",
		"cve_id <> ''",
		"ORDER BY cve_id",
	} {
		if !strings.Contains(listActiveFindingCVEIDsQuery, want) {
			t.Fatalf("finding CVE ids query missing %q:\n%s", want, listActiveFindingCVEIDsQuery)
		}
	}
}
