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
