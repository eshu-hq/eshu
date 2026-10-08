// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

type coveredSuccessorFakeReader struct {
	IntentReader
	covered    map[string]struct{}
	err        error
	calls      int
	gotIDs     [][]string
	gotDomains []string
}

func (f *coveredSuccessorFakeReader) CoveredByEmittedFullSuccessorIDs(
	_ context.Context, domain string, generationIDs []string,
) (map[string]struct{}, error) {
	f.calls++
	f.gotIDs = append(f.gotIDs, generationIDs)
	f.gotDomains = append(f.gotDomains, domain)
	if f.err != nil {
		return nil, f.err
	}
	covered := make(map[string]struct{})
	for _, id := range generationIDs {
		if _, ok := f.covered[id]; ok {
			covered[id] = struct{}{}
		}
	}
	return covered, nil
}

func coveredSuccessorRow(intentID, genID string) sharedintent.Row {
	return sharedintent.Row{IntentID: intentID, GenerationID: genID}
}

func TestSplitCoveredByFullSuccessorRowsDrainsOnlyCovered(t *testing.T) {
	t.Parallel()

	reader := &coveredSuccessorFakeReader{covered: map[string]struct{}{"gen-old": {}}}
	rows := []sharedintent.Row{
		coveredSuccessorRow("drain-1", "gen-old"),
		coveredSuccessorRow("drain-2", "gen-old"),
		coveredSuccessorRow("keep-1", "gen-new"),
		coveredSuccessorRow("keep-2", ""),
	}

	kept, drainable, err := SplitCoveredByFullSuccessorRows(context.Background(), reader, "code_calls", rows)
	if err != nil {
		t.Fatalf("SplitCoveredByFullSuccessorRows() error = %v", err)
	}
	if len(drainable) != 2 || drainable[0].IntentID != "drain-1" || drainable[1].IntentID != "drain-2" {
		t.Fatalf("drainable = %v, want [drain-1 drain-2]", drainable)
	}
	if len(kept) != 2 || kept[0].IntentID != "keep-1" || kept[1].IntentID != "keep-2" {
		t.Fatalf("kept = %v, want [keep-1 keep-2]", kept)
	}
	if reader.calls != 1 {
		t.Fatalf("lookups = %d, want exactly 1", reader.calls)
	}
	if len(reader.gotIDs) != 1 || len(reader.gotIDs[0]) != 2 {
		t.Fatalf("looked-up ids = %v, want the 2 distinct non-empty generation ids", reader.gotIDs)
	}
	if reader.gotDomains[0] != "code_calls" {
		t.Fatalf("looked-up domain = %q, want code_calls", reader.gotDomains[0])
	}
}

func TestSplitCoveredByFullSuccessorRowsSkipsLookupWhenNothingCovered(t *testing.T) {
	t.Parallel()

	reader := &coveredSuccessorFakeReader{covered: map[string]struct{}{}}
	rows := []sharedintent.Row{coveredSuccessorRow("keep-1", "gen-new")}

	kept, drainable, err := SplitCoveredByFullSuccessorRows(context.Background(), reader, "code_calls", rows)
	if err != nil {
		t.Fatalf("SplitCoveredByFullSuccessorRows() error = %v", err)
	}
	if len(drainable) != 0 || len(kept) != 1 {
		t.Fatalf("kept=%v drainable=%v, want all kept", kept, drainable)
	}
}

func TestSplitCoveredByFullSuccessorRowsSkipsLookupForEmptyRows(t *testing.T) {
	t.Parallel()

	reader := &coveredSuccessorFakeReader{covered: map[string]struct{}{"gen-old": {}}}
	kept, drainable, err := SplitCoveredByFullSuccessorRows(context.Background(), reader, "code_calls", nil)
	if err != nil {
		t.Fatalf("SplitCoveredByFullSuccessorRows() error = %v", err)
	}
	if len(kept) != 0 || len(drainable) != 0 || reader.calls != 0 {
		t.Fatalf("kept=%v drainable=%v lookups=%d, want empty/empty/0", kept, drainable, reader.calls)
	}
}

type plainIntentReader struct {
	IntentReader
}

func TestSplitCoveredByFullSuccessorRowsKeepsRowsWithoutThePort(t *testing.T) {
	t.Parallel()

	rows := []sharedintent.Row{coveredSuccessorRow("keep-1", "gen-old")}
	kept, drainable, err := SplitCoveredByFullSuccessorRows(context.Background(), &plainIntentReader{}, "code_calls", rows)
	if err != nil {
		t.Fatalf("SplitCoveredByFullSuccessorRows() error = %v", err)
	}
	if len(kept) != 1 || len(drainable) != 0 {
		t.Fatalf("kept=%v drainable=%v, want rows unchanged", kept, drainable)
	}
}

func TestSplitCoveredByFullSuccessorRowsPropagatesLookupError(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection reset")
	reader := &coveredSuccessorFakeReader{err: boom}
	rows := []sharedintent.Row{coveredSuccessorRow("row-1", "gen-old")}

	_, _, err := SplitCoveredByFullSuccessorRows(context.Background(), reader, "code_calls", rows)
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want wrapped %v", err, boom)
	}
}
