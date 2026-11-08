// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestScopedEPSSHolderKeepsFindingRowAdvisoryArmOnFollowUp is the #10420
// (follow-up to #10403, drive #9779) regression proof for the
// scope:9695:epss copy: an EPSS holder whose own facts never change must
// converge to the operator's accepted_risk decision, not stay active.
//
// The holder's scope holds only a signal envelope (EPSS), so its own
// envelopes seed no advisory identity. The stamped finding row is the only
// carrier of the deciding advisory (advisory_id == CVE for a native record,
// exactly what ListScopedSupplyChainImpactFindingPackages selects post-#10420).
// The until-stable loop (handler_helpers.go) intersects every follow-up round
// with the scope, so a scope that seeds no advisory arm empties a discovered
// AdvisoryIDs=[CVE] follow-up, the loop ends before fetching the operator
// suppression, and the holder copy stays active instead of accepted_risk.
// That was the pre-#10420 behavior: the old row had no AdvisoryID at all, so
// the arm was always empty for this holder.
func TestScopedEPSSHolderKeepsFindingRowAdvisoryArmOnFollowUp(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2026-9695"
	// Holder condition: only the EPSS signal, like seedHolder in the
	// fan-out convergence harness (no CVE/affected envelopes of its own).
	holderEnvelopes := []facts.Envelope{
		vulnerabilityEPSSFact("fact:9695:epss", cve, "0.5", "0.9"),
	}
	for _, tc := range []struct {
		name      string
		advisory  string
		wantAlive bool
	}{
		// Post-#10420 row shape: the finding row carries its deciding
		// advisory, so the follow-up arm survives the scope intersect.
		{name: "advisory arm kept", advisory: cve, wantAlive: true},
		// Pre-#10420 row shape: the old SQL selected no advisory_id and
		// the old row had no such field, so the arm empties and the
		// until-stable loop ends before the operator suppression.
		{name: "advisory arm empty", advisory: "", wantAlive: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := []ScopedFindingPackage{{
				CVEID:       cve,
				AdvisoryID:  tc.advisory,
				PackageID:   "pkg:npm/fanout-9695@1.0.0",
				PackageName: "fanout-9695",
				Ecosystem:   "npm",
			}}
			stamp := []string{cve}

			scope, ok := seedScopedSupplyChainImpactScope(holderEnvelopes, rows, stamp)
			if !ok {
				t.Fatal("seedScopedSupplyChainImpactScope() = false, want a scope: the finding row seeds packages")
			}
			// Round-1 follow-up discovered through the source-side advisory evidence.
			followUp := restrictSupplyChainImpactFilter(
				SupplyChainImpactFactFilter{AdvisoryIDs: []string{cve}}, scope)
			if tc.wantAlive {
				if len(followUp.AdvisoryIDs) != 1 || followUp.AdvisoryIDs[0] != cve {
					t.Fatalf("follow-up AdvisoryIDs = %v, want [%s]: the advisory arm was emptied, so the loop ends before the operator suppression and the holder copy stays active instead of accepted_risk", followUp.AdvisoryIDs, cve)
				}
				return
			}
			if len(followUp.AdvisoryIDs) != 0 {
				t.Fatalf("follow-up AdvisoryIDs = %v, want []: an empty advisory arm must empty the follow-up, else the seeded violation proves nothing", followUp.AdvisoryIDs)
			}
		})
	}
}
