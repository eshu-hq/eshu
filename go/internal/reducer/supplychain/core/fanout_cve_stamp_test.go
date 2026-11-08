// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// TestFanOutStampCarriesChangedCVEIDs is the #9740 regression: a reopen stamps
// the changed CVE ids onto fanout_origin, sorted, de-duplicated, and capped,
// so a flagged pass can compare what it derived with the change that flagged
// it. It fails until the stamp carries the set.
func TestFanOutStampCarriesChangedCVEIDs(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"holder-a"}, payload: map[string]any{}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{
		ChangedCVEIDs: []string{"CVE-2026-9002", "CVE-2026-9001", "CVE-2026-9002"},
	}}
	handler := fanOutHandler(port, writer, nil)
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, port.payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.origins) != 1 {
		t.Fatalf("origins = %d, want 1 reopened holder", len(port.origins))
	}
	want := []string{"CVE-2026-9001", "CVE-2026-9002"}
	if !reflect.DeepEqual(port.origins[0].CVEIDs, want) {
		t.Fatalf("stamp CVE ids = %v, want sorted de-duplicated %v", port.origins[0].CVEIDs, want)
	}
}

// TestFanOutStampCapsCVEIDsWithSentinel pins the cap: above it the stamp
// carries the sentinel alone, because an unbounded id list would bloat every
// reopened item's payload.
func TestFanOutStampCapsCVEIDsWithSentinel(t *testing.T) {
	t.Parallel()

	many := make([]string, 0, FanOutOriginCVEIDCap+10)
	for i := 0; i < FanOutOriginCVEIDCap+10; i++ {
		many = append(many, fmt.Sprintf("CVE-2026-%04d", i))
	}
	got := CapFanOutStampCVEIDs(many)
	if len(got) != 1 || got[0] != FanOutStampSentinel {
		t.Fatalf("capped stamp = %v, want [%q]", got, FanOutStampSentinel)
	}
	few := []string{"CVE-2026-9002", "CVE-2026-9001", "CVE-2026-9001"}
	if got := CapFanOutStampCVEIDs(few); !slices.Equal(got, []string{"CVE-2026-9001", "CVE-2026-9002"}) {
		t.Fatalf("capped stamp = %v, want sorted de-duplicated ids", got)
	}
	if got := CapFanOutStampCVEIDs(nil); len(got) != 0 {
		t.Fatalf("capped stamp = %v, want empty for no changes", got)
	}
}

// TestDisjointStampedPassClearsItsReplayFlag is the #9740 disjoint narrowing:
// a stamped one-hop pass whose derived rows (written and retracted copies)
// share no CVE with the stamp clears cross_scope_replay_required under a
// stamp-equality fence, so its ack does not rerun it.
func TestDisjointStampedPassClearsItsReplayFlag(t *testing.T) {
	t.Parallel()

	stamp := map[string]any{
		"scope_id": "scope:src", "generation_id": "gen-src", "intent_id": "intent-src",
		"cves": []any{"CVE-2026-9001"},
	}
	payload := map[string]any{PayloadKeyFanOutOrigin: stamp}
	port := &stubFanOut{payload: payload}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{
		DerivedCVEIDs:    []string{"CVE-2026-9002"},
		EvidenceComplete: true,
	}}
	handler := fanOutHandler(port, writer, nil)
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.clearCalls) != 1 {
		t.Fatalf("clear calls = %d, want 1 conditional clear for a disjoint stamp", len(port.clearCalls))
	}
	if !reflect.DeepEqual(port.clearCalls[0], stamp) {
		t.Fatalf("clear fence = %v, want the seen stamp %v", port.clearCalls[0], stamp)
	}
}

// TestOverlappingStampedPassKeepsItsReplayFlag pins the other half: a stamped
// pass that derived a stamped CVE keeps the flag, so its ack reruns it once
// under post-change evidence.
func TestOverlappingStampedPassKeepsItsReplayFlag(t *testing.T) {
	t.Parallel()

	stamp := map[string]any{
		"scope_id": "scope:src", "generation_id": "gen-src", "intent_id": "intent-src",
		"cves": []any{"CVE-2026-9001"},
	}
	payload := map[string]any{PayloadKeyFanOutOrigin: stamp}
	port := &stubFanOut{payload: payload}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{
		DerivedCVEIDs:    []string{"CVE-2026-9001", "CVE-2026-9002"},
		EvidenceComplete: true,
	}}
	handler := fanOutHandler(port, writer, nil)
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.clearCalls) != 0 {
		t.Fatalf("clear calls = %d, want none: an overlapping pass must rerun", len(port.clearCalls))
	}
}

// TestPartialStampedPassKeepsItsReplayFlag pins the truncated-load leg: a
// pass whose evidence load stopped short may have missed the flagged change,
// so even a disjoint derived set does not clear the flag.
func TestPartialStampedPassKeepsItsReplayFlag(t *testing.T) {
	t.Parallel()

	stamp := map[string]any{
		"scope_id": "scope:src", "generation_id": "gen-src", "intent_id": "intent-src",
		"cves": []any{"CVE-2026-9001"},
	}
	payload := map[string]any{PayloadKeyFanOutOrigin: stamp}
	port := &stubFanOut{payload: payload}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{
		DerivedCVEIDs: []string{"CVE-2026-9002"},
	}}
	handler := fanOutHandler(port, writer, nil)
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.clearCalls) != 0 {
		t.Fatalf("clear calls = %d, want none: a partial pass must rerun", len(port.clearCalls))
	}
}

// TestSentinelStampedPassKeepsItsReplayFlag pins the fail-closed leg: the
// sentinel names an unbounded change no pass may prove disjoint from.
func TestSentinelStampedPassKeepsItsReplayFlag(t *testing.T) {
	t.Parallel()

	payload := map[string]any{PayloadKeyFanOutOrigin: map[string]any{
		"scope_id": "scope:src", "generation_id": "gen-src", "intent_id": "intent-src",
		"cves": []any{FanOutStampSentinel},
	}}
	port := &stubFanOut{payload: payload}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{EvidenceComplete: true}}
	handler := fanOutHandler(port, writer, nil)
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.clearCalls) != 0 {
		t.Fatalf("clear calls = %d, want none: the sentinel always reruns", len(port.clearCalls))
	}
}
