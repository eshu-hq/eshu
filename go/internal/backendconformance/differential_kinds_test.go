// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"slices"
	"strings"
	"testing"
)

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

// TestSplitAdvisorySeparatesExecutions pins the permanent disposition of the
// executions kind (#6782): agreeing result sets with different execution
// counts are scheduling noise by the classifier's own definition, so they
// partition into the advisory slice and never into the gate-failing one.
// Every other kind stays required, and both slices keep input order.
func TestSplitAdvisorySeparatesExecutions(t *testing.T) {
	t.Parallel()
	fp := func(s string) DifferentialFingerprint { return DifferentialFingerprint{Statement: s, Parameters: `{}`} }
	diffs := []DifferentialDifference{
		{Fingerprint: fp("MATCH (a) RETURN a"), Kind: DivergenceResults},
		{Fingerprint: fp("MATCH (b) RETURN b"), Kind: DivergenceExecutions},
		{Fingerprint: fp("MATCH (c) RETURN c"), Kind: DivergenceMissing},
		{Fingerprint: fp("MATCH (d) RETURN d"), Kind: DivergenceExecutions},
		{Fingerprint: fp("MATCH (e) RETURN e"), Kind: DivergenceFailures},
		{Fingerprint: fp("MATCH (f) RETURN f"), Kind: DivergenceRowCount},
	}
	required, advisory := SplitAdvisory(diffs)
	wantRequired := []string{"MATCH (a) RETURN a", "MATCH (c) RETURN c", "MATCH (e) RETURN e", "MATCH (f) RETURN f"}
	wantAdvisory := []string{"MATCH (b) RETURN b", "MATCH (d) RETURN d"}
	got := func(ds []DifferentialDifference) []string {
		out := make([]string, 0, len(ds))
		for _, d := range ds {
			out = append(out, d.Fingerprint.Statement)
		}
		return out
	}
	if g := got(required); !slices.Equal(g, wantRequired) {
		t.Fatalf("required = %v, want %v", g, wantRequired)
	}
	if g := got(advisory); !slices.Equal(g, wantAdvisory) {
		t.Fatalf("advisory = %v, want %v", g, wantAdvisory)
	}
	for _, kind := range []string{DivergenceMissing, DivergenceResults, DivergenceFailures, DivergenceRowCount} {
		if AdvisoryKind(kind) {
			t.Fatalf("AdvisoryKind(%q) = true, want false: only executions is advisory", kind)
		}
	}
	if !AdvisoryKind(DivergenceExecutions) {
		t.Fatal("AdvisoryKind(executions) = false, want true")
	}
	if r, a := SplitAdvisory(nil); r != nil || a != nil {
		t.Fatalf("SplitAdvisory(nil) = %v, %v; want nil, nil", r, a)
	}
}

// TestTopAdvisoryStatementReportsOrdersByCountDescending pins the ranking
// order for #6941: the advisory ceiling names its top offenders, so a
// statement reproducing more often must sort first.
func TestTopAdvisoryStatementReportsOrdersByCountDescending(t *testing.T) {
	t.Parallel()
	fp := func(s string) DifferentialFingerprint { return DifferentialFingerprint{Statement: s, Parameters: `{}`} }
	diffs := []DifferentialDifference{
		{Fingerprint: fp("MATCH (a) RETURN a")},
		{Fingerprint: fp("MATCH (b) RETURN b")},
		{Fingerprint: fp("MATCH (b) RETURN b")},
		{Fingerprint: fp("MATCH (b) RETURN b")},
		{Fingerprint: fp("MATCH (c) RETURN c")},
		{Fingerprint: fp("MATCH (c) RETURN c")},
	}
	got := TopAdvisoryStatementReports(diffs, 3, 120)
	want := []string{
		"MATCH (b) RETURN b (3)",
		"MATCH (c) RETURN c (2)",
		"MATCH (a) RETURN a (1)",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("TopAdvisoryStatementReports = %v, want %v", got, want)
	}
}

// TestTopAdvisoryStatementReportsTieBreaksByStatementText pins the
// determinism requirement: equal counts must not depend on map iteration
// order, so the tie breaks on the statement text ascending.
func TestTopAdvisoryStatementReportsTieBreaksByStatementText(t *testing.T) {
	t.Parallel()
	fp := func(s string) DifferentialFingerprint { return DifferentialFingerprint{Statement: s, Parameters: `{}`} }
	diffs := []DifferentialDifference{
		{Fingerprint: fp("MATCH (z) RETURN z")},
		{Fingerprint: fp("MATCH (a) RETURN a")},
		{Fingerprint: fp("MATCH (m) RETURN m")},
	}
	got := TopAdvisoryStatementReports(diffs, 10, 120)
	want := []string{
		"MATCH (a) RETURN a (1)",
		"MATCH (m) RETURN m (1)",
		"MATCH (z) RETURN z (1)",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("TopAdvisoryStatementReports = %v, want %v", got, want)
	}
}

// TestTopAdvisoryStatementReportsTruncatesLongStatements pins the 120-rune
// bound (#6941): a long Cypher statement must not dominate the one-line gate
// report, so it is cut with a "..." suffix while the count stays intact.
func TestTopAdvisoryStatementReportsTruncatesLongStatements(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 10)
	fp := DifferentialFingerprint{Statement: long, Parameters: `{}`}
	diffs := []DifferentialDifference{{Fingerprint: fp}}
	got := TopAdvisoryStatementReports(diffs, 3, 4)
	want := []string{"xxxx... (1)"}
	if !slices.Equal(got, want) {
		t.Fatalf("TopAdvisoryStatementReports = %v, want %v", got, want)
	}
	// A statement at or under the bound is never marked truncated.
	short := DifferentialFingerprint{Statement: "MATCH (n) RETURN n", Parameters: `{}`}
	got = TopAdvisoryStatementReports([]DifferentialDifference{{Fingerprint: short}}, 3, 120)
	want = []string{"MATCH (n) RETURN n (1)"}
	if !slices.Equal(got, want) {
		t.Fatalf("TopAdvisoryStatementReports = %v, want %v", got, want)
	}
}

// TestTopAdvisoryStatementReportsEmptyInput pins nil-in/nil-out: an empty
// advisory slice must not synthesize a phantom report line.
func TestTopAdvisoryStatementReportsEmptyInput(t *testing.T) {
	t.Parallel()
	if got := TopAdvisoryStatementReports(nil, 3, 120); got != nil {
		t.Fatalf("TopAdvisoryStatementReports(nil) = %v, want nil", got)
	}
	if got := TopAdvisoryStatementReports([]DifferentialDifference{}, 3, 120); got != nil {
		t.Fatalf("TopAdvisoryStatementReports(empty) = %v, want nil", got)
	}
}
