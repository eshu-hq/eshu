// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import "testing"

// TestCompareRecordingsFailOpenResolveAloneCannotGreen proves option A (fail-open emit gate)
// is insufficient for the oracle: the stale leg gains a converged execution
// but keeps its pre-convergence one, so the digest SETS still differ.
func TestCompareRecordingsFailOpenResolveAloneCannotGreen(t *testing.T) {
	fp := DifferentialFingerprint{
		Statement:  "UNWIND $pairs AS pair MATCH (fn:Function {uid: pair.function_uid}) RETURN 1",
		Parameters: `{"pairs":[{"function_uid":"content-entity:e_6e935a20d390"}]}`,
	}
	// Leg B with fail-open refresh: stale 12-row read + converged 14-row re-read.
	legB := []DifferentialRecord{
		{Fingerprint: fp, Backend: "neo4j", RowCount: 12, Digest: "pre-convergence-digest"},
		{Fingerprint: fp, Backend: "neo4j", RowCount: 14, Digest: "converged-digest"},
	}
	legA := []DifferentialRecord{
		{Fingerprint: fp, Backend: "nornicdb", RowCount: 14, Digest: "converged-digest"},
	}
	diffs := CompareRecordings(legB, legA)
	if len(diffs) != 1 || diffs[0].Kind != DivergenceResults {
		t.Fatalf("diffs = %v, want one results-kind divergence (fail-open is oracle-insufficient)", diffs)
	}
	t.Logf("PROVEN: fail-open alone leaves %s", diffs[0].Detail)
}

// TestCompareRecordingsLastWinsWouldMaskFlaps proves option D (compare-side last-wins)
// hides genuine within-leg backend nondeterminism: leg A flaps between two
// answers while leg B is stable, yet last-wins reports clean.
func TestCompareRecordingsLastWinsWouldMaskFlaps(t *testing.T) {
	fp := DifferentialFingerprint{
		Statement:  "MATCH (n:Function) RETURN n.name ORDER BY n.name",
		Parameters: `{}`,
	}
	legA := []DifferentialRecord{
		{Fingerprint: fp, Backend: "nornicdb", RowCount: 2, Digest: "answer-x"},
		{Fingerprint: fp, Backend: "nornicdb", RowCount: 2, Digest: "answer-y"},
	}
	legB := []DifferentialRecord{
		{Fingerprint: fp, Backend: "neo4j", RowCount: 2, Digest: "answer-x"},
	}
	// Current set semantics catch the flap...
	if diffs := CompareRecordings(legA, legB); len(diffs) != 1 || diffs[0].Kind != DivergenceResults {
		t.Fatalf("set compare diffs = %v, want one results-kind (sanity)", diffs)
	}
	// ...while last-execution-wins would compare {answer-y} vs {answer-x}
	// only when the flap lands on y, and report CLEAN when it lands on x.
	lastWins := func(recs []DifferentialRecord) string { return recs[len(recs)-1].Digest }
	stableCase := []DifferentialRecord{
		{Fingerprint: fp, Backend: "nornicdb", RowCount: 2, Digest: "answer-y"},
		{Fingerprint: fp, Backend: "nornicdb", RowCount: 2, Digest: "answer-x"},
	}
	if lastWins(stableCase) != lastWins(legB) {
		t.Fatalf("shim setup broken")
	}
	t.Logf("PROVEN: last-wins reports clean whenever the flap lands on x — flap detection becomes luck")
}
