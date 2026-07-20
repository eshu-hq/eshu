// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestDataflowScannedMarkerEmittedWithoutFindings proves that when the value-flow
// gate ran (DataflowScanned) but produced no taint or interproc findings, a
// single code_dataflow_scanned marker fact is still emitted and counted, so the
// reducer can reconcile and retract stale evidence.
func TestDataflowScannedMarkerEmittedWithoutFindings(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)

	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	snapshot.DataflowScanned = true // gate on, zero findings

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	if got, want := len(envelopes), collected.FactCount(); got != want {
		t.Fatalf("streamed facts = %d, FactCount = %d (marker not counted)", got, want)
	}
	markers := 0
	for _, e := range envelopes {
		if e.FactKind == facts.CodeDataflowScannedFactKind {
			markers++
		}
	}
	if markers != 1 {
		t.Fatalf("code_dataflow_scanned markers = %d, want exactly 1", markers)
	}
}

// TestDataflowScannedMarkerAbsentWhenGateOff proves the marker is not emitted when
// the gate was off, preserving the byte-identical-when-off guarantee.
func TestDataflowScannedMarkerAbsentWhenGateOff(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)

	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	// DataflowScanned defaults to false.

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	if got, want := len(envelopes), collected.FactCount(); got != want {
		t.Fatalf("streamed facts = %d, FactCount = %d", got, want)
	}
	for _, e := range envelopes {
		if e.FactKind == facts.CodeDataflowScannedFactKind {
			t.Fatalf("marker emitted with gate off: %+v", e)
		}
	}
}

// TestDataflowScannedMarkerAbsentOnDelta proves the marker is not emitted on a
// delta generation even when the gate ran: a delta carries only changed-file
// findings, and the evidence reducers retract the whole scope before writing, so
// a marker-triggered delta would wipe evidence for unchanged files (#2927 review).
func TestDataflowScannedMarkerAbsentOnDelta(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)

	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	snapshot.DataflowScanned = true
	snapshot.Delta = true // partial generation

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	if got, want := len(envelopes), collected.FactCount(); got != want {
		t.Fatalf("streamed facts = %d, FactCount = %d", got, want)
	}
	for _, e := range envelopes {
		if e.FactKind == facts.CodeDataflowScannedFactKind {
			t.Fatalf("marker must not be emitted on a delta generation: %+v", e)
		}
	}
}

// TestDataflowScannedMarkerStableKey proves the marker's stable fact key is
// repo-scoped and idempotent across re-emission of the same generation.
func TestDataflowScannedMarkerStableKey(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	a := dataflowScannedFactEnvelope("/repo", "repo-1", "scope-1", "gen-1", at)
	b := dataflowScannedFactEnvelope("/repo", "repo-1", "scope-1", "gen-1", at)
	if a.FactKind != facts.CodeDataflowScannedFactKind {
		t.Fatalf("FactKind = %q, want %q", a.FactKind, facts.CodeDataflowScannedFactKind)
	}
	if a.StableFactKey != "code_dataflow_scanned:repo-1" {
		t.Fatalf("StableFactKey = %q", a.StableFactKey)
	}
	if a.StableFactKey != b.StableFactKey || a.FactID != b.FactID {
		t.Fatalf("marker not stable across re-emission: %q/%q vs %q/%q", a.StableFactKey, a.FactID, b.StableFactKey, b.FactID)
	}
}
