// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestSupersededSweepStatementsFailClosed pins the #8231 sweep's SQL
// contract: each batch deletes only a superseded generation that is not the
// scope's active generation, the scope's active pointer is NOT NULL (a NULL
// pointer fails closed instead of reading as "distinct"), and the generation
// has no claimed, running, or retrying work. The terms and documents batches
// share the pre-delete's page and ctid DELETE so they stay on one generation
// and one hash leaf.
func TestSupersededSweepStatementsFailClosed(t *testing.T) {
	for name, query := range map[string]string{
		"terms":     sweepSupersededTermsBatchQuery,
		"documents": sweepSupersededDocumentsBatchQuery,
	} {
		for _, fragment := range []string{
			"generation.status = 'superseded'",
			"generation.superseded_at IS NOT NULL",
			"scope.active_generation_id IS NOT NULL",
			"scope.active_generation_id IS DISTINCT FROM $2",
			"work.status IN ('claimed', 'running', 'retrying')",
			"AND EXISTS (SELECT 1 FROM eligible)",
		} {
			if !strings.Contains(query, fragment) {
				t.Fatalf("%s superseded sweep batch lacks %q", name, fragment)
			}
		}
	}
	start := strings.Index(deleteTermsBatchQuery, "page AS MATERIALIZED")
	if start < 0 {
		t.Fatal("pre-delete terms batch lacks its page CTE")
	}
	if body := deleteTermsBatchQuery[start:]; !strings.HasSuffix(sweepSupersededTermsBatchQuery, body) {
		t.Fatal("superseded terms sweep batch does not share the pre-delete page and DELETE")
	}
	start = strings.Index(deleteDocumentsBatchQuery, "page AS MATERIALIZED")
	if start < 0 {
		t.Fatal("pre-delete documents batch lacks its page CTE")
	}
	if body := deleteDocumentsBatchQuery[start:]; !strings.HasSuffix(sweepSupersededDocumentsBatchQuery, body) {
		t.Fatal("superseded documents sweep batch does not share the pre-delete page and DELETE")
	}
}

// TestSupersededSweepCandidatesListOnlySweepableGenerations pins the
// candidate read: terminal superseded rows with a live active pointer that
// names another generation, no live work, and index rows left to sweep,
// oldest first. A NULL active pointer never lists its generations.
func TestSupersededSweepCandidatesListOnlySweepableGenerations(t *testing.T) {
	for _, fragment := range []string{
		"generation.status = 'superseded'",
		"generation.superseded_at IS NOT NULL",
		"scope.active_generation_id IS NOT NULL",
		"scope.active_generation_id IS DISTINCT FROM generation.generation_id",
		"work.status IN ('claimed', 'running', 'retrying')",
		"eshu_search_index_terms",
		"eshu_search_index_documents",
		"eshu_search_index_stats",
		"ORDER BY generation.superseded_at",
		"LIMIT $1",
	} {
		if !strings.Contains(supersededSweepCandidatesQuery, fragment) {
			t.Fatalf("superseded sweep candidate query lacks %q:\n%s", fragment, supersededSweepCandidatesQuery)
		}
	}
}

// supersededSweepScript answers the candidate read with the given
// generations, every batch try-lock with held=true, and each table's batches
// from its own queue. Stats batches answer (eligible, deleted).
func supersededSweepScript(
	listed []Generation,
	terms, documents, stats [][][]any,
) func(string, []any) ([][]any, error) {
	return func(query string, _ []any) ([][]any, error) {
		switch {
		case strings.Contains(query, "superseded sweep candidates"):
			rows := make([][]any, 0, len(listed))
			for _, generation := range listed {
				rows = append(rows, []any{generation.ScopeID, generation.GenerationID})
			}
			return rows, nil
		case isTryLock(query):
			return [][]any{{true}}, nil
		case strings.Contains(query, "superseded terms sweep batch"):
			next := terms[0]
			terms = terms[1:]
			return next, nil
		case strings.Contains(query, "superseded documents sweep batch"):
			next := documents[0]
			documents = documents[1:]
			return next, nil
		case strings.Contains(query, "superseded stats sweep batch"):
			next := stats[0]
			stats = stats[1:]
			return next, nil
		}
		return nil, errors.New("unexpected statement: " + query)
	}
}

// countBatchLines counts the per-batch commit log lines in buf.
func countBatchLines(t *testing.T, buf *bytes.Buffer) int {
	t.Helper()
	return strings.Count(buf.String(), "superseded batch committed")
}

func TestSweepSupersededDrainsTermsDocumentsAndStats(t *testing.T) {
	old := Generation{ScopeID: "scope-a", GenerationID: "gen-old"}
	terms := [][][]any{
		page(true, BatchRows, BatchRows, "k1", "d1"), // full page: advance to (k1, d1)
		page(true, 5, 5, "k9", "d9"),                 // short page: re-sweep from the start
		page(true, 0, 0, "", ""),                     // from-start page empty: drained
	}
	documents := [][][]any{page(true, 0, 0, "", "")}
	stats := [][][]any{{{true, int64(1)}}}
	database := &scriptedDB{answer: supersededSweepScript([]Generation{old}, terms, documents, stats)}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sweep, err := SweepSuperseded(context.Background(), database, time.Time{}, 0, logger)
	if err != nil {
		t.Fatalf("SweepSuperseded() error = %v", err)
	}
	if sweep.Candidates != 1 || sweep.Unswept != 0 {
		t.Fatalf("sweep = %+v, want 1 candidate fully swept", sweep)
	}
	if sweep.RowsSwept[TermsTable] != BatchRows+5 || sweep.RowsSwept[DocumentsTable] != 0 || sweep.RowsSwept[StatsTable] != 1 {
		t.Fatalf("RowsSwept = %v, want terms=%d documents=0 stats=1", sweep.RowsSwept, BatchRows+5)
	}
	if sweep.Batches != 5 {
		t.Fatalf("Batches = %d, want 5 (3 terms + 1 documents + 1 stats)", sweep.Batches)
	}
	if got := countBatchLines(t, &buf); got != 5 {
		t.Fatalf("per-batch log lines = %d, want 5:\n%s", got, buf.String())
	}
	if !strings.Contains(buf.String(), "gen-old") || !strings.Contains(buf.String(), "scope-a") {
		t.Fatalf("batch lines lack scope/generation ids:\n%s", buf.String())
	}
}

// TestSweepSupersededLeavesAnIneligibleGenerationWhole proves the fail-closed
// batch: a listed generation whose batch reports ineligible loses no row and
// counts as unswept for a later pass.
func TestSweepSupersededLeavesAnIneligibleGenerationWhole(t *testing.T) {
	old := Generation{ScopeID: "scope-a", GenerationID: "gen-old"}
	terms := [][][]any{page(false, 0, 0, "", "")}
	database := &scriptedDB{answer: supersededSweepScript([]Generation{old}, terms, nil, nil)}
	sweep, err := SweepSuperseded(context.Background(), database, time.Time{}, 0, nil)
	if err != nil {
		t.Fatalf("SweepSuperseded() error = %v", err)
	}
	if sweep.Unswept != 1 {
		t.Fatalf("sweep = %+v, want the ineligible generation unswept", sweep)
	}
	if got := sweep.RowsSwept[TermsTable] + sweep.RowsSwept[DocumentsTable] + sweep.RowsSwept[StatsTable]; got != 0 {
		t.Fatalf("RowsSwept = %v, want no row deleted", sweep.RowsSwept)
	}
	if database.commits != 0 {
		t.Fatalf("commits = %d, want no committed batch", database.commits)
	}
}

// TestSweepSupersededHonorsTheRowCap proves one cap bounds the sweep: a
// single-batch cap stops after the first committed batch and leaves the
// generation unswept.
func TestSweepSupersededHonorsTheRowCap(t *testing.T) {
	old := Generation{ScopeID: "scope-a", GenerationID: "gen-old"}
	terms := [][][]any{
		page(true, BatchRows, BatchRows, "k1", "d1"),
		page(true, BatchRows, BatchRows, "k2", "d2"),
	}
	documents := [][][]any{page(true, 0, 0, "", "")}
	stats := [][][]any{{{true, int64(1)}}}
	database := &scriptedDB{answer: supersededSweepScript([]Generation{old}, terms, documents, stats)}
	sweep, err := SweepSuperseded(context.Background(), database, time.Time{}, BatchRows, nil)
	if err != nil {
		t.Fatalf("SweepSuperseded() error = %v", err)
	}
	if sweep.RowsSwept[TermsTable] != BatchRows || sweep.Unswept != 1 {
		t.Fatalf("sweep = %+v, want one batch then unswept", sweep)
	}
}
