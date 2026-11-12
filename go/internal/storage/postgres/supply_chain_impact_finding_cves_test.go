// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestListActiveFindingCVEIDsQueryShape pins the executable contract of the
// findings-derived EPSS read (eshu-hq/eshu#9876): distinct non-blank ids from
// the canonical winners whose read-time effective suppression state is
// visible (#9805 reader #10), in a stable order. The coordinator normalizes
// CVE shape and counts non-CVE exclusions, so the store must not filter
// schemes itself. The live proof lives in
// supply_chain_impact_finding_cves_live_test.go.
func TestListActiveFindingCVEIDsQueryShape(t *testing.T) {
	t.Parallel()

	query := listActiveFindingCVEIDsQuery
	for _, want := range []string{
		"SELECT DISTINCT w.cve_id",
		"FROM supply_chain_impact_canonical_winners",
		"w.cve_id <> ''",
		"ORDER BY w.cve_id",
		"suppression_expires_at",
		"NOW()",
		"'expired'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("finding CVE ids query missing %q:\n%s", want, query)
		}
	}
	for _, state := range supplychaincore.SupplyChainSuppressionHiddenStates() {
		if !strings.Contains(query, "'"+string(state)+"'") {
			t.Fatalf("finding CVE ids query must exclude hidden suppression state %q:\n%s", state, query)
		}
	}
	for _, visible := range []string{"'expired'", "'provider_dismissed'", "'scope_mismatch'"} {
		if strings.Contains(query, "NOT IN ("+visible) || strings.Contains(query, ","+visible) {
			t.Fatalf("finding CVE ids query must keep visible suppression state %s out of the hidden set:\n%s", visible, query)
		}
	}
	if strings.Contains(query, "suppression_state = 'active'") || strings.Contains(query, "suppression_state='active'") {
		t.Fatalf("finding CVE ids query must not filter on a raw suppression_state='active' equality; expiry-aware effective state decides:\n%s", query)
	}
}
