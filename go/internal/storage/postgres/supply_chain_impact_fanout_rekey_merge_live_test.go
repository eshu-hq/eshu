// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

const (
	rekeySurvivor = rekeyCVE
	rekeyDead     = "CVE-2026-0402"
)

// seedMergePair seeds the dead-id world: the committer holds two lone CVE
// findings, and the holder holds the higher (soon dead) id as a singleton.
// The merge event retires the dead group id; no new committer row is keyed by
// it.
func (c *rekeyProof) seedMergePair() {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyHolder, "g1")
	c.upsert(
		c.envelope(rekeyCommitter, "g1", "fact:10408:cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeySurvivor, "cve_id": rekeySurvivor, "source": "ghsa", "cvss_score": 7.5, "aliases": []string{rekeySurvivor},
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

// rescanAddEdge copies the committer to a second generation with an alias edge
// from the dead id to the survivor, modelling the merge.
func (c *rekeyProof) rescanAddEdge() {
	c.t.Helper()
	c.copyGeneration(rekeyCommitter, "g1", "g2")
	aliases, err := json.Marshal([]string{rekeySurvivor})
	if err != nil {
		c.t.Fatalf("marshal aliases: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
UPDATE fact_records SET payload = jsonb_set(payload, '{aliases}', $3::jsonb)
WHERE scope_id = $1 AND generation_id = $2 AND fact_id = 'fact:10408:dead:g2'`, rekeyCommitter, fanOutGen(rekeyCommitter, "g2"), string(aliases)); err != nil {
		c.t.Fatalf("add edge: %v", err)
	}
}

// TestRekeyMergeReopensDeadIDHolderLive pins the dead-id path: the merge
// retires the dead group id (no committer row is keyed by it) and the holder
// keyed by it must reopen with no manual rerun and converge onto the survivor
// group. The merged row's member set already names the dead id, so this
// passes before and after L2; it guards the member expansion L2 consumes.
func TestRekeyMergeReopensDeadIDHolderLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedMergePair()
	c.drain()
	t.Logf("pre-merge findings: %v", c.findingKeys())
	// Both scopes mirror: the alias-closed load sees the same two lone
	// groups, so the holder already holds the survivor's row too. What
	// matters is the dead-id row: it must retire through fan-out.
	if got := c.keysByScope(rekeyHolder); len(got) != 2 || got[0] != rekeySurvivor || got[1] != rekeyDead {
		t.Fatalf("holder keys = %v, want both lone groups [%s %s]", got, rekeySurvivor, rekeyDead)
	}

	c.rescanAddEdge()
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	t.Logf("post-merge findings: %v", c.findingKeys())
	t.Logf("post-merge handled: %v", c.handled)
	if c.handledCount(rekeyHolder) == 0 {
		t.Fatalf("holder never reopened after the merge (handled %v; findings %v)", c.handled, c.findingKeys())
	}
	if got := c.keysByScope(rekeyHolder); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("holder keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
	if got := c.keysByScope(rekeyCommitter); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("committer keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
}

// TestRekeyMergeInFlightHolderConvergesViaReplayLive pins the in-flight-first
// rule on a re-key: the holder rescans and its first pass is still gated
// (claimed, no rows yet) when the committer merges. The in-flight read flags
// it, its pass commits the pre-merge pair, and the durable replay reruns it
// onto the survivor group with no manual rerun.
func TestRekeyMergeInFlightHolderConvergesViaReplayLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedMergePair()
	c.drain()
	c.copyGeneration(rekeyHolder, "g1", "g2")
	holderIntent := c.claim(rekeyHolder)

	gate := &raceGateWriter{inner: c.handler.Writer, loaded: make(chan struct{}), release: make(chan struct{})}
	holderHandler := c.handler
	holderHandler.Writer = gate
	done := make(chan error, 1)
	go func() {
		result, err := holderHandler.Handle(c.ctx, holderIntent)
		if err != nil {
			done <- err
			return
		}
		done <- c.queue.Ack(c.ctx, holderIntent, result)
	}()
	<-gate.loaded

	c.rescanAddEdge()
	c.rerun(rekeyCommitter)
	c.drain()
	st := fanOutItem(t, c.ctx, c.db, holderIntent.IntentID)
	if st.status != "claimed" && st.status != "running" {
		t.Fatalf("holder item status = %s, want it still claimed while gated", st.status)
	}
	close(gate.release)
	if err := <-done; err != nil {
		t.Fatalf("holder pass: %v", err)
	}
	c.drain()
	t.Logf("post-replay findings: %v", c.findingKeys())
	if got := c.keysByScope(rekeyHolder); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("holder keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
}

// TestRekeyDropCommitThenCrashConvergesViaRecoveryLive pins the owed-retry
// union on a re-key: the committer's drop pass commits, its first holder read
// fails, and the retry's own rewrite shows no change. The retry still fans out
// through the member-expanded recovery set (own plus prior generation rows),
// so the fallback-singleton holder reopens with no manual rerun.
func TestRekeyDropCommitThenCrashConvergesViaRecoveryLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedDropPair(1)
	c.drain()
	t.Logf("pre-drop findings: %v", c.findingKeys())
	// Arm the flake only for the event phase, so the first holder read of
	// the drop pass fails after its commit and the owed retry must carry
	// the fan-out through the recovery set.
	flaky := &flakyFindFanOut{SupplyChainImpactFanOut: c.fanOut}
	c.handler.FanOut = flaky
	c.holderHandler = nil
	c.rescanDropMember()
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	t.Logf("post-retry findings: %v", c.findingKeys())
	if !flaky.failed.Load() {
		t.Fatal("flaky holder read never fired")
	}
	if c.handledCount(rekeyHolder) == 0 {
		t.Fatalf("holder never reopened on the owed retry (handled %v; findings %v)", c.handled, c.findingKeys())
	}
	if got := c.keysByScope(rekeyHolder); len(got) != 1 || got[0] != rekeyMember {
		t.Fatalf("holder keys = %v, want the fallback singleton [%s] intact", got, rekeyMember)
	}
}
