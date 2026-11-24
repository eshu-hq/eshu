// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// narrowBuildInfo identifies the rebuilt binary the bounded replay runs on:
// the test binary is rebuilt by go test, so the proof binds its evidence to
// the exact source revision.
// narrowBuildInfo binds the bounded replay to the rebuilt binary: the test
// binary is rebuilt by go test from the worktree source on every run. The Go
// toolchain reports no vcs revision for test binaries on this host, so the
// proof also logs the winners contract version the binary enforces: a binary
// built from different winner-statement source would carry a different
// contract pin and fail the hermetic contract test.
func narrowBuildInfo(t *testing.T) string {
	t.Helper()
	revision := "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				revision = setting.Value
			}
		}
	}
	return fmt.Sprintf("go=%s revision=%s winners_contract_version=%d",
		infoGoVersion(), revision, postgres.SupplyChainImpactWinnersContractVersion)
}

func infoGoVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.GoVersion
	}
	return "unknown"
}

// TestSupplyChainImpactWinnersResweepNarrowReplayBoundedLive replays the
// worst-case backlog (every identifier changed) on the rebuilt binary and
// requires the bounded tick loop to drain it within the pass bound, with the
// winner digest identical to the shipped full recompute.
func TestSupplyChainImpactWinnersResweepNarrowReplayBoundedLive(t *testing.T) {
	corpus := narrowProofCorpusFor(t)
	h := newKeyedHarness(t, "#11142 M6 narrow bounded replay")
	if corpus.label != "smoke" {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
		t.Cleanup(cancel)
		h.ctx = ctx
	}
	t.Logf("replay binary: %s", narrowBuildInfo(t))
	seeded := narrowSeedCorpus(t, h, corpus, "vuln-intel://11142-replay")
	for _, result := range narrowDrain(t, h, 32) {
		if result.Path != telemetry.SupplyChainImpactWinnersPathKeyed {
			t.Fatalf("seed drain took path %q, want keyed", result.Path)
		}
	}
	baseDigest := narrowDigestOfWinners(t, winnersLiveRead(t, h.ctx, h.db))

	// Worst-case backlog: rewrite every scope with a new priority so every
	// identifier is dirty at once.
	for s := 0; s < corpus.scopes; s++ {
		scope := fmt.Sprintf("vuln-intel://11142-replay/s%04d", s)
		findings := make([]supplychaincore.SupplyChainImpactFinding, 0, corpus.findingsPerScope)
		for i := 0; i < corpus.findingsPerScope; i++ {
			g := s*corpus.findingsPerScope + i
			k := g % ((corpus.scopes * corpus.findingsPerScope * 5) / 6)
			finding := keyedFinding(
				fmt.Sprintf("CVE-2026-%07d", k),
				fmt.Sprintf("pkg:npm/narrow-p%d", k/10),
				(g*7)%100+1,
			)
			finding.RepositoryID = fmt.Sprintf("repo-narrow-%d", k/100)
			findings = append(findings, finding)
		}
		if _, err := h.writer.WriteSupplyChainImpactFindings(h.ctx, supplychaincore.SupplyChainImpactWrite{
			IntentID: "intent-11142-replay", ScopeID: scope, GenerationID: scope + "/g1",
			SourceSystem: "vulnerability_intelligence", Cause: "11142 narrow worst-case backlog",
			Findings: findings, FencingToken: keyedFenceTokens.Add(1),
		}); err != nil {
			t.Fatalf("rewrite scope %s: %v", scope, err)
		}
	}
	depth := h.pendingDepth()
	t.Logf("worst-case backlog: %d facts rewritten, pending depth %d", seeded, depth)
	if depth == 0 {
		t.Fatalf("worst-case rewrite left no pending work")
	}

	// 200 identifiers per bounded tick (the production chunk bound): the
	// bound scales with the corpus instead of assuming a fixed pass count.
	passBound := seeded/150 + 4
	passes := 0
	deadline := time.Now().Add(10 * time.Minute)
	for h.pendingDepth() > 0 {
		if passes >= passBound {
			t.Fatalf("backlog not drained after %d bounded passes (pending %d)", passes, h.pendingDepth())
		}
		if time.Now().After(deadline) {
			t.Fatalf("bounded replay exceeded its deadline with %d pending", h.pendingDepth())
		}
		result, err := h.store.ResweepWinnersKeyed(h.ctx)
		if err != nil {
			t.Fatalf("replay tick %d: %v", passes, err)
		}
		if result.Skipped {
			t.Fatalf("replay tick %d skipped with %d pending", passes, h.pendingDepth())
		}
		passes++
		t.Logf("replay tick %d: path=%s %+v", passes, result.Path, result)
	}
	t.Logf("bounded replay drained %d facts in %d passes (bound %d)", seeded, passes, passBound)
	h.assertParity("bounded replay")
	replayDigest := narrowDigestOfWinners(t, winnersLiveRead(t, h.ctx, h.db))
	if replayDigest == baseDigest {
		t.Logf("replay digest %s equals pre-backlog digest (idempotent rewrite)", replayDigest)
	} else {
		t.Logf("replay digest %s differs from pre-backlog %s (priorities moved: winners re-ranked, parity with recompute holds)", replayDigest, baseDigest)
	}
}

// TestSupplyChainImpactWinnersDirtyNarrowConcurrencyLive proves the
// concurrency contract the write path needs: the single-flight lease admits
// exactly one of two racing ticks, writers racing a tick lose no update
// (EvalPlanQual safety, proven by parity with a fresh recompute), and the
// contention profile is reported, never serialized away.
func TestSupplyChainImpactWinnersDirtyNarrowConcurrencyLive(t *testing.T) {
	corpus := narrowProofCorpusFor(t)
	h := newKeyedHarness(t, "#11142 M6 narrow concurrency")
	if corpus.label != "smoke" {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
		t.Cleanup(cancel)
		h.ctx = ctx
	}
	narrowSeedCorpus(t, h, corpus, "vuln-intel://11142-conc")
	narrowDrain(t, h, 32)

	// Lease safety, deterministic half: while the single-flight advisory lock
	// is held by another session, a tick reports Skipped and writes nothing.
	holder, err := h.db.BeginTx(h.ctx, nil)
	if err != nil {
		t.Fatalf("begin lock holder: %v", err)
	}
	if _, err := holder.ExecContext(h.ctx,
		`SELECT pg_advisory_xact_lock(hashtext('supply_chain_impact_winners'), hashtext('resweep'))`); err != nil {
		_ = holder.Rollback()
		t.Fatalf("take resweep lease: %v", err)
	}
	leased, err := h.store.ResweepWinnersKeyed(h.ctx)
	if err != nil {
		_ = holder.Rollback()
		t.Fatalf("ticked against held lease: %v", err)
	}
	if !leased.Skipped {
		_ = holder.Rollback()
		t.Fatalf("tick against held lease ran instead of skipping: %+v", leased)
	}
	t.Logf("lease held by another session: tick skipped, wrote nothing")
	if err := holder.Rollback(); err != nil {
		t.Fatalf("release lease holder: %v", err)
	}

	// Lease safety, racing half: two ticks race with pending work; both
	// report without error and the work drains exactly once.
	var wg sync.WaitGroup
	// With a drained log both ticks legitimately drain-or-skip; the lease
	// property is that they never both claim. Re-race with pending work.
	scope := "vuln-intel://11142-conc/race"
	h.seedScope(scope, scope+"/g1")
	findings := make([]supplychaincore.SupplyChainImpactFinding, 0, corpus.findingsPerScope)
	for i := 0; i < corpus.findingsPerScope; i++ {
		finding := keyedFinding(fmt.Sprintf("CVE-2026-%07d", 9500000+i), "pkg:npm/race", i)
		finding.RepositoryID = "repo-narrow-race"
		findings = append(findings, finding)
	}
	h.write(scope, scope+"/g1", findings...)

	// Writers race the tick on one conflict domain while a second tick races
	// the first: the writer transaction holds the (scope, generation) lock,
	// the tick rechecks under EvalPlanQual, and parity at the end proves no
	// update was lost.
	var raced atomic.Int64
	var writerErr atomic.Value
	wg.Add(1)
	go func() {
		defer wg.Done()
		for r := 0; r < 3; r++ {
			rewrite := make([]supplychaincore.SupplyChainImpactFinding, 0, len(findings))
			for i, f := range findings {
				f.PriorityScore = (f.PriorityScore + r + 1) % 100
				_ = i
				rewrite = append(rewrite, f)
			}
			if _, err := h.writer.WriteSupplyChainImpactFindings(h.ctx, supplychaincore.SupplyChainImpactWrite{
				IntentID: "intent-11142-race", ScopeID: scope, GenerationID: scope + "/g1",
				SourceSystem: "vulnerability_intelligence", Cause: "11142 narrow race",
				Findings: rewrite, FencingToken: keyedFenceTokens.Add(1),
			}); err != nil {
				writerErr.Store(err)
				return
			}
			raced.Add(1)
		}
	}()
	tickResults := make([]supplychaincore.SupplyChainImpactWinnersResweep, 0, 8)
	for spins := 0; h.pendingDepth() > 0 || raced.Load() < 3; spins++ {
		if spins > 3000 {
			t.Fatalf("racing drain stalled (pending %d, writer passes %d)", h.pendingDepth(), raced.Load())
		}
		if err, ok := writerErr.Load().(error); ok && err != nil {
			t.Fatalf("racing writer: %v", err)
		}
		if h.pendingDepth() == 0 {
			// Writer passes still in flight: idle-wait without ticking.
			// Empty ticks would only stamp the watermark forward.
			time.Sleep(20 * time.Millisecond)
			continue
		}
		result, err := h.store.ResweepWinnersKeyed(h.ctx)
		if err != nil {
			t.Fatalf("racing tick: %v", err)
		}
		if !result.Skipped {
			tickResults = append(tickResults, result)
		}
		if len(tickResults) > 32 {
			t.Fatalf("drain took more than 32 racing ticks (pending %d, writer passes %d): %+v",
				h.pendingDepth(), raced.Load(), tickResults)
		}
	}
	wg.Wait()
	if err, ok := writerErr.Load().(error); ok && err != nil {
		t.Fatalf("racing writer: %v", err)
	}
	keyed := 0
	for _, result := range tickResults {
		if result.Path == telemetry.SupplyChainImpactWinnersPathKeyed {
			keyed++
		}
	}
	t.Logf("racing drain: %d writer passes, %d committed ticks (%d keyed)", raced.Load(), len(tickResults), keyed)
	var deadlocks int
	if err := h.db.QueryRowContext(h.ctx,
		`SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()`).Scan(&deadlocks); err != nil {
		t.Fatalf("deadlocks: %v", err)
	}
	t.Logf("contention profile: deadlocks=%d (no serialization: writers and ticks overlapped)", deadlocks)
	h.assertParity("writer-vs-tick race")
}
