// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"testing"
)

// TestPlanHolderReadStatementsSplitsHotIDs is the #10643 failing-first proof
// for cardinality-adaptive holder-read splitting: one 5-id fanout chunk is
// partitioned by measured per-id row mass, down to one hot id per statement,
// so no statement re-forms the 67k-row / 9.8 s timed-out shape. Cold ids keep
// the existing 5-per-statement packing. It fails until planHolderReadStatements
// exists.
func TestPlanHolderReadStatementsSplitsHotIDs(t *testing.T) {
	t.Parallel()

	hot := int64(fanOutHolderHotMassRows + 1)
	chunk := []string{"CVE-2026-hot", "CVE-2026-c1", "CVE-2026-c2", "CVE-2026-c3", "CVE-2026-c4"}
	masses := map[string]int64{
		"CVE-2026-hot": hot,
		"CVE-2026-c1":  12,
		"CVE-2026-c2":  34,
		"CVE-2026-c3":  56,
		"CVE-2026-c4":  78,
	}
	got := planHolderReadStatements(chunk, masses)
	want := [][]string{
		{"CVE-2026-hot"},
		{"CVE-2026-c1", "CVE-2026-c2", "CVE-2026-c3", "CVE-2026-c4"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("planHolderReadStatements(%v) = %v, want %v", chunk, got, want)
	}
}

// TestPlanHolderReadStatementsKeepsColdChunksWhole proves the common case is
// byte-identical to today: an all-cold chunk stays one statement, so the
// existing chunking tests and the admitted id set do not move.
func TestPlanHolderReadStatementsKeepsColdChunksWhole(t *testing.T) {
	t.Parallel()

	chunk := []string{"CVE-2026-0001", "CVE-2026-0002", "CVE-2026-0003", "CVE-2026-0004", "CVE-2026-0005"}
	masses := map[string]int64{
		"CVE-2026-0001": 3,
		"CVE-2026-0002": 0,
		"CVE-2026-0003": 41,
		"CVE-2026-0004": 7,
		"CVE-2026-0005": 19,
	}
	got := planHolderReadStatements(chunk, masses)
	want := [][]string{{
		"CVE-2026-0001", "CVE-2026-0002", "CVE-2026-0003", "CVE-2026-0004", "CVE-2026-0005",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("planHolderReadStatements(%v) = %v, want %v", chunk, got, want)
	}
}

// TestPlanHolderReadStatementsIsolatesEveryHotID proves the all-hot edge: five
// hot ids in one chunk become five singleton statements, each under the same
// 10 s guard the unsplit statement would have blown.
func TestPlanHolderReadStatementsIsolatesEveryHotID(t *testing.T) {
	t.Parallel()

	hot := int64(fanOutHolderHotMassRows + 1)
	chunk := []string{"CVE-2026-h1", "CVE-2026-h2", "CVE-2026-h3", "CVE-2026-h4", "CVE-2026-h5"}
	masses := map[string]int64{}
	for _, id := range chunk {
		masses[id] = hot
	}
	got := planHolderReadStatements(chunk, masses)
	want := [][]string{{"CVE-2026-h1"}, {"CVE-2026-h2"}, {"CVE-2026-h3"}, {"CVE-2026-h4"}, {"CVE-2026-h5"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("planHolderReadStatements(%v) = %v, want %v", chunk, got, want)
	}
}

// TestPlanHolderReadStatementsFailsSafeWithoutMass proves the fail-safe
// direction: an id with no measured mass (estimator absent or a row the probe
// did not return) is isolated alone, never packed into a statement that could
// re-form the timed-out shape.
func TestPlanHolderReadStatementsFailsSafeWithoutMass(t *testing.T) {
	t.Parallel()

	chunk := []string{"CVE-2026-known", "CVE-2026-unknown"}
	got := planHolderReadStatements(chunk, map[string]int64{"CVE-2026-known": 5})
	want := [][]string{{"CVE-2026-known"}, {"CVE-2026-unknown"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("planHolderReadStatements(%v) = %v, want %v", chunk, got, want)
	}
	if got := planHolderReadStatements(chunk, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("planHolderReadStatements(%v, nil) = %v, want %v", chunk, got, want)
	}
}

// TestPlanHolderReadStatementsCapsCombinedColdMass proves cold packing honors
// the mass budget too: five cold ids whose combined mass exceeds the hot
// threshold never share one statement, so a cold-only chunk cannot rebuild a
// 67k-row statement out of sub-threshold ids.
func TestPlanHolderReadStatementsCapsCombinedColdMass(t *testing.T) {
	t.Parallel()

	perID := int64(fanOutHolderHotMassRows) / 2
	chunk := []string{"CVE-2026-m1", "CVE-2026-m2", "CVE-2026-m3"}
	masses := map[string]int64{
		"CVE-2026-m1": perID,
		"CVE-2026-m2": perID,
		"CVE-2026-m3": perID,
	}
	got := planHolderReadStatements(chunk, masses)
	if len(got) != 2 {
		t.Fatalf("planHolderReadStatements(%v) = %v, want 2 statements", chunk, got)
	}
	var total int
	for _, stmt := range got {
		if len(stmt) > fanOutHolderReadChunk {
			t.Fatalf("statement %v exceeds the %d-id chunk cap", stmt, fanOutHolderReadChunk)
		}
		var sum int64
		for _, id := range stmt {
			sum += masses[id]
		}
		total += len(stmt)
		if len(stmt) > 1 && sum > fanOutHolderHotMassRows {
			t.Fatalf("multi-id statement %v covers %d rows, over the %d-row budget", stmt, sum, fanOutHolderHotMassRows)
		}
	}
	if total != len(chunk) {
		t.Fatalf("plan covers %d ids, want %d: %v", total, len(chunk), got)
	}
}

// TestPlanHolderReadStatementsKeepsIDSet proves the split never changes the
// admitted set: every input id appears in exactly one statement, in chunk
// order within and across statements.
func TestPlanHolderReadStatementsKeepsIDSet(t *testing.T) {
	t.Parallel()

	hot := int64(fanOutHolderHotMassRows + 1)
	chunk := []string{"CVE-2026-a", "CVE-2026-b", "CVE-2026-c", "CVE-2026-d", "CVE-2026-e"}
	masses := map[string]int64{
		"CVE-2026-a": 1,
		"CVE-2026-b": hot,
		"CVE-2026-c": int64(fanOutHolderHotMassRows) / 2,
		"CVE-2026-d": int64(fanOutHolderHotMassRows) / 2,
		"CVE-2026-e": 2,
	}
	var flat []string
	for _, stmt := range planHolderReadStatements(chunk, masses) {
		flat = append(flat, stmt...)
	}
	seen := map[string]int{}
	for _, id := range flat {
		seen[id]++
	}
	for _, id := range chunk {
		if seen[id] != 1 {
			t.Fatalf("id %q appears %d times in plan %v, want exactly once", id, seen[id], flat)
		}
	}
}
