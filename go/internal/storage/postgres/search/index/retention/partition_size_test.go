// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

import (
	"context"
	"strings"
	"testing"
)

// TestTakePartitionSizeCensusReadsOneCatalogPass is the #10874 RED-first
// proof: a bloated leaf (p29-shaped: 193 GiB total at ~500 KiB/row) must be
// visible from ONE catalog-only statement, and a planted bloated leaf must
// trip the PAGE verdict while a dense small leaf and an empty leaf stay
// quiet. It fails until TakePartitionSizeCensus exists.
func TestTakePartitionSizeCensusReadsOneCatalogPass(t *testing.T) {
	database := &scriptedDB{answer: func(query string, _ []any) ([][]any, error) {
		if query == partitionSizeCensusQuery {
			return [][]any{
				{"eshu_search_index_terms_p29", int64(207544205312), int64(30502215680), int64(176990003200), int64(386974)},
				{"eshu_search_index_terms_p07", int64(39740211200), int64(2180579328), int64(37549441312), int64(914886)},
				{"eshu_search_index_terms_p03", int64(34493464576), int64(11549392896), int64(22940844032), int64(21326268)},
			}, nil
		}
		t.Fatalf("unexpected query %q", query)
		return nil, nil
	}}
	census, err := TakePartitionSizeCensus(context.Background(), database)
	if err != nil {
		t.Fatalf("TakePartitionSizeCensus() error = %v", err)
	}
	if len(database.statements) != 1 {
		t.Fatalf("statements = %d, want exactly 1 catalog-only pass", len(database.statements))
	}
	if len(census) != 3 {
		t.Fatalf("leaves = %d, want 3", len(census))
	}
	if got := ClassifyPartitionSize(census[0]); got != PartitionSizePage {
		t.Fatalf("p29 verdict = %v, want PAGE (193 GiB at ~536 KiB/row)", got)
	}
	// The verdicts above read TotalBytes only, so a heap/index column swap
	// in the census SQL or Scan would stay green. Pin the scanned mapping
	// against the distinct canned values of the p29 fixture row.
	if census[0].HeapBytes != 30502215680 {
		t.Fatalf("p29 HeapBytes = %d, want 30502215680", census[0].HeapBytes)
	}
	if census[0].IndexBytes != 176990003200 {
		t.Fatalf("p29 IndexBytes = %d, want 176990003200", census[0].IndexBytes)
	}
	if census[0].EstimatedRows != 386974 {
		t.Fatalf("p29 EstimatedRows = %d, want 386974", census[0].EstimatedRows)
	}
	if got := ClassifyPartitionSize(census[1]); got != PartitionSizePage {
		t.Fatalf("p07 verdict = %v, want PAGE (37 GiB at ~43 KiB/row)", got)
	}
	if got := ClassifyPartitionSize(census[2]); got != PartitionSizeOK {
		t.Fatalf("p03 verdict = %v, want OK (dense healthy leaf)", got)
	}
}

// TestClassifyPartitionSizeSeededViolation is the new-gate RED/GREEN pair:
// the planted bloated leaf trips WARN, a tiny-but-sparse leaf stays quiet
// (the absolute-size half of the AND), a huge-but-dense leaf stays quiet
// (the ratio half), and an empty leaf never alarms.
func TestClassifyPartitionSizeSeededViolation(t *testing.T) {
	bloated := PartitionSize{Leaf: "eshu_search_index_terms_p29", TotalBytes: 8 << 30, EstimatedRows: 100 << 10}
	if got := ClassifyPartitionSize(bloated); got != PartitionSizeWarn {
		t.Fatalf("bloated leaf verdict = %v, want WARN", got)
	}
	tinySparse := PartitionSize{Leaf: "eshu_search_index_terms_p01", TotalBytes: 1 << 20, EstimatedRows: 1}
	if got := ClassifyPartitionSize(tinySparse); got != PartitionSizeOK {
		t.Fatalf("tiny sparse leaf verdict = %v, want OK", got)
	}
	hugeDense := PartitionSize{Leaf: "eshu_search_index_terms_p03", TotalBytes: 32 << 30, EstimatedRows: 32 << 20}
	if got := ClassifyPartitionSize(hugeDense); got != PartitionSizeOK {
		t.Fatalf("huge dense leaf verdict = %v, want OK", got)
	}
	empty := PartitionSize{Leaf: "eshu_search_index_terms_p02", TotalBytes: 64 << 10, EstimatedRows: 0}
	if got := ClassifyPartitionSize(empty); got != PartitionSizeOK {
		t.Fatalf("empty leaf verdict = %v, want OK", got)
	}
}

// TestPartitionParitySnapshotMatchesAcrossIdenticalReads is the #10874
// search-parity harness: the same leaf read twice compares equal, while a
// lost or rewritten row breaks the comparison. The copy-rebuild runbook
// takes one snapshot before REINDEX CONCURRENTLY and one after; equality is
// the rebuild-equivalence proof.
func TestPartitionParitySnapshotMatchesAcrossIdenticalReads(t *testing.T) {
	database := &scriptedDB{answer: func(query string, _ []any) ([][]any, error) {
		if strings.Contains(query, "partition parity snapshot") {
			return [][]any{{int64(386974), int64(-123456789)}}, nil
		}
		t.Fatalf("unexpected query %q", query)
		return nil, nil
	}}
	before, err := TakePartitionParitySnapshot(context.Background(), database, "eshu_search_index_terms_p29")
	if err != nil {
		t.Fatalf("TakePartitionParitySnapshot() error = %v", err)
	}
	after, err := TakePartitionParitySnapshot(context.Background(), database, "eshu_search_index_terms_p29")
	if err != nil {
		t.Fatalf("TakePartitionParitySnapshot() error = %v", err)
	}
	if err := ComparePartitionParity(before, after); err != nil {
		t.Fatalf("ComparePartitionParity() error = %v, want identical snapshots to compare equal", err)
	}
	after.Rows++
	if err := ComparePartitionParity(before, after); err == nil {
		t.Fatal("ComparePartitionParity() error = nil, want a mismatch when a row is lost")
	}
}

// TestPartitionParitySnapshotRejectsUnexpectedLeaf is the #10874 hardening
// proof: only a terms leaf short name or its schema-qualified regclass text
// reaches SQL; anything else fails before a statement is built.
func TestPartitionParitySnapshotRejectsUnexpectedLeaf(t *testing.T) {
	for _, leaf := range []string{
		"eshu_search_index_terms_p29",
		"eshu_search_index_terms_p00",
		"eshu_search_index_terms_p63",
		"public.eshu_search_index_terms_p29",
	} {
		database := &scriptedDB{answer: func(query string, _ []any) ([][]any, error) {
			if strings.Contains(query, "partition parity snapshot") {
				return [][]any{{int64(1), int64(2)}}, nil
			}
			t.Fatalf("unexpected query %q", query)
			return nil, nil
		}}
		if _, err := TakePartitionParitySnapshot(context.Background(), database, leaf); err != nil {
			t.Fatalf("TakePartitionParitySnapshot(%q) error = %v, want accepted", leaf, err)
		}
		if len(database.statements) != 1 {
			t.Fatalf("TakePartitionParitySnapshot(%q) statements = %d, want 1", leaf, len(database.statements))
		}
	}
	for _, leaf := range []string{
		"",
		"eshu_search_index_terms_p29; DROP TABLE eshu_search_index_terms_p29",
		"eshu_search_index_terms_p7",
		"eshu_search_index_terms",
		"public.eshu_search_index_documents",
		"pg_class",
	} {
		database := &scriptedDB{answer: func(query string, _ []any) ([][]any, error) {
			t.Fatalf("rejected leaf %q reached SQL: %q", leaf, query)
			return nil, nil
		}}
		if _, err := TakePartitionParitySnapshot(context.Background(), database, leaf); err == nil {
			t.Fatalf("TakePartitionParitySnapshot(%q) error = nil, want rejection before SQL", leaf)
		}
		if len(database.statements) != 0 {
			t.Fatalf("TakePartitionParitySnapshot(%q) statements = %d, want 0", leaf, len(database.statements))
		}
	}
}
