// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// Chunked-consume proofs for #9805 (arbiter ruling: chunk-build-sufficient).
// One keyed transaction bounds the post-expansion identifiers it probes: a
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

func TestSupplyChainImpactWinnersResweepKeyedChunkedConsumeLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 chunked consume proof")
	h.seedScope("vuln-intel://9805-chunked/s1", "vuln-intel://9805-chunked/s1/g1")

	h.write("vuln-intel://9805-chunked/s1", "vuln-intel://9805-chunked/s1/g1", chunkedFindings("chunk", 12, 2, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}
	before := chunkedWatermark(h)

	// Rewrite every finding with a new priority: 12 triples of 2 keys go
	// dirty at once (24 post-expansion identifiers), a backlog far past one
	// bounded transaction.
	h.write("vuln-intel://9805-chunked/s1", "vuln-intel://9805-chunked/s1/g1", chunkedFindings("chunk", 12, 2, 1000)...)
	if depth := h.dirtyDepth(); depth == 0 {
		t.Fatal("rewrite left the dirty log empty")
	}

	h.store.ChunkMaxIdentifiers = 10
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("first tick over 24 dirty identifiers with a 10-identifier bound = %+v, want Bounded", first)
	}
	if first.ChunkIdentifiers != 10 {
		t.Errorf("first tick probed %d identifiers, want exactly the 10-identifier chunk", first.ChunkIdentifiers)
	}
	if first.ChunkKeptRows < 10 {
		t.Errorf("first tick kept %d facts for 10 identifiers, want at least one kept fact per identifier", first.ChunkKeptRows)
	}
	if first.Keys > 10 {
		t.Errorf("first tick recomputed %d keys, want at most the keys of the 10-identifier chunk", first.Keys)
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

// TestSupplyChainImpactWinnersKeyedChunkedSingleIdentLive pins the progress
// half of the identifier bound: a bound of one still completes that one
// identifier per tick, so even an all-hot backlog drains instead of stalling.
func TestSupplyChainImpactWinnersResweepKeyedChunkedSingleIdentLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 chunked single-identifier proof")
	// Four triples, 5 keys each: 20 post-expansion identifiers in one scope.
	h.seedScope("vuln-intel://9805-chunked/hot", "vuln-intel://9805-chunked/hot/g1")
	h.write("vuln-intel://9805-chunked/hot", "vuln-intel://9805-chunked/hot/g1", chunkedFindings("hot", 4, 5, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}
	before := chunkedWatermark(h)
	h.write("vuln-intel://9805-chunked/hot", "vuln-intel://9805-chunked/hot/g1", chunkedFindings("hot", 4, 5, 1000)...)

	h.store.ChunkMaxIdentifiers = 1
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("single-identifier tick = %+v, want Bounded", first)
	}
	if first.ChunkIdentifiers != 1 {
		t.Errorf("single-identifier tick probed %d identifiers, want exactly the one the bound admits", first.ChunkIdentifiers)
	}
	if got := chunkedWatermark(h); !got.Equal(before) {
		t.Errorf("single-identifier tick stamped the watermark: before %v, after %v", before, got)
	}
	ticks := chunkedDrain(h)
	if len(ticks) == 0 {
		t.Fatal("drain ran no further ticks after the single-identifier first tick")
	}
	h.assertParity("single-identifier drain")
}

// TestSupplyChainImpactWinnersResweepKeyedChunkedAdmissionTruncateLive pins the
// dirty-row window bound: with more dirty rows queued than ChunkMaxDirtyRows
// admits, the tick applies the admitted window and reports Bounded through the
// remaining depth alone, without truncating the identifier chunk.
func TestSupplyChainImpactWinnersResweepKeyedChunkedAdmissionTruncateLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 chunked admission-window proof")
	// Eight findings, one key each: eight post-expansion identifiers in one
	// scope, each with its own fact dirty row.
	h.seedScope("vuln-intel://9805-chunked/adm", "vuln-intel://9805-chunked/adm/g1")
	h.write("vuln-intel://9805-chunked/adm", "vuln-intel://9805-chunked/adm/g1", chunkedFindings("adm", 8, 1, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}
	before := chunkedWatermark(h)
	h.write("vuln-intel://9805-chunked/adm", "vuln-intel://9805-chunked/adm/g1", chunkedFindings("adm", 8, 1, 1000)...)
	if depth := h.dirtyDepth(); depth != 8 {
		t.Fatalf("rewrite queued %d dirty rows, want the 8 fact rows", depth)
	}

	h.store.ChunkMaxDirtyRows = 3
	h.store.ChunkMaxIdentifiers = 1000
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("admission-truncated tick = %+v, want Bounded", first)
	}
	if first.ChunkIdentifiers != 3 {
		t.Errorf("admission-truncated tick probed %d identifiers, want the 3 admitted", first.ChunkIdentifiers)
	}
	if first.Remaining != 5 {
		t.Errorf("admission-truncated tick left %d rows queued, want the 5 unadmitted", first.Remaining)
	}
	if depth := h.dirtyDepth(); depth != 5 {
		t.Errorf("dirty log depth after the admitted window applied = %d, want 5", depth)
	}
	if got := chunkedWatermark(h); !got.Equal(before) {
		t.Errorf("bounded tick stamped the watermark: before %v, after %v", before, got)
	}
	chunkedDrain(h)
	_ = chunkedTick(h)
	if got := chunkedWatermark(h); !got.After(before) {
		t.Errorf("watermark after drain = %v, want after %v", got, before)
	}
	h.assertParity("admission-window drain")
}

// TestSupplyChainImpactWinnersResweepKeyedChunkedZeroAppliedScopeLive pins the
// scope row a chunk skips entirely: when every identifier of a scope row's
// expansion sorts after the chunk prefix, the tick applies none of them, so
// the scope row stays queued (no remainder to convert, nothing to delete) and
// the next ticks resume from it instead of re-expanding the generation.
func TestSupplyChainImpactWinnersResweepKeyedChunkedZeroAppliedScopeLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 chunked zero-applied scope proof")
	scope := "vuln-intel://9805-chunked/mix"
	h.seedScope(scope, scope+"/g1")
	h.write(scope, scope+"/g1", chunkedFindings("aaa", 4, 1, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		t.Fatalf("initial full resweep: %v", err)
	}
	before := chunkedWatermark(h)

	h.addGeneration(scope, scope+"/g2", "pending")
	h.write(scope, scope+"/g2", chunkedFindings("zzz", 4, 1, 0)...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log WHERE kind = 'fact'`)
	h.activate(scope, scope+"/g1", scope+"/g2")
	// Four fact rows for the rewritten aaa findings sort before every zzz
	// identifier of the scope row's expansion.
	h.write(scope, scope+"/g1", chunkedFindings("aaa", 4, 1, 1000)...)
	// The activation queued one scope row per generation side (old and new),
	// plus the four fact rows for the rewritten aaa findings.
	if depth := h.dirtyDepth(); depth != 6 {
		t.Fatalf("queued %d dirty rows, want 4 fact rows plus 2 scope rows", depth)
	}

	h.store.ChunkMaxIdentifiers = 4
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("zero-applied-scope tick = %+v, want Bounded", first)
	}
	if first.ChunkIdentifiers != 4 {
		t.Errorf("zero-applied-scope tick probed %d identifiers, want the 4 aaa identifiers", first.ChunkIdentifiers)
	}
	// The g1 scope row's expansion (the aaa identifiers) was fully applied,
	// so it leaves with the fact rows; the g2 scope row's expansion (the zzz
	// identifiers, all sorting after the chunk prefix) saw zero applied
	// identifiers, so it stays queued instead of being dropped or converted.
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE kind = 'scope'`); n != 1 {
		t.Errorf("%d scope rows still queued, want the one whose expansion the chunk skipped", n)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE kind = 'scope' AND generation_id = $1`, scope+"/g2"); n != 1 {
		t.Errorf("%d g2 scope rows still queued, want the unapplied one", n)
	}
	if depth := h.dirtyDepth(); depth != 1 {
		t.Errorf("dirty log depth after the fact prefix applied = %d, want only the g2 scope row", depth)
	}
	if got := chunkedWatermark(h); !got.Equal(before) {
		t.Errorf("zero-applied-scope tick stamped the watermark: before %v, after %v", before, got)
	}
	chunkedDrain(h)
	_ = chunkedTick(h)
	if got := chunkedWatermark(h); !got.After(before) {
		t.Errorf("watermark after drain = %v, want after %v", got, before)
	}
	h.assertParity("zero-applied-scope drain")
}

// TestSupplyChainImpactWinnersResweepKeyedChunkedScopeSplitLive pins the truncation
// order a dirty-row cap alone misses: one scope dirty row expands to every
// identifier of a generation. A bounded tick must apply a prefix of that
// expansion and keep the rest queued (as fact rows), never drop it with the
// scope row.
func TestSupplyChainImpactWinnersResweepKeyedChunkedScopeSplitLive(t *testing.T) {
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

	h.store.ChunkMaxIdentifiers = 3
	first := chunkedTick(h)
	if !first.Bounded {
		t.Fatalf("scope-split tick = %+v, want Bounded", first)
	}
	if first.ChunkIdentifiers > 3 {
		t.Errorf("scope-split tick probed %d identifiers, want at most 3", first.ChunkIdentifiers)
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
