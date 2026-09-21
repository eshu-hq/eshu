// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"context"
	"errors"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestFingerprintStatementNormalizesWhitespace(t *testing.T) {
	t.Parallel()
	a, err := FingerprintStatement("MATCH (n:File)\n  WHERE n.path = $path\nRETURN n", map[string]any{"path": "a/b"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := FingerprintStatement("  MATCH   (n:File) WHERE n.path = $path RETURN n  ", map[string]any{"path": "a/b"})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("whitespace variants differ:\n%q\n%q", a, b)
	}
	c, err := FingerprintStatement("MATCH (n:File) WHERE n.path = $other RETURN n", map[string]any{"path": "a/b"})
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Fatal("different statement text shares a fingerprint")
	}
}

func TestFingerprintStatementParamsDistinguishValues(t *testing.T) {
	t.Parallel()
	a, err := FingerprintStatement("MATCH (n) WHERE n.x = $x RETURN n", map[string]any{"x": 1, "y": "s"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := FingerprintStatement("MATCH (n) WHERE n.x = $x RETURN n", map[string]any{"y": "s", "x": 1})
	if err != nil {
		t.Fatal(err)
	}
	if a != again {
		t.Fatal("same params in different map order differ")
	}
	b, err := FingerprintStatement("MATCH (n) WHERE n.x = $x RETURN n", map[string]any{"x": 2, "y": "s"})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("different param values share a fingerprint")
	}
}

// Diagnostic `_eshu_*` metadata keys never reach either backend's driver
// (SanitizeStatementParameters strips them on every executor path), so they
// carry no execution truth and must not split the fingerprint. A NornicDB
// run records sanitized params while a Neo4j run records the same statement
// pre-sanitize; without this the same logical write never pairs (#6782).
func TestFingerprintStatementStripsDiagnosticMetadata(t *testing.T) {
	t.Parallel()
	plain, err := FingerprintStatement("MERGE (r:Repository {id: $repo_id}) SET r.name = $name",
		map[string]any{"repo_id": "repository:r_1", "name": "acme/web"})
	if err != nil {
		t.Fatal(err)
	}
	tagged, err := FingerprintStatement("MERGE (r:Repository {id: $repo_id}) SET r.name = $name",
		map[string]any{"repo_id": "repository:r_1", "name": "acme/web", "_eshu_phase": "repository"})
	if err != nil {
		t.Fatal(err)
	}
	if plain != tagged {
		t.Fatal("diagnostic metadata splits the fingerprint")
	}
}

func TestDigestRowsIgnoresOrderWithoutOrderBy(t *testing.T) {
	t.Parallel()
	rows := []map[string]any{{"n": int64(1), "tags": []any{"a"}}, {"n": int64(2)}}
	shuffled := []map[string]any{{"n": 2}, {"n": 1, "tags": []string{"a"}}}
	a, err := DigestRows(rows, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DigestRows(shuffled, false)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("unordered digest distinguishes row order and driver int types")
	}
	if a == "" {
		t.Fatal("empty digest")
	}
}

func TestDigestRowsKeepsOrderWithOrderBy(t *testing.T) {
	t.Parallel()
	rows := []map[string]any{{"n": 1}, {"n": 2}}
	forward, err := DigestRows(rows, true)
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := DigestRows([]map[string]any{{"n": 2}, {"n": 1}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if forward == reversed {
		t.Fatal("ordered digest ignores row order, ORDER BY regressions would pass")
	}
	unorderedReversed, err := DigestRows([]map[string]any{{"n": 2}, {"n": 1}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if reversed == unorderedReversed {
		t.Fatal("ordered digest of unsorted rows matches the sorted digest, ORDER BY regressions would pass")
	}
}

func TestHasOrderByDetection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cypher string
		want   bool
	}{
		{"MATCH (n) RETURN n ORDER BY n.x", true},
		{"match (n) return n order by n.x", true},
		{"MATCH (n)\nRETURN n\nORDER\nBY n.x", true},
		{"MATCH (n) RETURN n", false},
		{"MATCH (n) WHERE n.note = 'ORDER BY please' RETURN n", false},
		{"MATCH (n:Reorder) RETURN n", false},
		{"// ORDER BY in a line comment\nMATCH (n) RETURN n", false},
		{"MATCH (n) RETURN n /* ORDER BY trailing block */", false},
		{"MATCH (n) WHERE n.note = \"ORDER BY please\" RETURN n", false},
		{`MATCH (n) WHERE n.a = 'it\'s ORDER BY x' RETURN n`, false},
		{"MATCH (`order by`) RETURN `order by`", false},
		{"// comment\nMATCH (n) RETURN n ORDER BY n.x", true},
		{"MATCH (n) RETURN n ORDER/*split*/BY n.x", true},
		{"MATCH (n) WHERE n.a = 'x' RETURN n ORDER BY n.a", true},
	} {
		if got := HasOrderBy(tc.cypher); got != tc.want {
			t.Errorf("HasOrderBy(%q) = %v, want %v", tc.cypher, got, tc.want)
		}
	}
}

// stubDifferentialGraphQuery is a fake GraphQuery backend for the recording
// decorator tests.
type stubDifferentialGraphQuery struct {
	rows []map[string]any
	err  error
}

func (s stubDifferentialGraphQuery) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return s.rows, s.err
}

func (s stubDifferentialGraphQuery) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(s.rows) == 0 {
		return nil, nil
	}
	return s.rows[0], nil
}

func TestRecordingGraphQueryCapturesReads(t *testing.T) {
	// t.Setenv forbids Parallel: the wrapper only records with the
	// ESHU_DIFFERENTIAL_CAPTURE opt-in set.
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	inner := stubDifferentialGraphQuery{rows: []map[string]any{{"n": 1}, {"n": 2}}}
	query := WrapGraphQuery(inner, recorder, "nornicdb")
	rows, err := query.Run(context.Background(), "MATCH (n) RETURN n", map[string]any{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("passthrough rows = %d, want 2", len(rows))
	}
	records := recorder.Records()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	rec := records[0]
	if rec.Backend != "nornicdb" || rec.RowCount != 2 || rec.Failed || rec.Digest == "" {
		t.Fatalf("record = %+v, want backend/rowcount/digest set and not failed", rec)
	}
	fp, err := FingerprintStatement("MATCH (n) RETURN n", map[string]any{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Fingerprint != fp {
		t.Fatalf("record fingerprint = %+v, want %+v", rec.Fingerprint, fp)
	}
}

func TestRecordingGraphQueryMarksFailures(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	query := WrapGraphQuery(stubDifferentialGraphQuery{err: errors.New("boom")}, recorder, "neo4j")
	if _, err := query.Run(context.Background(), "MATCH (n) RETURN n", nil); err == nil {
		t.Fatal("expected the inner error to propagate")
	}
	records := recorder.Records()
	if len(records) != 1 || !records[0].Failed {
		t.Fatalf("records = %+v, want one failed record", records)
	}
}

// stubDifferentialExecutor is a fake sourcecypher.Executor without group or
// probe support.
type stubDifferentialExecutor struct {
	executed []sourcecypher.Statement
	err      error
}

func (s *stubDifferentialExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	s.executed = append(s.executed, stmt)
	return s.err
}

// stubDifferentialGroupExecutor is the full-surface stub: grouped and phased
// writes plus probes, recording how many inner calls each fan-out costs.
type stubDifferentialGroupExecutor struct {
	stubDifferentialExecutor
	groupCalls int
	phaseCalls int
}

func (s *stubDifferentialGroupExecutor) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	s.groupCalls++
	s.executed = append(s.executed, stmts...)
	return s.err
}

func (s *stubDifferentialGroupExecutor) ExecutePhaseGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	s.phaseCalls++
	s.executed = append(s.executed, stmts...)
	return s.err
}

func (s *stubDifferentialGroupExecutor) ExecuteProbe(_ context.Context, stmt sourcecypher.Statement) (bool, error) {
	s.executed = append(s.executed, stmt)
	return true, s.err
}

// stubDifferentialGroupOnlyExecutor supports grouped writes but neither
// phased writes nor probes.
type stubDifferentialGroupOnlyExecutor struct {
	stubDifferentialExecutor
	groupCalls int
}

func (s *stubDifferentialGroupOnlyExecutor) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	s.groupCalls++
	s.executed = append(s.executed, stmts...)
	return s.err
}

func TestRecordingExecutorCapturesWrites(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	inner := &stubDifferentialExecutor{}
	exec := WrapExecutor(inner, recorder, "nornicdb")
	stmt := sourcecypher.Statement{Cypher: "MERGE (n:File {path: $path})", Parameters: map[string]any{"path": "a"}}
	if err := exec.Execute(context.Background(), stmt); err != nil {
		t.Fatal(err)
	}
	if len(inner.executed) != 1 {
		t.Fatalf("inner executions = %d, want 1", len(inner.executed))
	}
	records := recorder.Records()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if records[0].Backend != "nornicdb" || records[0].Failed || records[0].RowCount != 0 {
		t.Fatalf("record = %+v, want a clean write record", records[0])
	}
}

func TestRecordingExecutorStripsWithoutGroupSupport(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	exec := WrapExecutor(&stubDifferentialExecutor{}, recorder, "nornicdb")
	checks := map[string]bool{
		"group": false,
		"phase": false,
		"probe": false,
	}
	if _, ok := exec.(interface {
		ExecuteGroup(context.Context, []sourcecypher.Statement) error
	}); ok {
		checks["group"] = true
	}
	if _, ok := exec.(interface {
		ExecutePhaseGroup(context.Context, []sourcecypher.Statement) error
	}); ok {
		checks["phase"] = true
	}
	if _, ok := exec.(interface {
		ExecuteProbe(context.Context, sourcecypher.Statement) (bool, error)
	}); ok {
		checks["probe"] = true
	}
	for name, exposed := range checks {
		if exposed {
			t.Fatalf("wrapped non-grouping inner exposes %s: callers lose the sequential fallback", name)
		}
	}
	if err := exec.Execute(context.Background(), sourcecypher.Statement{Cypher: "MERGE (n)"}); err != nil {
		t.Fatal(err)
	}
	if len(recorder.Records()) != 1 {
		t.Fatalf("records = %d, want the single execution recorded", len(recorder.Records()))
	}
}

func TestRecordingExecutorPhaseGroupRequiresInnerSupport(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	exec := WrapExecutor(&stubDifferentialGroupOnlyExecutor{}, recorder, "nornicdb")
	phased, ok := exec.(interface {
		ExecutePhaseGroup(context.Context, []sourcecypher.Statement) error
	})
	if !ok {
		t.Fatal("group-capable inner must keep the phase surface for capability probing")
	}
	err := phased.ExecutePhaseGroup(context.Background(), []sourcecypher.Statement{{Cypher: "MERGE (n)"}})
	if err == nil {
		t.Fatal("phased write on a non-phasing inner must fail loudly, not degrade silently")
	}
	if len(recorder.Records()) != 0 {
		t.Fatalf("failed phase recorded %d records", len(recorder.Records()))
	}
}

// TestCompareRecordingsCatchesSeededDivergenceRED is the seeded-violation half
// of the slice-2 RED/GREEN pair: two recordings that differ in exactly one
// statement's rows must compare unequal, naming the divergent fingerprint.
func TestCompareRecordingsCatchesSeededDivergenceRED(t *testing.T) {
	t.Parallel()
	nornicdb := []DifferentialRecord{
		{Backend: "nornicdb", Fingerprint: DifferentialFingerprint{Statement: "MATCH (n) RETURN n"}, RowCount: 2, Digest: "digest-same"},
		{Backend: "nornicdb", Fingerprint: DifferentialFingerprint{Statement: "MATCH (m) RETURN m"}, RowCount: 0, Digest: "digest-empty"},
	}
	neo4j := []DifferentialRecord{
		{Backend: "neo4j", Fingerprint: DifferentialFingerprint{Statement: "MATCH (n) RETURN n"}, RowCount: 2, Digest: "digest-same"},
		{Backend: "neo4j", Fingerprint: DifferentialFingerprint{Statement: "MATCH (m) RETURN m"}, RowCount: 1, Digest: "digest-seeded-divergence"},
	}
	diffs := CompareRecordings(nornicdb, neo4j)
	if len(diffs) != 1 {
		t.Fatalf("differences = %v, want exactly the seeded divergence", diffs)
	}
	if diffs[0].Fingerprint.Statement != "MATCH (m) RETURN m" {
		t.Fatalf("difference names %+v, want the divergent statement", diffs[0].Fingerprint)
	}
}

// TestCompareRecordingsAcceptsIdenticalPairGREEN is the other half: the same
// comparison over two recordings that agree backend-for-backend must report
// nothing. Without it the RED test alone could not distinguish "detects a
// real divergence" from "reports every recording as divergent".
func TestCompareRecordingsAcceptsIdenticalPairGREEN(t *testing.T) {
	t.Parallel()
	mk := func(backend string) []DifferentialRecord {
		return []DifferentialRecord{
			{Backend: backend, Fingerprint: DifferentialFingerprint{Statement: "MATCH (n) RETURN n"}, RowCount: 1, Digest: "digest-same"},
		}
	}
	if diffs := CompareRecordings(mk("nornicdb"), mk("neo4j")); len(diffs) != 0 {
		t.Fatalf("differences = %v, want none for an agreeing pair", diffs)
	}
}

func TestRecordingExecutorForwardsProbes(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	inner := &stubDifferentialGroupExecutor{}
	exec := WrapExecutor(inner, recorder, "neo4j")
	prober, ok := exec.(interface {
		ExecuteProbe(context.Context, sourcecypher.Statement) (bool, error)
	})
	if !ok {
		t.Fatal("group-capable inner must keep the probe surface for capability probing")
	}
	found, err := prober.ExecuteProbe(context.Background(), sourcecypher.Statement{Cypher: "MATCH (n) RETURN n LIMIT 1"})
	if err != nil || !found {
		t.Fatalf("probe = (%v, %v), want (true, nil)", found, err)
	}
	records := recorder.Records()
	if len(records) != 1 || records[0].Failed || records[0].RowCount != 1 || records[0].Digest == "" {
		t.Fatalf("records = %+v, want one clean probe record", records)
	}
}

func TestRecordingExecutorProbeRequiresInnerSupport(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	exec := WrapExecutor(&stubDifferentialGroupOnlyExecutor{}, recorder, "neo4j")
	prober, ok := exec.(interface {
		ExecuteProbe(context.Context, sourcecypher.Statement) (bool, error)
	})
	if !ok {
		t.Fatal("group-capable inner must keep the probe surface for capability probing")
	}
	if _, err := prober.ExecuteProbe(context.Background(), sourcecypher.Statement{Cypher: "MATCH (n) RETURN n LIMIT 1"}); err == nil {
		t.Fatal("probe on a non-probing inner must fail loudly, not report unknown as false")
	}
	if len(recorder.Records()) != 0 {
		t.Fatalf("failed probe recorded %d records", len(recorder.Records()))
	}
}

func TestRecordingExecutorFansGroupOutToStatements(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	inner := &stubDifferentialGroupExecutor{}
	exec := WrapExecutor(inner, recorder, "nornicdb")
	stmts := []sourcecypher.Statement{
		{Cypher: "MERGE (a:A {id: $id})", Parameters: map[string]any{"id": 1}},
		{Cypher: "MERGE (b:B {id: $id})", Parameters: map[string]any{"id": 2}},
	}
	grouped, ok := exec.(interface {
		ExecuteGroup(context.Context, []sourcecypher.Statement) error
	})
	if !ok {
		t.Fatal("wrapped executor must preserve the group surface for capability probing")
	}
	if err := grouped.ExecuteGroup(context.Background(), stmts); err != nil {
		t.Fatal(err)
	}
	if inner.groupCalls != 1 {
		t.Fatalf("inner group calls = %d, want exactly 1 (no per-statement split)", inner.groupCalls)
	}
	records := recorder.Records()
	if len(records) != 2 {
		t.Fatalf("records = %d, want one per grouped statement", len(records))
	}
	for _, rec := range records {
		if rec.Failed || rec.Backend != "nornicdb" || rec.Digest != "" || rec.RowCount != 0 {
			t.Fatalf("record = %+v, want a clean write record carrying the call outcome", rec)
		}
	}
	if records[0].Fingerprint == records[1].Fingerprint {
		t.Fatal("grouped statements share a fingerprint, the diff could not tell them apart")
	}
}

func TestRecordingExecutorFansFailedGroupOutToStatements(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	inner := &stubDifferentialGroupExecutor{stubDifferentialExecutor: stubDifferentialExecutor{err: errors.New("commit failed")}}
	exec := WrapExecutor(inner, recorder, "nornicdb")
	phased, ok := exec.(interface {
		ExecutePhaseGroup(context.Context, []sourcecypher.Statement) error
	})
	if !ok {
		t.Fatal("wrapped executor must preserve the phase surface for capability probing")
	}
	stmts := []sourcecypher.Statement{{Cypher: "MERGE (a)"}, {Cypher: "MERGE (b)"}}
	if err := phased.ExecutePhaseGroup(context.Background(), stmts); err == nil {
		t.Fatal("expected the inner phase error to propagate")
	}
	if inner.phaseCalls != 1 {
		t.Fatalf("inner phase calls = %d, want 1", inner.phaseCalls)
	}
	records := recorder.Records()
	if len(records) != 2 {
		t.Fatalf("records = %d, want one failed entry per phased statement", len(records))
	}
	for _, rec := range records {
		if !rec.Failed {
			t.Fatalf("record = %+v, want the call error carried", rec)
		}
	}
}

func TestRecordingGraphQueryCapturesSingle(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	query := WrapGraphQuery(stubDifferentialGraphQuery{rows: []map[string]any{{"n": 1}}}, recorder, "neo4j")
	row, err := query.RunSingle(context.Background(), "MATCH (n) RETURN n LIMIT 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if row["n"] != 1 {
		t.Fatalf("passthrough row = %v, want map[n:1]", row)
	}
	records := recorder.Records()
	if len(records) != 1 || records[0].RowCount != 1 || records[0].Failed {
		t.Fatalf("records = %+v, want one clean single-row record", records)
	}
}

func TestRecordingGraphQueryCapturesSingleNilRow(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	query := WrapGraphQuery(stubDifferentialGraphQuery{}, recorder, "neo4j")
	row, err := query.RunSingle(context.Background(), "MATCH (n) RETURN n LIMIT 1", nil)
	if err != nil || row != nil {
		t.Fatalf("passthrough = (%v, %v), want (nil, nil)", row, err)
	}
	records := recorder.Records()
	if len(records) != 1 || records[0].RowCount != 0 || records[0].Digest == "" {
		t.Fatalf("records = %+v, want one clean empty record", records)
	}
}

func TestCompareRecordingsFlagsRepeatedDivergence(t *testing.T) {
	t.Parallel()
	fp := DifferentialFingerprint{Statement: "MATCH (n) RETURN n"}
	stable := []DifferentialRecord{
		{Backend: "nornicdb", Fingerprint: fp, RowCount: 1, Digest: "d1"},
		{Backend: "nornicdb", Fingerprint: fp, RowCount: 1, Digest: "d1"},
	}
	flaky := []DifferentialRecord{
		{Backend: "neo4j", Fingerprint: fp, RowCount: 1, Digest: "d1"},
		{Backend: "neo4j", Fingerprint: fp, RowCount: 1, Digest: "d2"},
	}
	if diffs := CompareRecordings(stable, stable); len(diffs) != 0 {
		t.Fatalf("repeated identical executions differ: %v", diffs)
	}
	diffs := CompareRecordings(stable, flaky)
	if len(diffs) != 1 {
		t.Fatalf("differences = %v, want the repeated-execution divergence", diffs)
	}
}

// TestCompareRecordingsFlagsOneSidedFailure is the regression for the #6889
// post-merge P1: a write that succeeds on one backend and fails on the other
// must compare unequal, even though both records carry an empty digest and a
// zero row count.
func TestCompareRecordingsFlagsOneSidedFailure(t *testing.T) {
	t.Parallel()
	fp := DifferentialFingerprint{Statement: "MERGE (n:File {path: $path})", Parameters: `{"path":"a"}`}
	a := []DifferentialRecord{
		{Backend: "nornicdb", Fingerprint: fp, Failed: false},
	}
	b := []DifferentialRecord{
		{Backend: "neo4j", Fingerprint: fp, Failed: true},
	}
	diffs := CompareRecordings(a, b)
	if len(diffs) != 1 {
		t.Fatalf("differences = %v, want the one-sided failure", diffs)
	}
	if diffs[0].Fingerprint != fp {
		t.Fatalf("difference names %+v, want the failed statement", diffs[0].Fingerprint)
	}
}

// TestCompareRecordingsFlagsDoubledWrite pins that execution-count
// divergence compares unequal even when every execution agrees: the
// per-fingerprint digest multiset differs in length, so a doubled write
// (2 vs 3 successful executions) cannot pass silent.
func TestCompareRecordingsFlagsDoubledWrite(t *testing.T) {
	t.Parallel()
	fp := DifferentialFingerprint{Statement: "MERGE (n:File {path: $path})", Parameters: `{"path":"a"}`}
	mk := func(n int) []DifferentialRecord {
		out := make([]DifferentialRecord, 0, n)
		for range n {
			out = append(out, DifferentialRecord{Backend: "x", Fingerprint: fp})
		}
		return out
	}
	if diffs := CompareRecordings(mk(2), mk(2)); len(diffs) != 0 {
		t.Fatalf("differences = %v, want none for equal execution counts", diffs)
	}
	diffs := CompareRecordings(mk(2), mk(3))
	if len(diffs) != 1 {
		t.Fatalf("differences = %v, want the doubled write", diffs)
	}
}

func TestCompareRecordingsReportsMissingStatements(t *testing.T) {
	t.Parallel()
	a := []DifferentialRecord{
		{Backend: "a", Fingerprint: DifferentialFingerprint{Statement: "MATCH (n) RETURN n"}, Digest: "d"},
	}
	diffs := CompareRecordings(a, nil)
	if len(diffs) != 1 || diffs[0].Fingerprint.Statement != "MATCH (n) RETURN n" {
		t.Fatalf("differences = %v, want the one-sided statement", diffs)
	}
}

func TestWrappersPassThroughWhenCaptureDisabled(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "")
	recorder := NewDifferentialRecorder()
	inner := stubDifferentialGraphQuery{rows: []map[string]any{{"n": 1}}}
	if _, err := WrapGraphQuery(inner, recorder, "nornicdb").Run(context.Background(), "MATCH (n) RETURN n", nil); err != nil {
		t.Fatal(err)
	}
	if len(recorder.Records()) != 0 {
		t.Fatal("disabled capture still recorded")
	}
	innerExec := &stubDifferentialExecutor{}
	if err := WrapExecutor(innerExec, recorder, "nornicdb").Execute(context.Background(), sourcecypher.Statement{Cypher: "MERGE (n)"}); err != nil {
		t.Fatal(err)
	}
	if len(recorder.Records()) != 0 {
		t.Fatal("disabled capture still recorded")
	}
}

func TestCaptureEnabledReadsEnv(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	if !CaptureEnabled() {
		t.Fatal("CaptureEnabled with ESHU_DIFFERENTIAL_CAPTURE=1 is false")
	}
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "0")
	if CaptureEnabled() {
		t.Fatal("CaptureEnabled with ESHU_DIFFERENTIAL_CAPTURE=0 is true")
	}
}

func TestRecorderStreamsEveryAddedRecord(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	var streamed []DifferentialRecord
	recorder.OnRecord = func(record DifferentialRecord) {
		streamed = append(streamed, record)
	}
	inner := stubDifferentialGraphQuery{rows: []map[string]any{{"n": 1}}}
	if _, err := WrapGraphQuery(inner, recorder, "nornicdb").Run(context.Background(), "MATCH (n) RETURN n", nil); err != nil {
		t.Fatal(err)
	}
	if len(streamed) != 1 || len(recorder.Records()) != 1 {
		t.Fatalf("streamed = %d, stored = %d, want 1 and 1", len(streamed), len(recorder.Records()))
	}
	if streamed[0] != recorder.Records()[0] {
		t.Fatalf("streamed = %+v, stored = %+v, want identical", streamed[0], recorder.Records()[0])
	}
}
