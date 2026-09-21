// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import "testing"

// TestCompareRecordingsLabelsDivergenceKinds pins the kind decomposition of
// issue #6782 slice 3: every divergence carries the check that caught it, so
// the allowlist can excuse scheduling noise (poll iteration counts) without
// ever excusing a result disagreement.
func TestCompareRecordingsLabelsDivergenceKinds(t *testing.T) {
	t.Parallel()
	results := DifferentialFingerprint{Statement: "MATCH (n) RETURN n", Parameters: `{}`}
	polls := DifferentialFingerprint{Statement: "MATCH (n) RETURN n LIMIT $limit", Parameters: `{"limit":11}`}
	write := DifferentialFingerprint{Statement: "MERGE (n:File {path: $path})", Parameters: `{"path":"a"}`}
	flaky := DifferentialFingerprint{Statement: "MATCH (m) RETURN m", Parameters: `{}`}
	a := []DifferentialRecord{
		{Backend: "nornicdb", Fingerprint: results, RowCount: 1, Digest: "d-nornic"},
		{Backend: "nornicdb", Fingerprint: polls, RowCount: 1, Digest: "d-poll"},
		{Backend: "nornicdb", Fingerprint: polls, RowCount: 1, Digest: "d-poll"},
		{Backend: "nornicdb", Fingerprint: write},
		{Backend: "nornicdb", Fingerprint: flaky, RowCount: 2, Digest: ""},
		{Backend: "nornicdb", Fingerprint: flaky, RowCount: 2, Digest: ""},
	}
	b := []DifferentialRecord{
		{Backend: "neo4j", Fingerprint: results, RowCount: 1, Digest: "d-neo4j"},
		{Backend: "neo4j", Fingerprint: polls, RowCount: 1, Digest: "d-poll"},
		{Backend: "neo4j", Fingerprint: polls, RowCount: 1, Digest: "d-poll"},
		{Backend: "neo4j", Fingerprint: polls, RowCount: 1, Digest: "d-poll"},
		{Backend: "neo4j", Fingerprint: flaky, RowCount: 3, Digest: ""},
		{Backend: "neo4j", Fingerprint: flaky, RowCount: 3, Digest: ""},
	}
	diffs := CompareRecordings(a, b)
	kindOf := make(map[string]string)
	for _, diff := range diffs {
		kindOf[diff.Fingerprint.Statement+diff.Fingerprint.Parameters] = diff.Kind
	}
	want := map[string]string{
		results.Statement + results.Parameters: "results",
		polls.Statement + polls.Parameters:     "executions",
		flaky.Statement + flaky.Parameters:     "rowcount",
	}
	if len(diffs) != len(want)+1 {
		t.Fatalf("differences = %v, want %d named kinds plus the one-sided write", diffs, len(want))
	}
	for key, kind := range want {
		if kindOf[key] != kind {
			t.Errorf("kind for %q = %q, want %q (all: %v)", key, kindOf[key], kind, diffs)
		}
	}
	var missing bool
	for _, diff := range diffs {
		if diff.Fingerprint == write && diff.Kind == "missing" {
			missing = true
		}
	}
	if !missing {
		t.Errorf("no missing-kind difference for the one-sided write (all: %v)", diffs)
	}
}

// TestCompareRecordingsKeepsFailuresKind pins that a failed-execution count
// mismatch with agreeing digests reports as failures, not executions: a
// backend that errors where the other succeeds is never scheduling noise.
func TestCompareRecordingsKeepsFailuresKind(t *testing.T) {
	t.Parallel()
	fp := DifferentialFingerprint{Statement: "MERGE (n:File {path: $path})", Parameters: `{"path":"a"}`}
	a := []DifferentialRecord{{Backend: "nornicdb", Fingerprint: fp}}
	b := []DifferentialRecord{{Backend: "neo4j", Fingerprint: fp, Failed: true}}
	diffs := CompareRecordings(a, b)
	if len(diffs) != 1 {
		t.Fatalf("differences = %v, want the one-sided failure", diffs)
	}
	if diffs[0].Kind != "failures" {
		t.Fatalf("kind = %q, want failures", diffs[0].Kind)
	}
}

// TestQuorumIntersectionKeepsReproducedDivergences pins the #6782 multi-leg
// quorum: only a divergence present in both leg pairings — same fingerprint
// and same kind — fails the gate. Pairing-local noise stays visible in the
// report but never reds the gate on its own.
func TestQuorumIntersectionKeepsReproducedDivergences(t *testing.T) {
	t.Parallel()
	systematic := DifferentialFingerprint{Statement: "MATCH (n) RETURN n", Parameters: `{}`}
	noiseA := DifferentialFingerprint{Statement: "MATCH (m) RETURN m", Parameters: `{}`}
	noiseB := DifferentialFingerprint{Statement: "MATCH (k) RETURN k", Parameters: `{}`}
	kindFlip := DifferentialFingerprint{Statement: "MATCH (j) RETURN j", Parameters: `{}`}
	first := []DifferentialDifference{
		{Fingerprint: systematic, Kind: "results", Detail: "row digest differs (nornicdb=14 rows, neo4j=12 rows)"},
		{Fingerprint: noiseA, Kind: "executions", Detail: "execution count differs with agreeing results (nornicdb=7, neo4j=14)"},
		{Fingerprint: kindFlip, Kind: "results", Detail: "row digest differs (nornicdb=58 rows, neo4j=59 rows)"},
	}
	second := []DifferentialDifference{
		{Fingerprint: systematic, Kind: "results", Detail: "row digest differs (nornicdb=14 rows, neo4j=12 rows)"},
		{Fingerprint: noiseB, Kind: "failures", Detail: "failed executions differ (nornicdb=1, neo4j=0)"},
		{Fingerprint: kindFlip, Kind: "executions", Detail: "execution count differs with agreeing results (nornicdb=7, neo4j=7)"},
	}
	kept := QuorumIntersection(first, second)
	if len(kept) != 1 {
		t.Fatalf("quorum kept %d divergences (%v), want only the reproduced systematic one", len(kept), kept)
	}
	if kept[0].Fingerprint != systematic || kept[0].Kind != "results" {
		t.Fatalf("quorum kept %+v, want the systematic results divergence", kept[0])
	}
}

// TestQuorumIntersectionDropsDisjointPairings pins the empty case: two red
// pairings with nothing in common still pass quorum — the gate stays green
// on leg-local noise and says so in the report.
func TestQuorumIntersectionDropsDisjointPairings(t *testing.T) {
	t.Parallel()
	first := []DifferentialDifference{
		{Fingerprint: DifferentialFingerprint{Statement: "MATCH (a) RETURN a"}, Kind: "executions"},
	}
	second := []DifferentialDifference{
		{Fingerprint: DifferentialFingerprint{Statement: "MATCH (b) RETURN b"}, Kind: "executions"},
	}
	if kept := QuorumIntersection(first, second); len(kept) != 0 {
		t.Fatalf("quorum kept %v for disjoint pairings, want none", kept)
	}
	if kept := QuorumIntersection(first, nil); len(kept) != 0 {
		t.Fatalf("quorum kept %v against an empty pairing, want none", kept)
	}
}
