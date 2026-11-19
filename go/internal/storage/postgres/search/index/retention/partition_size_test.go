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
