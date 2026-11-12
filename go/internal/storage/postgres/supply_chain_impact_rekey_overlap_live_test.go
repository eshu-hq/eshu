// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const rekeyOverlapHolderB = "scope:10408:holder-b"

// seedOverlapPair seeds the two-holder dead-id world: the committer holds
// two lone CVE findings, and each holder holds the higher (soon dead) id as
// a singleton over the same package. The two holder copies share one
// canonical winner key, so the overlap proves the contested-key path too.
func (c *rekeyProof) seedOverlapPair() {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyHolder, "g1")
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyOverlapHolderB, "g1")
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
		c.consumption(rekeyHolder, "g1", "fact:10408:hcons", rekeyPackageB, "repository:r_10408_h"),
		c.envelope(rekeyOverlapHolderB, "g1", "fact:10408:bdead", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeyDead, "cve_id": rekeyDead, "source": "nvd", "cvss_score": 5.0, "aliases": []string{rekeyDead},
		}),
		c.affectedPackage(rekeyOverlapHolderB, "g1", "fact:10408:bdpkg", rekeyDead, rekeyPackageB),
		c.consumption(rekeyOverlapHolderB, "g1", "fact:10408:bcons", rekeyPackageB, "repository:r_10408_hb"),
	)
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyHolder, "g1")
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyOverlapHolderB, "g1")
}

// overlapGateWriter parks every pass at writer entry until the test releases
// it, and counts arrivals so the test can prove both holders' write
// transactions overlapped the committer's merge. Unlike raceGateWriter it
// blocks every call, not just the first, so two gated passes rendezvous.
type overlapGateWriter struct {
	inner   supplychaincore.SupplyChainImpactWriter
	arrived atomic.Int64
	release chan struct{}
}

func (g *overlapGateWriter) WriteSupplyChainImpactFindings(ctx context.Context, w supplychaincore.SupplyChainImpactWrite) (supplychaincore.SupplyChainImpactWriteResult, error) {
	g.arrived.Add(1)
	select {
	case <-g.release:
	case <-ctx.Done():
	}
	return g.inner.WriteSupplyChainImpactFindings(ctx, w)
}

// survivorMembers returns the advisory-group member set the scope's active
// survivor finding persists, for the determinism comparison.
func (c *rekeyProof) survivorMembers(scope string) string {
	c.t.Helper()
	var members string
	if err := c.db.QueryRowContext(c.ctx, `
SELECT COALESCE(f.payload #>> '{provenance,advisory_group_members}', '')
FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND f.scope_id = $1 AND f.payload->>'cve_id' = $2`, scope, rekeySurvivor).Scan(&members); err != nil {
		c.t.Fatalf("read survivor members in %s: %v", scope, err)
	}
	return members
}

// TestRekeyTwoHolderScopesRekeySameComponentLive is the criterion-8 proof:
// two holder scopes re-key the same component in overlapping passes. Both
// holders' passes load the pre-merge pair, park at writer entry, and stay
// claimed while the committer merges and drains: the merge's fan-out flags
// both in flight, both commit their stale pair with no uniqueness or fencing
// error (fact ids embed the scope, the fence is per scope and generation),
// and the durable replay reruns both onto the survivor group with identical
// member sets and no manual rerun.
func TestRekeyTwoHolderScopesRekeySameComponentLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedOverlapPair()
	c.drain()
	// Like seedMergePair, the alias-closed load mirrors both lone groups into
	// every scope: each holder already holds the survivor's row too. What
	// matters is the dead-id row both hold: it must retire through the
	// overlapping passes and converge onto the survivor group.
	for _, scope := range []string{rekeyHolder, rekeyOverlapHolderB} {
		if got := c.keysByScope(scope); !slices.Contains(got, rekeyDead) {
			t.Fatalf("holder %s keys = %v, want the dead group %s held before the merge", scope, got, rekeyDead)
		}
	}

	c.rerun(rekeyHolder)
	c.rerun(rekeyOverlapHolderB)
	// Claim both holders without assuming queue order, then sort them by
	// scope: the drain left two pending items and the claim order is not
	// part of the contract.
	byScope := map[string]reducercontract.Intent{}
	for range 2 {
		intent := c.claim("")
		byScope[intent.ScopeID] = intent
	}
	intentA, intentB := byScope[rekeyHolder], byScope[rekeyOverlapHolderB]
	if intentA.IntentID == "" || intentB.IntentID == "" {
		t.Fatalf("claimed holders = %v, want both %s and %s", byScope, rekeyHolder, rekeyOverlapHolderB)
	}
	gate := &overlapGateWriter{inner: c.handler.Writer, release: make(chan struct{})}
	gated := c.handler
	gated.Writer = gate
	done := make(chan error, 2)
	var wg sync.WaitGroup
	run := func(intent reducercontract.Intent) {
		defer wg.Done()
		result, err := gated.Handle(c.ctx, intent)
		if err != nil {
			done <- err
			return
		}
		done <- c.queue.Ack(c.ctx, intent, result)
	}
	wg.Add(2)
	go run(intentA)
	go run(intentB)
	deadline := time.Now().Add(30 * time.Second)
	for gate.arrived.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if gate.arrived.Load() != 2 {
		t.Fatalf("only %d holder passes parked at writer entry, want both overlapping the merge", gate.arrived.Load())
	}

	c.rescanAddEdge()
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	for _, intent := range []reducercontract.Intent{intentA, intentB} {
		if st := fanOutItem(t, c.ctx, c.db, intent.IntentID); st.status != "claimed" && st.status != "running" {
			t.Fatalf("holder item %s status = %s, want it still claimed while gated", intent.IntentID, st.status)
		}
	}
	close(gate.release)
	wg.Wait()
	close(done)
	for err := range done {
		if err != nil {
			t.Fatalf("overlapping holder pass: %v (want no uniqueness or fencing error)", err)
		}
	}
	c.drain()

	t.Logf("post-overlap findings: %v", c.findingKeys())
	for _, scope := range []string{rekeyHolder, rekeyOverlapHolderB} {
		if got := c.keysByScope(scope); len(got) != 1 || got[0] != rekeySurvivor {
			t.Fatalf("holder %s keys = %v, want the survivor group [%s]: a holder pass was lost", scope, got, rekeySurvivor)
		}
		// The gated pass runs outside the drain, so exactly one drain
		// handling per holder is its durable replay: the reopened pass ran
		// with no manual rerun.
		if n := c.handledCount(scope); n != 1 {
			t.Fatalf("holder %s handled %d time(s) after the overlap, want exactly its replay", scope, n)
		}
	}
	if membersA, membersB := c.survivorMembers(rekeyHolder), c.survivorMembers(rekeyOverlapHolderB); membersA != membersB {
		t.Fatalf("survivor member sets differ: %s = %s, %s = %s", rekeyHolder, membersA, rekeyOverlapHolderB, membersB)
	} else if membersA == "" || membersA == "[]" {
		t.Fatalf("survivor member set = %q, want the merged group ids", membersA)
	}
	if got := c.keysByScope(rekeyCommitter); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("committer keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}

	if err := (postgres.SupplyChainImpactWinnersStore{DB: postgres.SQLDB{DB: c.db}}).RebuildAllWinners(c.ctx, time.Now().UTC()); err != nil {
		t.Fatalf("rebuild winners: %v", err)
	}
	// The read model must hold exactly the derived set: one row per
	// distinct (package, repository) the survivor findings name, and no row
	// under the dead id.
	var derived int
	if err := c.db.QueryRowContext(c.ctx, `
SELECT count(*) FROM (
  SELECT DISTINCT COALESCE(f.payload->>'package_id', ''),
    COALESCE(f.payload->>'repository_id', ''),
    COALESCE(f.payload->>'subject_digest', ''),
    COALESCE(NULLIF(f.payload->>'cve_id', ''), NULLIF(f.payload->>'advisory_id', ''), ''),
    COALESCE(f.payload->>'advisory_id', '')
  FROM fact_records f
  JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
  WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
    AND f.payload->>'cve_id' = $1)`, rekeySurvivor).Scan(&derived); err != nil {
		t.Fatalf("read derived survivor keys: %v", err)
	}
	var dead, survivor int
	if err := c.db.QueryRowContext(c.ctx, `SELECT count(*) FROM supply_chain_impact_canonical_winners WHERE cve_id = $1`, rekeyDead).Scan(&dead); err != nil {
		t.Fatalf("read dead winners: %v", err)
	}
	if err := c.db.QueryRowContext(c.ctx, `SELECT count(*) FROM supply_chain_impact_canonical_winners WHERE cve_id = $1`, rekeySurvivor).Scan(&survivor); err != nil {
		t.Fatalf("read survivor winners: %v", err)
	}
	if dead != 0 || survivor != derived {
		t.Fatalf("winners after the overlap: dead = %d (want 0), survivor = %d (want the derived %d)", dead, survivor, derived)
	}
}
