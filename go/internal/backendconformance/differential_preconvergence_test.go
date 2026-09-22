// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import "testing"

// TestCompareRecordingsPreConvergenceExecutionPollutesDigestSet proves the #6923 mechanism:
// leg A runs the sink probe twice (pre-convergence 12-row answer, then the
// converged 14-row answer) while leg B runs it once (14 rows). Both legs
// converge on the same final truth, yet CompareRecordings must report a
// results-kind divergence because the digest SETS differ.
func TestCompareRecordingsPreConvergenceExecutionPollutesDigestSet(t *testing.T) {
	fp := DifferentialFingerprint{
		Statement:  "UNWIND $pairs AS pair MATCH (fn:Function {uid: pair.function_uid}) RETURN 1",
		Parameters: `{"pairs":[{"function_uid":"content-entity:e_6e935a20d390"}]}`,
	}
	legA := []DifferentialRecord{
		{Fingerprint: fp, Backend: "nornicdb", RowCount: 12, Digest: "pre-convergence-digest"},
		{Fingerprint: fp, Backend: "nornicdb", RowCount: 14, Digest: "converged-digest"},
	}
	legB := []DifferentialRecord{
		{Fingerprint: fp, Backend: "neo4j", RowCount: 14, Digest: "converged-digest"},
	}
	diffs := CompareRecordings(legA, legB)
	if len(diffs) != 1 {
		t.Fatalf("diffs = %v, want exactly one divergence", diffs)
	}
	if diffs[0].Kind != DivergenceResults {
		t.Fatalf("kind = %q, want %q", diffs[0].Kind, DivergenceResults)
	}
	t.Logf("PROVEN: pre-convergence execution pollutes the digest set -> results-kind wobble: %s", diffs[0].Detail)
}
