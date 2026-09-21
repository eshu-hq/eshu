// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import "testing"

// unwindBatchStatement is the production batch-write shape whose batch
// boundaries differ run to run: the same rows grouped differently must pair
// by element, not by batch.
const unwindBatchStatement = "UNWIND $rows AS row MATCH (f:File {path: row.file_path}) MERGE (n:Class {uid: row.uid})"

func unwindBatchFingerprint(t *testing.T, rows ...map[string]any) DifferentialFingerprint {
	t.Helper()
	fp, err := FingerprintStatement(unwindBatchStatement, map[string]any{"rows": rows})
	if err != nil {
		t.Fatalf("FingerprintStatement() error = %v", err)
	}
	return fp
}

func unwindBatchRecord(backend string, fp DifferentialFingerprint) DifferentialRecord {
	return DifferentialRecord{Backend: backend, Fingerprint: fp}
}

// TestCompareRecordingsPairsUnwindBatchesByElement is the slice-3 regression
// for batch-boundary nondeterminism: the NornicDB leg and the Neo4j leg slice
// the same UNWIND rows into different batches, so whole-batch fingerprints
// never pair and every batch reports one-sided. Exploding batches into
// per-element groups must compare clean when both sides attempted the same
// write set.
func TestCompareRecordingsPairsUnwindBatchesByElement(t *testing.T) {
	t.Parallel()
	a := map[string]any{"file_path": "a.go", "uid": "u-a"}
	b := map[string]any{"file_path": "b.go", "uid": "u-b"}
	c := map[string]any{"file_path": "c.go", "uid": "u-c"}
	nornicdb := []DifferentialRecord{
		unwindBatchRecord("nornicdb", unwindBatchFingerprint(t, a, b)),
		unwindBatchRecord("nornicdb", unwindBatchFingerprint(t, c)),
	}
	neo4j := []DifferentialRecord{
		unwindBatchRecord("neo4j", unwindBatchFingerprint(t, a)),
		unwindBatchRecord("neo4j", unwindBatchFingerprint(t, b, c)),
	}
	if diffs := CompareRecordings(nornicdb, neo4j); len(diffs) != 0 {
		t.Fatalf("differences = %v, want none for the same element set in different batches", diffs)
	}
}

// TestCompareRecordingsFlagsMissingUnwindElement pins the other half: when
// one backend never attempts an element, exactly one difference names it, so
// a dropped write cannot hide inside batch regrouping.
func TestCompareRecordingsFlagsMissingUnwindElement(t *testing.T) {
	t.Parallel()
	a := map[string]any{"file_path": "a.go", "uid": "u-a"}
	b := map[string]any{"file_path": "b.go", "uid": "u-b"}
	nornicdb := []DifferentialRecord{
		unwindBatchRecord("nornicdb", unwindBatchFingerprint(t, a, b)),
	}
	neo4j := []DifferentialRecord{
		unwindBatchRecord("neo4j", unwindBatchFingerprint(t, a)),
	}
	diffs := CompareRecordings(nornicdb, neo4j)
	if len(diffs) != 1 {
		t.Fatalf("differences = %v, want exactly the missing element", diffs)
	}
	if diffs[0].Fingerprint.Statement != unwindBatchStatement {
		t.Fatalf("difference names %q, want the batch statement", diffs[0].Fingerprint.Statement)
	}
}

// TestCompareRecordingsUnwindWithoutListFallsBack pins fail-closed grouping:
// an UNWIND variable bound to a non-list (or absent) keeps whole-fingerprint
// pairing, so an unrecognized shape compares strictly instead of pairing
// vacuously.
func TestCompareRecordingsUnwindWithoutListFallsBack(t *testing.T) {
	t.Parallel()
	fp := func(params map[string]any) DifferentialFingerprint {
		out, err := FingerprintStatement("UNWIND $n AS x MERGE (m {v: x})", params)
		if err != nil {
			t.Fatalf("FingerprintStatement() error = %v", err)
		}
		return out
	}
	nornicdb := []DifferentialRecord{
		{Backend: "nornicdb", Fingerprint: fp(map[string]any{"n": "scalar"})},
	}
	neo4j := []DifferentialRecord{
		{Backend: "neo4j", Fingerprint: fp(map[string]any{"n": "other"})},
	}
	diffs := CompareRecordings(nornicdb, neo4j)
	if len(diffs) != 2 {
		t.Fatalf("differences = %v, want both one-sided whole-batch records", diffs)
	}
	for _, diff := range diffs {
		if diff.Kind != "missing" {
			t.Fatalf("kind = %q, want missing for unpaired whole-batch records", diff.Kind)
		}
	}
}

// TestUnwindBatchVarMatchesFirstUnwind pins multi-UNWIND grouping: only the
// first UNWIND variable explodes, and the remaining bindings stay in the
// group key, so regrouping the second batch still splits groups loudly
// instead of pairing vacuously.
func TestUnwindBatchVarMatchesFirstUnwind(t *testing.T) {
	t.Parallel()
	if got := unwindBatchVar("UNWIND $a AS x UNWIND $b AS y MERGE (m {v: x})"); got != "a" {
		t.Fatalf("unwindBatchVar() = %q, want the first variable a", got)
	}
	if got := unwindBatchVar("MATCH (n) RETURN n"); got != "" {
		t.Fatalf("unwindBatchVar() = %q, want empty for a non-batch statement", got)
	}
}

// TestCompareRecordingsPairsInListBatchesByElement pins element pairing for
// IN-list batch statements: stale-generation retraction passes slice the
// same file set into differently ordered batches run to run, so whole-batch
// fingerprints never pair. Exploding the single-use IN-list variable pairs
// per file.
func TestCompareRecordingsPairsInListBatchesByElement(t *testing.T) {
	t.Parallel()
	const stmt = "MATCH (p:Parameter) WHERE p.path IN $file_paths AND p.evidence_source = 'projector/canonical' DETACH DELETE p"
	fp := func(files ...string) DifferentialFingerprint {
		rows := make([]any, 0, len(files))
		for _, f := range files {
			rows = append(rows, f)
		}
		out, err := FingerprintStatement(stmt, map[string]any{"file_paths": rows})
		if err != nil {
			t.Fatalf("FingerprintStatement() error = %v", err)
		}
		return out
	}
	nornicdb := []DifferentialRecord{
		{Backend: "nornicdb", Fingerprint: fp("catalog-info.yaml", "application.yaml", "deployment.yaml")},
	}
	neo4j := []DifferentialRecord{
		{Backend: "neo4j", Fingerprint: fp("deployment.yaml", "catalog-info.yaml", "application.yaml")},
	}
	if diffs := CompareRecordings(nornicdb, neo4j); len(diffs) != 0 {
		t.Fatalf("differences = %v, want none for the same file set in different order", diffs)
	}
}

// TestCompareRecordingsInListMultiUseFallsBack pins fail-closed grouping for
// IN-list variables referenced more than once: the statement may depend on
// the list beyond membership, so it keeps whole-fingerprint pairing.
func TestCompareRecordingsInListMultiUseFallsBack(t *testing.T) {
	t.Parallel()
	const stmt = "MATCH (p) WHERE p.path IN $files RETURN $files"
	fp := func(files ...string) DifferentialFingerprint {
		rows := make([]any, 0, len(files))
		for _, f := range files {
			rows = append(rows, f)
		}
		out, err := FingerprintStatement(stmt, map[string]any{"files": rows})
		if err != nil {
			t.Fatalf("FingerprintStatement() error = %v", err)
		}
		return out
	}
	nornicdb := []DifferentialRecord{
		{Backend: "nornicdb", Fingerprint: fp("a.yaml", "b.yaml")},
	}
	neo4j := []DifferentialRecord{
		{Backend: "neo4j", Fingerprint: fp("b.yaml", "a.yaml")},
	}
	diffs := CompareRecordings(nornicdb, neo4j)
	if len(diffs) != 2 {
		t.Fatalf("differences = %v, want both one-sided whole-batch records", diffs)
	}
}

// TestCompareRecordingsPairsUnwindElementsModuloListOrder pins element
// pairing across evidence-ordering nondeterminism: the same workload
// materialized with the same provenance SET in a different order is the same
// logical write, so list order inside an element view never splits groups.
func TestCompareRecordingsPairsUnwindElementsModuloListOrder(t *testing.T) {
	t.Parallel()
	element := func(provenance []any) map[string]any {
		return map[string]any{
			"workload_id": "workload:container-ci-lineage",
			"provenance":  provenance,
		}
	}
	fp := func(rows ...map[string]any) DifferentialFingerprint {
		out, err := FingerprintStatement(
			"UNWIND $rows AS row MERGE (w:Workload {id: row.workload_id}) SET w.provenance = row.provenance",
			map[string]any{"rows": rows},
		)
		if err != nil {
			t.Fatalf("FingerprintStatement() error = %v", err)
		}
		return out
	}
	nornicdb := []DifferentialRecord{
		{Backend: "nornicdb", Fingerprint: fp(element([]any{"dockerfile_runtime", "github_actions_workflow"}))},
	}
	neo4j := []DifferentialRecord{
		{Backend: "neo4j", Fingerprint: fp(element([]any{"github_actions_workflow", "dockerfile_runtime"}))},
	}
	if diffs := CompareRecordings(nornicdb, neo4j); len(diffs) != 0 {
		t.Fatalf("differences = %v, want none for the same provenance set in different order", diffs)
	}
}
