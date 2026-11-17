// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 proof that a generation larger than the batch row limit is drained by successive bounded batches inside one pass, each deleting at most the two picking statements' row limits plus the generation rows, with every generation swept exactly once and none counted lock_busy; and that when another session holds some of the rows the batches skip them (the limit counts the rows taken, never the ones skipped), finish everything else, and count the generation lock_busy exactly once, at the end, after which the next pass sweeps it
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"

	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

// batchRowsFromLogs reads the rows each committed batch deleted from the sweep's
// JSON log lines, in batch order.
func batchRowsFromLogs(t *testing.T, logs string) []int64 {
	t.Helper()
	var rows []int64
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, "batch committed") {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		n, ok := entry["batch_rows_deleted"].(float64)
		if !ok {
			t.Fatalf("batch line has no batch_rows_deleted: %s", line)
		}
		rows = append(rows, int64(n))
	}
	return rows
}

// TestGenerationLargerThanTheRowLimitIsDrainedInBoundedBatchesLive seeds one
// large generation next to a small one (10 evidence and 3 candidate rows) and
// sweeps with a row limit of 1,000. No batch may delete more than the two picking
// statements' limits plus the generation rows (2,050), the generations are swept
// once each, and nothing is counted lock_busy: a batch that cut a generation was
// not held by anyone, it ran out of row budget. The two cases make each picking
// statement the only one that stops a batch, so a batch that ignored either
// statement's limit, or counted only one of them as a cut, shows up as a different
// batch sequence.
func TestGenerationLargerThanTheRowLimitIsDrainedInBoundedBatchesLive(t *testing.T) {
	const limit = 1000
	for _, tc := range []struct {
		name       string
		evidence   int
		candidates int
		batches    []int64 // rows each committed batch deletes, in order
		wantRows   map[string]int64
	}{
		{
			// Batch 1 cuts both statements, batch 2 only the evidence delete (203
			// candidate rows are left, under the limit), batch 3 finishes both
			// generations (510 evidence rows and the two generation rows).
			name: "evidence larger", evidence: 2500, candidates: 1200,
			batches: []int64{2000, 1203, 512},
			wantRows: map[string]int64{
				relationshipretention.EvidenceTable: 2510, relationshipretention.CandidatesTable: 1203, relationshipretention.GenerationsTable: 2,
			},
		},
		{
			// Batch 1 cuts both statements, batches 2 and 3 only the candidates
			// delete (the evidence is gone after 510 rows in batch 2), batch 4
			// finishes with 503 candidate rows and the two generation rows.
			name: "candidates larger", evidence: 1500, candidates: 3500,
			batches: []int64{2000, 1510, 1000, 505},
			wantRows: map[string]int64{
				relationshipretention.EvidenceTable: 1510, relationshipretention.CandidatesTable: 3503, relationshipretention.GenerationsTable: 2,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openLiveFixture(t)
			f.scope(t, "scope-big")
			for _, id := range []string{"gen-big", "gen-small"} {
				f.generation(t, "scope-big", id, "superseded")
			}
			f.evidence(t, "gen-big", tc.evidence)
			f.candidates(t, "gen-big", tc.candidates)
			f.evidence(t, "gen-small", 10)
			f.candidates(t, "gen-small", 3)

			var logs bytes.Buffer
			result, err := relationshipretention.Sweep(f.ctx, f.database, relationshipretention.Options{
				Deletes: true, RowCap: 1 << 40, BatchRows: limit, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
			})
			if err != nil {
				t.Fatalf("Sweep: %v", err)
			}
			perBatch := batchRowsFromLogs(t, logs.String())
			t.Logf("rows per batch: %v", perBatch)
			bound := int64(2*limit + relationshipretention.BatchGenerations)
			for i, n := range perBatch {
				if n > bound {
					t.Errorf("batch %d deleted %d rows, want at most %d (two row limits and the generation rows)", i+1, n, bound)
				}
			}
			if !slices.Equal(perBatch, tc.batches) {
				t.Fatalf("rows per batch = %v, want %v", perBatch, tc.batches)
			}
			if result.Batches != len(tc.batches) {
				t.Fatalf("pass = %d batches, want %d", result.Batches, len(tc.batches))
			}
			if result.GenerationsSwept != 2 || result.Capped != "" {
				t.Fatalf("pass swept %d generations, capped %q, want both swept in a clean pass", result.GenerationsSwept, result.Capped)
			}
			wantSkipped := map[string]int{relationshipretention.SkipLockBusy: 0, relationshipretention.SkipLockTimeout: 0, relationshipretention.SkipIneligible: 0}
			if !reflect.DeepEqual(result.Skipped, wantSkipped) {
				t.Fatalf("skipped = %v, want %v: a generation cut by the row limit is not held by anyone", result.Skipped, wantSkipped)
			}
			if !reflect.DeepEqual(result.RowsSwept, tc.wantRows) {
				t.Fatalf("rows swept = %v, want %v", result.RowsSwept, tc.wantRows)
			}
			for _, id := range []string{"gen-big", "gen-small"} {
				if got := f.of(t, id); got != (counts{}) {
					t.Fatalf("%s = %+v after the pass, want nothing left", id, got)
				}
			}
		})
	}
}

// TestLargeGenerationWithSomeRowsHeldEndsBusyOnceLive holds the first 100
// evidence rows of a 2,500-row generation from another session. Each batch skips
// them without waiting and takes the next rows, so with a limit of 1,000 the
// batches delete 1,000, 1,000 and 400: the limit counts the rows taken, never the
// ones skipped. The generation row and the held rows stay, the generation is
// counted lock_busy once (by the batch that was no longer cut), and the next pass
// sweeps it once the holder lets go.
func TestLargeGenerationWithSomeRowsHeldEndsBusyOnceLive(t *testing.T) {
	f := openLiveFixture(t)
	f.scope(t, "scope-held-big")
	f.generation(t, "scope-held-big", "gen-held-big", "superseded")
	f.evidence(t, "gen-held-big", 2500)
	release := f.holdRows(t, `SELECT 1 FROM relationship_evidence_facts
WHERE evidence_id = ANY (SELECT $1::text || '-ev-' || i FROM generate_series(1, 100) i) FOR UPDATE`, "gen-held-big")

	const limit = 1000
	var logs bytes.Buffer
	opts := relationshipretention.Options{
		Deletes: true, RowCap: 1 << 40, BatchRows: limit, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	first, err := relationshipretention.Sweep(f.ctx, f.database, opts)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	perBatch := batchRowsFromLogs(t, logs.String())
	t.Logf("rows per batch: %v", perBatch)
	if want := []int64{400, 1000, 1000}; !slices.Equal(sorted(perBatch), want) {
		t.Fatalf("rows per batch = %v, want %v in some order: the limit counts rows taken, not the held ones it skipped", perBatch, want)
	}
	if first.GenerationsSwept != 0 || first.Batches != 3 || first.Skipped[relationshipretention.SkipLockBusy] != 1 ||
		first.Skipped[relationshipretention.SkipLockTimeout] != 0 {
		t.Fatalf("pass 1 = swept %d in %d batches, skipped %v; want 3 batches and the generation lock_busy once, never a lock timeout",
			first.GenerationsSwept, first.Batches, first.Skipped)
	}
	if got := first.RowsSwept[relationshipretention.EvidenceTable]; got != 2400 {
		t.Fatalf("pass 1 swept %d evidence rows, want 2,400 (all but the 100 held)", got)
	}
	if got := f.of(t, "gen-held-big"); got != (counts{evidence: 100, generations: 1}) {
		t.Fatalf("rows after pass 1 = %+v, want the 100 held evidence rows and the generation row", got)
	}

	release()
	second, err := relationshipretention.Sweep(f.ctx, f.database, opts)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if second.GenerationsSwept != 1 || second.Skipped[relationshipretention.SkipLockBusy] != 0 {
		t.Fatalf("pass 2 = swept %d, skipped %v; want the generation swept once its rows are free", second.GenerationsSwept, second.Skipped)
	}
	if got := f.of(t, "gen-held-big"); got != (counts{}) {
		t.Fatalf("rows after pass 2 = %+v, want none", got)
	}
}

// sorted returns a sorted copy.
func sorted(in []int64) []int64 {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}
