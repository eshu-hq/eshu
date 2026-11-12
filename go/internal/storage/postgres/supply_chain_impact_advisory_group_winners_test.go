// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestAdvisoryGroupWinnersQueryShape pins the qualifying-winners read to its
// contract: group match on winners columns only, the qualifying impact set,
// expiry-aware effective suppression over the shared hidden set, the static
// scan exclusions, and the winner-fact payload join. Guards are derived from
// the shipped constant and the shared reducer vocabulary, never a hand-copied
// whole query.
func TestAdvisoryGroupWinnersQueryShape(t *testing.T) {
	t.Parallel()

	query := advisoryGroupWinnersQuery

	if !strings.Contains(query, "FROM supply_chain_impact_canonical_winners") {
		t.Fatalf("winners query must read FROM supply_chain_impact_canonical_winners:\n%s", query)
	}
	if !strings.Contains(query, "btrim(w.cve_id)") || !strings.Contains(query, "ELSE w.advisory_id END) = $1") {
		t.Fatalf("winners query must match the group on (CASE WHEN btrim(w.cve_id) <> '' THEN w.cve_id ELSE w.advisory_id END) = $1:\n%s", query)
	}

	qualifying := []supplychaincore.SupplyChainImpactStatus{
		supplychaincore.SupplyChainImpactAffectedExact,
		supplychaincore.SupplyChainImpactAffectedDerived,
		supplychaincore.SupplyChainImpactPossiblyAffected,
	}
	for _, status := range qualifying {
		if !strings.Contains(query, "'"+string(status)+"'") {
			t.Fatalf("winners query must qualify impact_status %q:\n%s", status, query)
		}
	}
	for _, excluded := range []supplychaincore.SupplyChainImpactStatus{
		supplychaincore.SupplyChainImpactNotAffectedKnownFixed,
		supplychaincore.SupplyChainImpactUnknown,
	} {
		if strings.Contains(query, "'"+string(excluded)+"'") {
			t.Fatalf("winners query must not qualify impact_status %q:\n%s", excluded, query)
		}
	}

	var hidden []string
	for _, state := range supplychaincore.SupplyChainSuppressionHiddenStates() {
		hidden = append(hidden, "'"+string(state)+"'")
	}
	for _, state := range hidden {
		if !strings.Contains(query, state) {
			t.Fatalf("winners query must exclude hidden suppression state %s:\n%s", state, query)
		}
	}
	for _, visible := range []string{"'expired'", "'provider_dismissed'", "'scope_mismatch'"} {
		if strings.Contains(query, "NOT IN ("+visible) || strings.Contains(query, ","+visible) {
			t.Fatalf("winners query must keep visible suppression state %s out of the hidden set:\n%s", visible, query)
		}
	}
	if strings.Contains(query, "suppression_state = 'active'") || strings.Contains(query, "suppression_state='active'") {
		t.Fatalf("winners query must not filter on a raw suppression_state='active' equality; expiry-aware effective state decides:\n%s", query)
	}
	if !strings.Contains(query, "suppression_expires_at") || !strings.Contains(query, "NOW()") {
		t.Fatalf("winners query must evaluate suppression expiry at read time:\n%s", query)
	}

	if !strings.Contains(query, "w.repository_id <> ''") {
		t.Fatalf("winners query must drop blank repository rows:\n%s", query)
	}
	if !strings.Contains(query, "w.winner_scope_id NOT LIKE 'image://derived/%'") {
		t.Fatalf("winners query must exclude derived image scopes:\n%s", query)
	}
	if !strings.Contains(query, "scanner_image_result_fact_associations") || !strings.Contains(query, "NOT EXISTS") {
		t.Fatalf("winners query must exclude scan-associated winners via NOT EXISTS:\n%s", query)
	}

	if !strings.Contains(query, "LEFT JOIN fact_records") || !strings.Contains(query, "f.fact_id = w.winner_fact_id") {
		t.Fatalf("winners query must join payload only via LEFT JOIN fact_records on winner_fact_id:\n%s", query)
	}
	if !strings.Contains(query, "f.payload #> '{provenance,advisory_group_members}'") || !strings.Contains(query, "->>'purl'") {
		t.Fatalf("winners query must select provenance.advisory_group_members via #> and purl from the winner fact payload:\n%s", query)
	}
	if strings.Contains(query, "f.payload->'advisory_group_members'") {
		t.Fatalf("winners query must not read top-level advisory_group_members; production writes members only under provenance (#10475):\n%s", query)
	}
	if !strings.Contains(query, "ORDER BY") {
		t.Fatalf("winners query must end in a deterministic ORDER BY:\n%s", query)
	}
}
