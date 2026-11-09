// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestRekeyCarryOperatorCopyRekeysLive is the criterion-5c proof: after the
// merge the operator scope's own fan-out-reopened pass re-derives its copy
// under the new canonical key, so the winners overlay can join it again, and
// the copy carries the dead-id suppression. The operator seeds its own
// filter from its suppression scope ids, so this passes with the pre-existing
// arms and pins the operator half of the carry.
func TestRekeyCarryOperatorCopyRekeysLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedCarryMergePair()
	c.seedRekeyOperator(rekeyDead)
	c.drain()
	c.rescanWithdrawDeadRecord()
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	c.rescanWithdrawHolderDeadRecord()
	c.rerun(rekeyHolder)
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	t.Logf("post-withdrawal findings: %v", c.findingKeys())
	committer := c.keysByScope(rekeyCommitter)
	operator := c.keysByScope("operator:vulnerability_suppressions")
	if len(committer) != 1 || committer[0] != rekeySurvivor {
		t.Fatalf("committer keys = %v, want the survivor group [%s]", committer, rekeySurvivor)
	}
	if len(operator) != 1 || operator[0] != committer[0] {
		t.Fatalf("operator keys = %v, committer keys = %v, want the operator copy under the new canonical key", operator, committer)
	}
	if got := c.suppressionStates("operator:vulnerability_suppressions")[operator[0]]; got == "" || got == "active" || got == "expired" {
		t.Fatalf("operator copy state = %q, want it hidden by the carried dead-id suppression", got)
	}
}

// TestRekeyCarryExpiredStaysExpiredLive is the criterion-5e proof: an expired
// suppression naming the dead id loads on the re-key pass (it is not active)
// but never hides the merged finding. Pre-arm the finding stays active
// because nothing loads the suppression; post-arm it reads expired.
func TestRekeyCarryExpiredStaysExpiredLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedCarryMergePair()
	c.seedCarrySuppression("suppression-10408-expired", `"advisory_id":"`+rekeyDead+`"`, "2026-10-01T12:00:00Z", "2026-10-15T00:00:00Z", "accepted_risk")
	c.activateCarryOperator()
	c.drain()
	if got := c.suppressionStates(rekeyCommitter)[rekeyDead]; got != "expired" {
		t.Fatalf("pre-merge dead finding state = %q, want expired (the expired suppression loads but never hides)", got)
	}
	c.rescanWithdrawDeadRecord()
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	t.Logf("post-merge findings: %v", c.findingKeys())
	if got := c.keysByScope(rekeyCommitter); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("committer keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
	// Withdraw the holder as well: only the survivor's aliases may still
	// name the dead id, so only the alias arm can load the expired
	// suppression for the re-derivation.
	c.rescanWithdrawHolderDeadRecord()
	c.rerun(rekeyHolder)
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	t.Logf("post-withdrawal findings: %v", c.findingKeys())
	if got := c.keysByScope(rekeyCommitter); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("committer keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
	if got := c.suppressionStates(rekeyCommitter)[rekeySurvivor]; got != "expired" {
		t.Fatalf("merged finding state = %q, want expired (loaded, visible, never resurrected as a hide)", got)
	}
	if got := c.keysByScope(rekeyHolder); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("holder keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
	if got := c.suppressionStates(rekeyHolder)[rekeySurvivor]; got != "expired" {
		t.Fatalf("holder finding state = %q, want expired (loaded, visible, never resurrected as a hide)", got)
	}
}

// The criterion-5f merge-conflict world: two lone groups whose non-key
// members carry conflicting active suppressions merge through a bridge
// alias. Both suppressions name alias-only member ids, so only the alias arm
// loads either of them after the merge.
const (
	carryConflictA1 = "CVE-2026-0311"
	carryConflictA2 = "CVE-2026-0312"
	carryConflictB1 = "CVE-2026-0321"
	carryConflictB2 = "CVE-2026-0322"
	carryConflictSA = "suppression-10408-confa"
	carryConflictSB = "suppression-10408-confb"
)

// seedCarryConflictPair seeds two lone groups {A1,A2} and {B1,B2} where the
// second id of each group is alias-only, with conflicting active suppressions:
// S_a (not_affected, earlier) names A2, S_b (accepted_risk, later) names B2.
func (c *rekeyProof) seedCarryConflictPair() {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	c.upsert(
		c.envelope(rekeyCommitter, "g1", "fact:10408:ca", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": carryConflictA1, "cve_id": carryConflictA1, "source": "ghsa", "cvss_score": 7.5, "aliases": []string{carryConflictA1, carryConflictA2},
		}),
		c.envelope(rekeyCommitter, "g1", "fact:10408:cb", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": carryConflictB1, "cve_id": carryConflictB1, "source": "ghsa", "cvss_score": 5.0, "aliases": []string{carryConflictB1, carryConflictB2},
		}),
		c.affectedPackage(rekeyCommitter, "g1", "fact:10408:cpkg", carryConflictA1, rekeyPackageA),
		c.affectedPackage(rekeyCommitter, "g1", "fact:10408:cdpkg", carryConflictB1, rekeyPackageB),
		c.consumption(rekeyCommitter, "g1", "fact:10408:ccons", rekeyPackageA, "repository:r_10408_c"),
		c.consumption(rekeyCommitter, "g1", "fact:10408:cdcons", rekeyPackageB, "repository:r_10408_c"),
	)
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyCommitter, "g1")
}

// rescanBridgeConflict adds the bridge alias from the A record across to the
// B group, merging both groups under A1.
func (c *rekeyProof) rescanBridgeConflict() {
	c.t.Helper()
	c.copyGeneration(rekeyCommitter, "g1", "g2")
	encoded, err := json.Marshal([]string{carryConflictA1, carryConflictA2, carryConflictB1, carryConflictB2})
	if err != nil {
		c.t.Fatalf("marshal aliases: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
UPDATE fact_records SET payload = jsonb_set(payload, '{aliases}', $3::jsonb)
WHERE scope_id = $1 AND generation_id = $2 AND fact_id = 'fact:10408:ca:g2'`, rekeyCommitter, fanOutGen(rekeyCommitter, "g2"), encoded); err != nil {
		c.t.Fatalf("add bridge alias: %v", err)
	}
}

// TestRekeyCarryConflictingSuppressionsResolveLive is the criterion-5f proof:
// two merged groups carry conflicting active suppressions naming alias-only
// member ids, and the existing precedence (later AuthoredAt wins) decides,
// with the lexicographic tie-break unchanged. Pre-arm both findings stay
// active because neither suppression loads.
func TestRekeyCarryConflictingSuppressionsResolveLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedCarryConflictPair()
	c.seedCarrySuppression(carryConflictSA, `"advisory_id":"`+carryConflictA2+`"`, "2026-10-31T11:00:00Z", "", "not_affected")
	c.seedCarrySuppression(carryConflictSB, `"advisory_id":"`+carryConflictB2+`"`, "2026-10-31T11:30:00Z", "", "accepted_risk")
	c.activateCarryOperator()
	c.drain()
	t.Logf("pre-merge findings: %v", c.findingKeys())
	if got := c.suppressionStates(rekeyCommitter)[carryConflictA1]; got != "not_affected" {
		t.Fatalf("pre-merge A finding state = %q, want not_affected (S_a hides through member A2)", got)
	}
	if got := c.suppressionStates(rekeyCommitter)[carryConflictB1]; got != "accepted_risk" {
		t.Fatalf("pre-merge B finding state = %q, want accepted_risk (S_b hides through member B2)", got)
	}

	c.rescanBridgeConflict()
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	t.Logf("post-merge findings: %v", c.findingKeys())
	if got := c.keysByScope(rekeyCommitter); len(got) != 1 || got[0] != carryConflictA1 {
		t.Fatalf("committer keys = %v, want the merged group [%s]", got, carryConflictA1)
	}
	if got := c.suppressionStates(rekeyCommitter)[carryConflictA1]; got != "accepted_risk" {
		t.Fatalf("merged finding state = %q, want accepted_risk (later-authored S_b wins the conflict)", got)
	}
	if got := c.suppressionIDs(rekeyCommitter)[carryConflictA1]; got != carryConflictSB {
		t.Fatalf("merged finding suppression = %q, want %q (precedence, not arbitration)", got, carryConflictSB)
	}
}

// seedCarryMemberPair seeds the suppression-carry member world (#10408,
// criterion 5b): like seedCarryMergePair, but the survivor record also
// carries the GHSA-0401-bbbb alias, which is never a deciding id in this
// world (the deciding records are the CVE survivor and the CVE dead id).
// The operator suppression names that GHSA member id, so every hide must
// come through the alias/member arm — the deciding-id arm can never match
// it.
func (c *rekeyProof) seedCarryMemberPair() {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyHolder, "g1")
	c.upsert(
		c.envelope(rekeyCommitter, "g1", "fact:10408:cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeySurvivor, "cve_id": rekeySurvivor, "source": "ghsa", "cvss_score": 7.5, "aliases": []string{rekeySurvivor, rekeyMember},
		}),
		c.envelope(rekeyCommitter, "g1", "fact:10408:dead", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeyDead, "cve_id": rekeyDead, "source": "nvd", "cvss_score": 5.0, "aliases": []string{rekeyDead},
		}),
		c.affectedPackage(rekeyCommitter, "g1", "fact:10408:pkg", rekeySurvivor, rekeyPackageA),
		c.affectedPackage(rekeyCommitter, "g1", "fact:10408:dpkg", rekeyDead, rekeyPackageB),
		c.consumption(rekeyCommitter, "g1", "fact:10408:cons", rekeyPackageA, "repository:r_10408_c"),
		c.consumption(rekeyCommitter, "g1", "fact:10408:dcons", rekeyPackageB, "repository:r_10408_c"),
		c.envelope(rekeyHolder, "g1", "fact:10408:hdead", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeyDead, "cve_id": rekeyDead, "source": "nvd", "cvss_score": 5.0, "aliases": []string{rekeyDead},
		}),
		c.affectedPackage(rekeyHolder, "g1", "fact:10408:hdpkg", rekeyDead, rekeyPackageB),
		c.consumption(rekeyHolder, "g1", "fact:10408:hdcons", rekeyPackageB, "repository:r_10408_h"),
	)
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyHolder, "g1")
}

// rescanWithdrawDeadRecordKeepMember mirrors rescanWithdrawDeadRecord but
// retains the GHSA member alias on the survivor: after withdrawal the dead
// id and the member id both survive only as aliases, so the member-anchored
// suppression must load through the alias arm alone.
func (c *rekeyProof) rescanWithdrawDeadRecordKeepMember() {
	c.t.Helper()
	c.copyGeneration(rekeyCommitter, "g1", "g2")
	if _, err := c.db.ExecContext(c.ctx, `
DELETE FROM fact_records WHERE scope_id = $1 AND generation_id = $2 AND fact_id IN ('fact:10408:dead:g2', 'fact:10408:dpkg:g2')`, rekeyCommitter, fanOutGen(rekeyCommitter, "g2")); err != nil {
		c.t.Fatalf("withdraw dead record: %v", err)
	}
	aliases, err := json.Marshal([]string{rekeySurvivor, rekeyDead, rekeyMember})
	if err != nil {
		c.t.Fatalf("marshal aliases: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
UPDATE fact_records SET payload = jsonb_set(payload, '{aliases}', $3::jsonb)
WHERE scope_id = $1 AND generation_id = $2 AND fact_id = 'fact:10408:cve:g2'`, rekeyCommitter, fanOutGen(rekeyCommitter, "g2"), string(aliases)); err != nil {
		c.t.Fatalf("retain member alias on survivor: %v", err)
	}
}

// TestRekeyCarryMemberSuppressionLive is the criterion-5b proof: the operator
// suppression names the GHSA member id, which is never a deciding id.
// Pre-merge the survivor hides through member-set match (pinned to the
// member-anchored suppression id) while the dead singleton stays active, so
// the anchor cannot blanket-hide. After the committer withdraws the dead
// record the merged finding stays hidden through the alias arm, on the
// committer and on the fanned-out holder, with the suppression fact itself
// byte-unchanged (carry by re-evaluation, never by rewriting the
// suppression).
func TestRekeyCarryMemberSuppressionLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedCarryMemberPair()
	c.seedRekeyOperator(rekeyMember)
	c.drain()
	t.Logf("pre-merge findings: %v", c.findingKeys())
	before := c.suppressionPayloads()
	if len(before) == 0 {
		t.Fatalf("no suppression facts seeded in the operator scope")
	}
	if got := c.suppressionStates(rekeyCommitter)[rekeySurvivor]; got == "" || got == "active" || got == "expired" {
		t.Fatalf("pre-merge survivor state = %q, want it hidden by the member suppression", got)
	}
	if got := c.suppressionIDs(rekeyCommitter)[rekeySurvivor]; got != "suppression-10408" {
		t.Fatalf("pre-merge survivor suppression = %q, want suppression-10408 (member-arm match, not deciding-id match)", got)
	}
	if got := c.suppressionStates(rekeyCommitter)[rekeyDead]; got != "active" {
		t.Fatalf("pre-merge dead finding state = %q, want active (the member anchor must not blanket-hide)", got)
	}

	c.rescanWithdrawDeadRecordKeepMember()
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	t.Logf("post-merge findings: %v", c.findingKeys())
	t.Logf("post-merge handled: %v", c.handled)
	if got := c.keysByScope(rekeyCommitter); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("committer keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
	if got := c.suppressionStates(rekeyCommitter)[rekeySurvivor]; got == "" || got == "active" || got == "expired" {
		t.Fatalf("merged committer finding state = %q, want it hidden by the carried member suppression", got)
	}
	if got := c.suppressionIDs(rekeyCommitter)[rekeySurvivor]; got != "suppression-10408" {
		t.Fatalf("merged committer suppression = %q, want suppression-10408 (member-arm carry)", got)
	}
	if c.handledCount(rekeyHolder) == 0 {
		t.Fatalf("holder never reopened after the merge (handled %v)", c.handled)
	}
	if got := c.keysByScope(rekeyHolder); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("holder keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
	if got := c.suppressionStates(rekeyHolder)[rekeySurvivor]; got == "" || got == "active" || got == "expired" {
		t.Fatalf("merged holder finding state = %q, want it hidden by the carried member suppression", got)
	}
	// Drive the holder to withdrawal as well: no scope holds a record
	// naming the dead id or the member id anymore, only the survivor's
	// aliases reference them. Both scopes must re-derive suppressed through
	// the alias arm alone.
	c.rescanWithdrawHolderDeadRecord()
	c.rerun(rekeyHolder)
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	t.Logf("post-withdrawal findings: %v", c.findingKeys())
	if got := c.keysByScope(rekeyCommitter); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("committer keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
	if got := c.suppressionStates(rekeyCommitter)[rekeySurvivor]; got == "" || got == "active" || got == "expired" {
		t.Fatalf("withdrawn committer finding state = %q, want it hidden by the carried member suppression", got)
	}
	if got := c.keysByScope(rekeyHolder); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("holder keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
	if got := c.suppressionStates(rekeyHolder)[rekeySurvivor]; got == "" || got == "active" || got == "expired" {
		t.Fatalf("withdrawn holder finding state = %q, want it hidden by the carried member suppression", got)
	}
	after := c.suppressionPayloads()
	if len(after) != len(before) {
		t.Fatalf("suppression facts = %d after the merge, want %d (carry must not rewrite suppressions)", len(after), len(before))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("suppression fact %d changed bytes across the merge:\nbefore: %s\nafter:  %s", i, before[i], after[i])
		}
	}
}
