// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestTakeCensusSumsLeavesAndDeduplicatesOrphans walks two terms leaves and
// the documents table: counts sum per table, a pair orphaned in both tables
// is listed once, and the list is sorted.
func TestTakeCensusSumsLeavesAndDeduplicatesOrphans(t *testing.T) {
	database := &scriptedDB{answer: func(query string, _ []any) ([][]any, error) {
		switch {
		case query == termsLeavesQuery:
			return [][]any{{"eshu_search_index_terms_p00"}, {"eshu_search_index_terms_p01"}}, nil
		case strings.Contains(query, "FROM eshu_search_index_terms_p00 AS relation"):
			return [][]any{{int64(5), int64(1), "scope-b", "gen-b"}}, nil
		case strings.Contains(query, "FROM eshu_search_index_terms_p01 AS relation"):
			return [][]any{{int64(7), int64(0), nil, nil}}, nil
		case strings.Contains(query, "FROM eshu_search_index_documents AS relation"):
			return [][]any{{int64(12), int64(2), "scope-a", "gen-a"}, {int64(12), int64(2), "scope-b", "gen-b"}}, nil
		}
		t.Fatalf("unexpected query %q", query)
		return nil, nil
	}}
	tx, _ := database.Begin(context.Background())
	census, err := TakeCensus(context.Background(), tx)
	if err != nil {
		t.Fatalf("TakeCensus() error = %v", err)
	}
	if want := map[string]int64{TermsTable: 12, DocumentsTable: 12}; !reflect.DeepEqual(census.PairsScanned, want) {
		t.Fatalf("PairsScanned = %v, want %v", census.PairsScanned, want)
	}
	if want := map[string]int64{TermsTable: 1, DocumentsTable: 2}; !reflect.DeepEqual(census.OrphanGenerations, want) {
		t.Fatalf("OrphanGenerations = %v, want %v", census.OrphanGenerations, want)
	}
	want := []Generation{{ScopeID: "scope-a", GenerationID: "gen-a"}, {ScopeID: "scope-b", GenerationID: "gen-b"}}
	if !reflect.DeepEqual(census.Orphans, want) {
		t.Fatalf("Orphans = %v, want %v", census.Orphans, want)
	}
}

// TestTakeCensusFallsBackToAnUnpartitionedTermsTable keeps the census
// working on an install whose terms table has no leaves, and a clean census
// reads 0 for both tables.
func TestTakeCensusFallsBackToAnUnpartitionedTermsTable(t *testing.T) {
	database := &scriptedDB{answer: func(query string, _ []any) ([][]any, error) {
		if query == termsLeavesQuery {
			return nil, nil
		}
		return [][]any{{int64(3), int64(0), nil, nil}}, nil
	}}
	tx, _ := database.Begin(context.Background())
	census, err := TakeCensus(context.Background(), tx)
	if err != nil {
		t.Fatalf("TakeCensus() error = %v", err)
	}
	if !strings.Contains(database.statements[1], "FROM eshu_search_index_terms AS relation") {
		t.Fatalf("census did not walk the unpartitioned terms table:\n%s", database.statements[1])
	}
	if census.OrphanGenerations[TermsTable] != 0 || census.OrphanGenerations[DocumentsTable] != 0 || len(census.Orphans) != 0 {
		t.Fatalf("census = %+v, want a clean census", census)
	}
}

// TestSweepOrphansReturnsTheCensusWhenABatchFails is review finding F1: a
// census that succeeded is returned with the batch error, so the runner can
// set the gauge from it; a census that failed returns no table counts.
func TestSweepOrphansReturnsTheCensusWhenABatchFails(t *testing.T) {
	database := &scriptedDB{answer: func(query string, _ []any) ([][]any, error) {
		switch {
		case query == termsLeavesQuery:
			return nil, nil
		case strings.Contains(query, "orphan census"):
			return [][]any{{int64(4), int64(1), "scope-a", "gen-a"}}, nil
		case isTryLock(query):
			return [][]any{{true}}, nil
		}
		return nil, errors.New("connection reset")
	}}
	sweep, err := SweepOrphans(context.Background(), database, time.Time{}, 0, nil)
	if err == nil {
		t.Fatal("SweepOrphans() error = nil, want the batch error")
	}
	if sweep.Census.OrphanGenerations[TermsTable] != 1 || sweep.Census.OrphanGenerations[DocumentsTable] != 1 {
		t.Fatalf("census = %v, want 1 orphan per table returned with the error", sweep.Census.OrphanGenerations)
	}

	failing := &scriptedDB{answer: func(string, []any) ([][]any, error) { return nil, errors.New("census down") }}
	sweep, err = SweepOrphans(context.Background(), failing, time.Time{}, 0, nil)
	if err == nil || sweep.Census.OrphanGenerations != nil {
		t.Fatalf("failed census = (%v, %v), want an error and no table counts", sweep.Census.OrphanGenerations, err)
	}
}

// TestSweepOrphansCountsOnlyCommittedBatches pins OrphanSweep.Batches after
// #8276 review thread A: a listed orphan whose advisory lock another session
// holds rolls back and adds no batch, while a drained orphan adds its two
// committed empty pages (one per table).
func TestSweepOrphansCountsOnlyCommittedBatches(t *testing.T) {
	database := &scriptedDB{answer: func(query string, args []any) ([][]any, error) {
		switch {
		case query == termsLeavesQuery:
			return nil, nil
		case strings.Contains(query, "orphan census"):
			return [][]any{{int64(4), int64(2), "scope-a", "gen-busy"}, {int64(4), int64(2), "scope-a", "gen-free"}}, nil
		case isTryLock(query):
			return [][]any{{args[0] != "gen-busy"}}, nil
		}
		return page(true, 0, 0, "", ""), nil
	}}
	sweep, err := SweepOrphans(context.Background(), database, time.Time{}, 0, nil)
	if err != nil {
		t.Fatalf("SweepOrphans() error = %v", err)
	}
	if sweep.Batches != 2 || database.commits != 2 || sweep.Unswept != 1 {
		t.Fatalf("Batches = %d, commits = %d, Unswept = %d; want 2 committed batches and gen-busy unswept", sweep.Batches, database.commits, sweep.Unswept)
	}
}
