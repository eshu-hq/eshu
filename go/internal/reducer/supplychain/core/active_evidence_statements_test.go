// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"
	"time"
)

// TestActiveEvidenceTallyCountsStatementsPerBranch pins the #9994 signal: a chunked
// branch reports one stat per statement, so the pass log can say how many statements
// it issued, summed over the pass's loads. Rows and seconds keep summing per branch.
func TestActiveEvidenceTallyCountsStatementsPerBranch(t *testing.T) {
	t.Parallel()

	var tally supplyChainImpactActiveEvidenceTally
	load := SupplyChainImpactActiveEvidenceLoadStats{Branches: []SupplyChainImpactActiveEvidenceBranchStat{
		{Branch: "vulnerability_cve_id", Rows: 3, Duration: time.Millisecond},
		{Branch: "vulnerability_cve_id", Rows: 4, Duration: time.Millisecond},
		{Branch: "vulnerability_cve_id", Rows: 0, Duration: time.Millisecond},
		{Branch: "security_alert_cve_id", Rows: 0, Duration: time.Millisecond},
	}}
	tally.add(load)
	tally.add(load)
	signals, durations := map[string]float64{}, map[string]float64{}
	tally.mergeInto(signals, durations)
	for key, want := range map[string]float64{
		"active_evidence_branch_vulnerability_cve_id_statements":  6,
		"active_evidence_branch_vulnerability_cve_id_rows":        14,
		"active_evidence_branch_security_alert_cve_id_statements": 2,
		"active_evidence_branch_security_alert_cve_id_rows":       0,
	} {
		if got, ok := signals[key]; !ok || got != want {
			t.Errorf("signals[%s] = %v (present %v), want %v", key, got, ok, want)
		}
	}
	if got, want := durations["active_evidence_branch_vulnerability_cve_id"], (6 * time.Millisecond).Seconds(); got != want {
		t.Errorf("duration = %v, want %v", got, want)
	}
}
