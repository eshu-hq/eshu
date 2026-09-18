// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"context"
	"strings"
	"testing"
)

// TestValueFlowCasesAreInTheDefaultCorpora pins the cases into the default
// corpora with exact rows. They used to sit behind an opt-in because the old
// single statement failed on NornicDB; an opt-in would now only hide a
// regression from the blocking live gate.
func TestValueFlowCasesAreInTheDefaultCorpora(t *testing.T) {
	for _, name := range []string{valueFlowWorkloadRowsCaseName, valueFlowTargetsCaseName} {
		c, ok := readCaseByName(name)
		if !ok {
			t.Fatalf("read case %q is not in DefaultReadCorpus", name)
		}
		if len(c.WantRows) == 0 {
			t.Fatalf("read case %q must assert exact rows, got none", name)
		}
	}
	var writeFound bool
	for _, c := range DefaultWriteCorpus() {
		if c.Name == valueFlowWriteCaseName {
			writeFound = true
			if !c.RequireAtomicGroup {
				t.Fatal("value-flow seed must commit atomically")
			}
		}
	}
	if !writeFound {
		t.Fatal("value-flow seed case is not in DefaultWriteCorpus")
	}
}

// TestValueFlowWorkloadRowsKeepTheAmbiguousFunction guards the case's intent:
// the first statement must return the two-workload function's rows, because
// excluding it is the loader's Go-side job. A case that expected the exclusion
// here would pass against a backend that silently drops rows.
func TestValueFlowWorkloadRowsKeepTheAmbiguousFunction(t *testing.T) {
	c, ok := readCaseByName(valueFlowWorkloadRowsCaseName)
	if !ok {
		t.Fatalf("read case %q is absent", valueFlowWorkloadRowsCaseName)
	}
	workloads := map[any]struct{}{}
	for _, row := range c.WantRows {
		if row["function_uid"] == valueFlowTwoWorkloadFunctionUID {
			workloads[row["workload_id"]] = struct{}{}
		}
	}
	if len(workloads) != 2 {
		t.Fatalf("two-workload function has %d expected workloads, want 2", len(workloads))
	}
}

func TestAnswerTruthCasesAssertExactRows(t *testing.T) {
	var found int
	for _, c := range DefaultReadCorpus() {
		if !strings.HasPrefix(c.Name, "answer-truth ") {
			continue
		}
		found++
		if len(c.WantRows) == 0 {
			t.Errorf("answer-truth case %q must assert exact rows", c.Name)
		}
	}
	if found != 4 {
		t.Fatalf("answer-truth read cases = %d, want 4", found)
	}
}

func TestCompareReadRowsIsAnExactMultiset(t *testing.T) {
	want := []map[string]any{
		{"id": "a", "count": 1, "labels": []string{"X"}},
		{"id": "b", "count": 2, "labels": []string{"Y"}},
	}
	// Driver-typed values in a different order match.
	got := []map[string]any{
		{"id": "b", "count": int64(2), "labels": []any{"Y"}},
		{"id": "a", "count": int64(1), "labels": []any{"X"}},
	}
	if err := compareReadRows(got, want); err != nil {
		t.Fatalf("equal multisets reported a difference: %v", err)
	}
	for name, bad := range map[string][]map[string]any{
		"missing row":      {{"id": "a", "count": 1, "labels": []string{"X"}}},
		"extra row":        append(append([]map[string]any(nil), got...), map[string]any{"id": "c", "count": 3, "labels": []string{}}),
		"wrong value":      {{"id": "a", "count": 3, "labels": []string{"X"}}, {"id": "b", "count": 2, "labels": []string{"Y"}}},
		"echoed text":      {{"id": "a", "count": 1, "labels": []string{"X"}}, {"id": "source.id", "count": 2, "labels": []string{"Y"}}},
		"duplicated row":   {{"id": "a", "count": 1, "labels": []string{"X"}}, {"id": "a", "count": 1, "labels": []string{"X"}}},
		"missing a column": {{"id": "a", "count": 1}, {"id": "b", "count": 2, "labels": []string{"Y"}}},
	} {
		if err := compareReadRows(bad, want); err == nil {
			t.Errorf("%s: compareReadRows accepted %v", name, bad)
		}
	}
}

// TestRunReadCorpusFailsOnWrongRows proves the runner enforces WantRows, not
// only the comparison helper: a backend that returns the right row count with a
// wrong value must fail the case.
func TestRunReadCorpusFailsOnWrongRows(t *testing.T) {
	c := ReadCase{
		Name:       "exact",
		Capability: CapabilityDirectGraphReads,
		Cypher:     "MATCH (n) RETURN count(n) AS c",
		WantRows:   []map[string]any{{"c": 1}},
	}
	wrong := &recordingGraphQuery{rows: []map[string]any{{"c": int64(3)}}}
	if _, err := RunReadCorpus(context.Background(), wrong, []ReadCase{c}); err == nil {
		t.Fatal("RunReadCorpus accepted a wrong value for an exact-row case")
	}
	right := &recordingGraphQuery{rows: []map[string]any{{"c": int64(1)}}}
	if _, err := RunReadCorpus(context.Background(), right, []ReadCase{c}); err != nil {
		t.Fatalf("RunReadCorpus rejected the exact rows: %v", err)
	}
}

func readCaseByName(name string) (ReadCase, bool) {
	for _, c := range DefaultReadCorpus() {
		if c.Name == name {
			return c, true
		}
	}
	return ReadCase{}, false
}
