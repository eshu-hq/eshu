// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"
)

// TestTaintEvidenceCountedStreamedAndFreshness proves that, when taint evidence
// is present, (1) it is emitted as a code_taint_evidence fact, (2) it is counted
// in FactCount so the streamed-fact parity holds, and (3) it changes the snapshot
// freshness hint so an already-fresh repo is not skipped on the first dataflow
// run.
func TestTaintEvidenceCountedStreamedAndFreshness(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 17, 0, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)

	base := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	withEvidence := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	withEvidence.TaintEvidence = []TaintEvidenceSnapshot{{
		FunctionUID: "func-1", RelativePath: "main.go", FunctionName: "handle",
		Language: "go", Kind: "TAINTED", SinkKind: "sql", SourceKind: "http_request",
		Binding: "q", SourceLine: 4, SinkLine: 5, Confidence: 0.8,
	}}

	baseCollected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, base, false)
	baseFacts := drainFactChannel(baseCollected.Facts)

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, withEvidence, false)
	envelopes := drainFactChannel(collected.Facts)

	// Parity: FactCount accounts for the extra taint evidence fact.
	if got, want := len(envelopes), collected.FactCount; got != want {
		t.Fatalf("streamed facts = %d, FactCount = %d (taint evidence not counted)", got, want)
	}
	// Exactly one extra fact relative to the no-evidence snapshot.
	if got := len(envelopes) - len(baseFacts); got != 1 {
		t.Fatalf("taint evidence added %d facts, want 1", got)
	}
	// The evidence is emitted as a code_taint_evidence fact.
	found := false
	for _, e := range envelopes {
		if e.FactKind == "code_taint_evidence" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no code_taint_evidence fact emitted; got %d facts", len(envelopes))
	}
	// The freshness hint changes when taint evidence is present.
	if baseCollected.Generation.FreshnessHint == collected.Generation.FreshnessHint {
		t.Fatalf("freshness hint unchanged despite taint evidence; an already-fresh repo would skip emission")
	}
}
