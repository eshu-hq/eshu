// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// Chunked-consume proofs for #9805 CODE-NEEDED-1 (arbiter ruling, #9975 shim):
// one keyed transaction must bound the distinct (package_id, repository_id,
// subject_digest) triples it probes and the candidate fact rows it fetches, a
// bounded pass must leave the rest of the dirty log queued and must not stamp
// the watermark, and the drained table must equal a fresh full recompute.

func chunkedWatermark(h *keyedHarness) time.Time {
	h.t.Helper()
	var at time.Time
	if err := h.db.QueryRowContext(h.ctx,
		`SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1`).Scan(&at); err != nil {
		h.t.Fatalf("read watermark: %v", err)
	}
	return at.UTC()
}

// chunkedTick runs one keyed tick without requiring a drained log.
func chunkedTick(h *keyedHarness) supplychaincore.SupplyChainImpactWinnersResweep {
	h.t.Helper()
	result, err := h.store.ResweepWinnersKeyed(h.ctx, h.tick())
	if err != nil || result.Skipped {
		h.t.Fatalf("chunked keyed resweep = %+v, %v", result, err)
	}
	return result
}

// chunkedDrain ticks until the dirty log is empty and returns the ticks run.
func chunkedDrain(h *keyedHarness) []supplychaincore.SupplyChainImpactWinnersResweep {
	h.t.Helper()
	var ticks []supplychaincore.SupplyChainImpactWinnersResweep
	for range 50 {
		if h.dirtyDepth() == 0 {
			return ticks
		}
		ticks = append(ticks, chunkedTick(h))
	}
	h.t.Fatalf("dirty log never drained after 50 chunked ticks (depth %d)", h.dirtyDepth())
	return ticks
}

// chunkedFindings builds findings over triples distinct package triples, keys
// CVEs on each, with the priority bump applied.
func chunkedFindings(prefix string, triples, keys, bump int) []supplychaincore.SupplyChainImpactFinding {
	findings := make([]supplychaincore.SupplyChainImpactFinding, 0, triples*keys)
	for tr := range triples {
		for k := range keys {
			findings = append(findings, keyedFinding(
				fmt.Sprintf("CVE-2026-%s-%02d-%02d", prefix, tr, k),
				fmt.Sprintf("pkg:npm/%s-%02d", prefix, tr), 10+k+bump))
		}
	}
	return findings
}

func TestSupplyChainImpactWinnersKeyedChunkedConsumeLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 chunked consume proof")
	h.seedScope("vuln-intel://9805-chunked/s1", "vuln-intel://9805-chunked/s1/g1")

	h.write("vuln-intel://9805-chunked/s1", "vuln-intel://9805-chunked/s1/g1", chunkedFindings("chunk", 12, 2, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}
	before := chunkedWatermark(h)

	// Rewrite every finding with a new priority: 12 triples go dirty at once,
	// the QA shape (a backlog far past one bounded transaction).
	h.write("vuln-intel://9805-chunked/s1", "vuln-intel://9805-chunked/s1/g1", chunkedFindings("chunk", 12, 2, 1000)...)
	if depth := h.dirtyDepth(); depth == 0 {
		t.Fatal("rewrite left the dirty log empty")
	}

	h.store.ChunkMaxTriples = 5
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("first tick over 12 dirty triples with a 5-triple bound = %+v, want Bounded", first)
	}
	if first.Triples > 5 || first.Triples == 0 {
		t.Errorf("first tick probed %d triples, want 1..5", first.Triples)
	}
	if first.Keys > 10 {
		t.Errorf("first tick recomputed %d keys, want at most the 10 keys of 5 triples", first.Keys)
	}
	if depth := h.dirtyDepth(); depth == 0 {
		t.Error("bounded tick drained the dirty log: the bound did not bound the transaction")
	}
	if got := chunkedWatermark(h); !got.Equal(before) {
		t.Errorf("bounded tick stamped the watermark: before %v, after %v", before, got)
	}

	ticks := chunkedDrain(h)
	if len(ticks) == 0 {
		t.Fatal("drain ran no further ticks")
	}
	if last := ticks[len(ticks)-1]; last.Bounded {
		t.Errorf("the tick that emptied the log still reports Bounded: %+v", last)
	}
	// One idle tick after the drain stamps the watermark the bounded passes
	// withheld (or the draining tick itself stamped it).
	_ = chunkedTick(h)
	if got := chunkedWatermark(h); !got.After(before) {
		t.Errorf("watermark after drain = %v, want after %v", got, before)
	}
	h.assertParity("chunked drain")
}

// TestSupplyChainImpactWinnersKeyedChunkedRowBudgetLive pins the candidate-row
// side of the bound: a hot triple fetches its whole candidate set per dirty
// key, so bounding triples alone cannot bound the heap fetches of a tick whose
// triples are hot. A row budget below one hot triple still completes that
// triple (progress) and stops before the next.
func TestSupplyChainImpactWinnersKeyedChunkedRowBudgetLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 chunked row-budget proof")
	// Four hot triples, 20 keys each, in one scope: one triple's candidate
	// fetch is its 20 findings.
	h.seedScope("vuln-intel://9805-chunked/hot", "vuln-intel://9805-chunked/hot/g1")
	h.write("vuln-intel://9805-chunked/hot", "vuln-intel://9805-chunked/hot/g1", chunkedFindings("hot", 4, 20, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}
	before := chunkedWatermark(h)
	h.write("vuln-intel://9805-chunked/hot", "vuln-intel://9805-chunked/hot/g1", chunkedFindings("hot", 4, 20, 1000)...)

	h.store.ChunkMaxTriples = 100
	h.store.ChunkMaxCandidateRows = 10
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("row-budget tick = %+v, want Bounded", first)
	}
	if first.Triples != 1 {
		t.Errorf("row-budget tick probed %d triples, want exactly the one triple that crosses the budget", first.Triples)
	}
	if first.CandidateRows < 20 {
		t.Errorf("row-budget tick fetched %d candidate rows, want the whole first hot triple (20)", first.CandidateRows)
	}
	if got := chunkedWatermark(h); !got.Equal(before) {
		t.Errorf("row-budget tick stamped the watermark: before %v, after %v", before, got)
	}
	chunkedDrain(h)
	h.assertParity("row-budget drain")
}

// TestSupplyChainImpactWinnersKeyedChunkedScopeSplitLive pins the truncation
// order a candidate cap alone misses: one scope dirty row expands to every
// triple of a generation. A bounded tick must apply a prefix of that expansion
// and keep the rest queued (as fact rows), never drop it with the scope row.
func TestSupplyChainImpactWinnersKeyedChunkedScopeSplitLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 chunked scope-split proof")
	scope := "vuln-intel://9805-chunked/act"
	h.seedScope(scope, scope+"/g1")
	h.write(scope, scope+"/g1", chunkedFindings("act", 8, 1, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}
	before := chunkedWatermark(h)

	h.addGeneration(scope, scope+"/g2", "pending")
	h.write(scope, scope+"/g2", chunkedFindings("act", 8, 1, 1000)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log WHERE kind = 'fact'`)
	h.activate(scope, scope+"/g1", scope+"/g2")
	if depth := h.dirtyDepth(); depth == 0 {
		t.Fatal("activation left the dirty log empty")
	}

	h.store.ChunkMaxTriples = 3
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("scope-split tick = %+v, want Bounded", first)
	}
	if first.Triples > 3 {
		t.Errorf("scope-split tick probed %d triples, want at most 3", first.Triples)
	}
	if depth := h.dirtyDepth(); depth == 0 {
		t.Error("bounded scope tick drained the log: the unapplied part of the scope expansion was lost")
	}
	if got := chunkedWatermark(h); !got.Equal(before) {
		t.Errorf("scope-split tick stamped the watermark: before %v, after %v", before, got)
	}
	chunkedDrain(h)
	h.assertParity("scope-split drain")
}
