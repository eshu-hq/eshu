// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

// Acceptance regressions for eshu-hq/eshu#9878 decision 1 (CVE-keyed
// re-trigger convergence): when a KEV or EPSS generation changes a CVE's
// enrichment, every scope holding a finding for that CVE re-runs
// supply_chain_impact so all copies under the canonical key score from one
// enrichment vintage. The harness is the #9695 convergence harness (real
// handler, writer, fan-out store and queue over a disposable database); the
// enrichment cases drive only a new EPSS or KEV generation and never rerun a
// holder scope by hand, so convergence can only arrive through the durable
// re-trigger edge.

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const enrichment9878KEVScope = "scope:9878:kev"

// enrichmentCopy is one scope's active finding copy reduced to the enrichment
// vintage it scored from.
type enrichmentCopy struct {
	epssProbability string
	knownExploited  bool
	priorityScore   int
}

// enrichmentCopies returns each scope's active finding copy for the
// convergence CVE.
func (c *convergence) enrichmentCopies() map[string]enrichmentCopy {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT f.scope_id,
       COALESCE(f.payload->>'epss_probability',''),
       COALESCE((f.payload->>'known_exploited')::boolean, FALSE),
       COALESCE((f.payload->>'priority_score')::int, 0)
FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = $1 AND NOT f.is_tombstone AND f.payload->>'cve_id' = $2`,
		facts.ReducerSupplyChainImpactFindingFactKind, convergenceCVE)
	if err != nil {
		c.t.Fatalf("read enrichment copies: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]enrichmentCopy{}
	for rows.Next() {
		var (
			scope string
			copy  enrichmentCopy
		)
		if err := rows.Scan(&scope, &copy.epssProbability, &copy.knownExploited, &copy.priorityScore); err != nil {
			c.t.Fatal(err)
		}
		out[scope] = copy
	}
	if err := rows.Err(); err != nil {
		c.t.Fatal(err)
	}
	return out
}

// enrichmentWinner rebuilds the canonical winners and returns the surfaced
// row for the convergence CVE.
func (c *convergence) enrichmentWinner() query.SupplyChainImpactFindingRow {
	c.t.Helper()
	if err := (postgres.SupplyChainImpactWinnersStore{DB: postgres.SQLDB{DB: c.db}}).RebuildAllWinners(c.ctx, time.Now().UTC()); err != nil {
		c.t.Fatalf("rebuild winners: %v", err)
	}
	store := query.NewPostgresSupplyChainImpactFindingStoreWithReadModel(c.db, true)
	rows, err := store.ListSupplyChainImpactFindings(c.ctx, query.SupplyChainImpactFindingFilter{CVEID: convergenceCVE, IncludeSuppressed: true, Limit: 10})
	if err != nil {
		c.t.Fatalf("list findings: %v", err)
	}
	if len(rows) != 1 {
		c.t.Fatalf("winners = %d rows, want exactly one canonical finding", len(rows))
	}
	return rows[0]
}

// activateGeneration supersedes the scope's current active generation and
// activates a fresh one, the way a collector generation commit does.
func (c *convergence) activateGeneration(scope, short string) string {
	c.t.Helper()
	gen := fanOutGen(scope, short)
	if _, err := c.db.ExecContext(c.ctx, `
UPDATE scope_generations SET status='superseded', superseded_at=now()
WHERE scope_id = $1 AND status = 'active'`, scope); err != nil {
		c.t.Fatalf("supersede %s generation: %v", scope, err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb)`, gen, scope); err != nil {
		c.t.Fatalf("insert generation %s: %v", gen, err)
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, gen); err != nil {
		c.t.Fatalf("activate %s: %v", gen, err)
	}
	return gen
}

// rescanEPSS plays a new EPSS collection for the holder scope: a new active
// generation holding one EPSS observation for the convergence CVE, plus its
// pending supply_chain_impact item. No other scope is touched.
func (c *convergence) rescanEPSS(short, probability, percentile, scoreDate string) {
	c.t.Helper()
	gen := c.activateGeneration(convergenceHolder, short)
	c.upsert(c.envelope(convergenceHolder, gen, "fact:9878:epss:"+short, chain.VulnerabilityEPSSScoreFactKind, map[string]any{
		"cve_id": convergenceCVE, "probability": probability, "percentile": percentile, "score_date": scoreDate,
	}))
	fanOutSeedItemAt(c.t, c.ctx, c.db, convergenceHolder, gen)
}

// seedKEVScope plays the first KEV collection that lists the convergence
// CVE: a new scope whose active generation holds one known-exploited fact,
// plus its pending item.
func (c *convergence) seedKEVScope() {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, enrichment9878KEVScope, "g1")
	gen := fanOutGen(enrichment9878KEVScope, "g1")
	c.upsert(c.envelope(enrichment9878KEVScope, gen, "fact:9878:kev", chain.VulnerabilityKnownExploitedFactKind, map[string]any{
		"cve_id": convergenceCVE, "date_added": "2026-11-01",
	}))
	fanOutSeedItemAt(c.t, c.ctx, c.db, enrichment9878KEVScope, gen)
}

func (c *convergence) handledCount(scope string) int {
	c.t.Helper()
	count := 0
	for _, handled := range c.handled {
		if len(handled) > len(scope) && handled[:len(scope)] == scope && handled[len(scope)] == '#' {
			count++
		}
	}
	return count
}

// TestEPSSLoweredConvergesAllCopiesAndWinner9878Live is the EPSS half of the
// #9878 acceptance pair. Both scopes first converge on the seeded EPSS
// vintage. A lowered EPSS observation with a newer score_date then lands in
// the EPSS scope only: before the drain the winner still carries the older
// higher vintage (the masking state), and after the drain — with no manual
// rerun of the source scope — every copy and the winner carry the lowered
// score, because the EPSS scope's changed copy re-triggered the holder.
func TestEPSSLoweredConvergesAllCopiesAndWinner9878Live(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()

	before := c.enrichmentCopies()
	if len(before) != 2 {
		t.Fatalf("copies before = %v, want source and holder copies", before)
	}
	for scope, copy := range before {
		if copy.epssProbability != "0.5" {
			t.Fatalf("copy %s epss = %q before, want seeded 0.5 (handled %v)", scope, copy.epssProbability, c.handled)
		}
	}
	winnerBefore := c.enrichmentWinner()
	if winnerBefore.EPSSProbability != "0.5" {
		t.Fatalf("winner epss = %q before, want 0.5", winnerBefore.EPSSProbability)
	}

	c.rescanEPSS("g2", "0.05", "0.10", "2026-11-01")

	// The masking state: the lowered vintage is committed but no pass has
	// re-derived yet, so the winner still serves the older higher copy.
	if stale := c.enrichmentWinner(); stale.EPSSProbability != "0.5" {
		t.Fatalf("winner epss before drain = %q, want stale 0.5", stale.EPSSProbability)
	}

	sourceBefore := c.handledCount(convergenceSource)
	c.drain()
	if got := c.handledCount(convergenceSource); got <= sourceBefore {
		t.Fatalf("source scope handled %d times after EPSS rescan drain, want a fan-out re-run beyond %d (handled %v)", got, sourceBefore, c.handled)
	}

	after := c.enrichmentCopies()
	if len(after) != 2 {
		t.Fatalf("copies after = %v, want source and holder copies", after)
	}
	for scope, copy := range after {
		if copy.epssProbability != "0.05" {
			t.Fatalf("copy %s epss = %q after, want converged 0.05 (handled %v)", scope, copy.epssProbability, c.handled)
		}
	}
	winnerAfter := c.enrichmentWinner()
	if winnerAfter.EPSSProbability != "0.05" {
		t.Fatalf("winner epss = %q after, want lowered 0.05", winnerAfter.EPSSProbability)
	}
	if winnerAfter.PriorityScore > winnerBefore.PriorityScore {
		t.Fatalf("winner priority rose %d -> %d on a lowered EPSS", winnerBefore.PriorityScore, winnerAfter.PriorityScore)
	}
	t.Logf("winner priority %d -> %d across the lowered rescore", winnerBefore.PriorityScore, winnerAfter.PriorityScore)
}

// TestNewKEVListingReachesPreExistingFindings9878Live is the KEV half of the
// pair. The finding exists in both scopes with no KEV signal. A first KEV
// listing for its CVE then lands in the KEV scope only; after the drain every
// pre-existing copy and the winner carry known_exploited, reached solely
// through the re-trigger edge.
func TestNewKEVListingReachesPreExistingFindings9878Live(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()

	before := c.enrichmentCopies()
	if len(before) != 2 {
		t.Fatalf("copies before = %v, want source and holder copies", before)
	}
	for scope, copy := range before {
		if copy.knownExploited {
			t.Fatalf("copy %s known_exploited before KEV listing, want false", scope)
		}
	}
	if c.enrichmentWinner().KnownExploited {
		t.Fatalf("winner known_exploited before KEV listing, want false")
	}

	c.seedKEVScope()
	c.drain()

	after := c.enrichmentCopies()
	for _, scope := range []string{convergenceSource, convergenceHolder} {
		copy, ok := after[scope]
		if !ok {
			t.Fatalf("copy %s missing after KEV drain (copies %v, handled %v)", scope, after, c.handled)
		}
		if !copy.knownExploited {
			t.Fatalf("copy %s known_exploited = false after KEV listing, want true (handled %v)", scope, c.handled)
		}
	}
	winner := c.enrichmentWinner()
	if !winner.KnownExploited {
		t.Fatalf("winner known_exploited = false after KEV listing, want true")
	}
	found := false
	for _, code := range winner.PriorityReasonCodes {
		if code == "cisa_kev" {
			found = true
		}
	}
	if !found {
		t.Fatalf("winner reason codes = %q, want cisa_kev", winner.PriorityReasonCodes)
	}
}

// TestEnrichmentCommittedDuringHolderPassStillConverges9878Live is the
// interleave half of the concurrency proof: a holder pass that loaded its
// evidence before the lowered EPSS generation committed must not lose that
// generation. The EPSS pass's fan-out flags the in-flight holder for replay,
// so after the stale commit acknowledges, one more pass re-derives the copy
// from the new vintage. No scope run is serialized: the two passes overlap
// (the gated holder pass is parked inside its writer while the EPSS pass
// commits), and convergence comes from the replay flag, not from ordering.
func TestEnrichmentCommittedDuringHolderPassStillConverges9878Live(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()

	// Park a source-scope pass after its evidence load, before its commit:
	// it holds the pre-rescore vintage.
	c.rerun(convergenceSource)
	gated := c.startGatedPass(c.raceClaim(convergenceSource))

	// The lowered EPSS generation commits and its pass runs while the gated
	// pass is still in flight.
	c.rescanEPSS("g2", "0.05", "0.10", "2026-11-01")
	holder := c.raceClaim(convergenceHolder)
	result, err := c.handler.Handle(c.ctx, holder)
	if err != nil {
		t.Fatalf("holder handle: %v", err)
	}
	if err := c.queue.Ack(c.ctx, holder, result); err != nil {
		t.Fatal(err)
	}

	// The gated pass now commits its stale vintage and acknowledges; the
	// replay flag returns it to pending instead of losing the rescore.
	gated.finish()
	if mid := c.enrichmentCopies()[convergenceSource]; mid.epssProbability != "0.5" {
		t.Fatalf("source copy epss after stale commit = %q, want the gated pass's 0.5", mid.epssProbability)
	}

	c.drain()
	after := c.enrichmentCopies()
	for scope, copy := range after {
		if copy.epssProbability != "0.05" {
			t.Fatalf("copy %s epss = %q after interleave drain, want converged 0.05 (handled %v)", scope, copy.epssProbability, c.handled)
		}
	}
	if winner := c.enrichmentWinner(); winner.EPSSProbability != "0.05" {
		t.Fatalf("winner epss = %q after interleave, want 0.05", winner.EPSSProbability)
	}
}

// TestIdenticalEPSSRescanFansOutNothing9878Live is the duplicate-delivery
// half: a new EPSS generation carrying the identical observation re-derives
// an identical copy, so the content digest is unchanged, no CVE is reported
// changed, and no holder is re-triggered. (The FreshnessHint skip normally
// prevents this generation upstream; this proves the reducer edge is
// idempotent even when one slips through.)
func TestIdenticalEPSSRescanFansOutNothing9878Live(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	sourceBefore := c.handledCount(convergenceSource)

	c.rescanEPSS("g2", "0.5", "0.9", "2026-10-31")
	c.drain()

	if got := c.handledCount(convergenceSource); got != sourceBefore {
		t.Fatalf("source scope handled %d times after identical EPSS rescan, want unchanged %d (handled %v)", got, sourceBefore, c.handled)
	}
	for scope, copy := range c.enrichmentCopies() {
		if copy.epssProbability != "0.5" {
			t.Fatalf("copy %s epss = %q, want unchanged 0.5", scope, copy.epssProbability)
		}
	}
}
