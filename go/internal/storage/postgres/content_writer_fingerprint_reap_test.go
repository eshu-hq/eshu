// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// TestReapStaleFingerprintsDeletesExactlyTheReadStaleSet pins the #7230 reap
// contract: each side table's stale ids come from its own set-difference read,
// are deleted in sorted chunks of staleFingerprintReapChunkSize with the
// statement for that table, and the counts and changed signal come from the
// delete results. The live tests prove the reads return the anti-join's set.
func TestReapStaleFingerprintsDeletesExactlyTheReadStaleSet(t *testing.T) {
	t.Parallel()

	bandIDs := make([][]any, 0, staleFingerprintReapChunkSize+1)
	for i := staleFingerprintReapChunkSize; i >= 0; i-- {
		bandIDs = append(bandIDs, []any{fmt.Sprintf("band-%05d", i)})
	}
	fake := &fakeExecQueryer{
		staleFingerprintRows: map[string][][]any{
			staleFingerprintEntityIDsSQL:     {{"fp-c"}, {"fp-a"}, {"fp-b"}},
			staleFingerprintBandEntityIDsSQL: bandIDs,
		},
		execResults: []sql.Result{
			fakeResultWithRowsAffected{rowsAffected: 3},
			fakeResultWithRowsAffected{rowsAffected: 64},
			fakeResultWithRowsAffected{rowsAffected: 32},
		},
	}
	reap, err := NewContentWriter(fake).reapStaleFingerprints(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("reapStaleFingerprints: %v", err)
	}

	if len(fake.execs) != 3 {
		t.Fatalf("delete statements = %d, want 3 (1 fp chunk + 2 band chunks)", len(fake.execs))
	}
	assertReapDelete(t, fake.execs[0], deleteWithdrawnFingerprintSQL, []string{"fp-a", "fp-b", "fp-c"})
	wantBand := make([]string, 0, staleFingerprintReapChunkSize+1)
	for i := 0; i <= staleFingerprintReapChunkSize; i++ {
		wantBand = append(wantBand, fmt.Sprintf("band-%05d", i))
	}
	assertReapDelete(t, fake.execs[1], deleteStaleFingerprintBandsSQL, wantBand[:staleFingerprintReapChunkSize])
	assertReapDelete(t, fake.execs[2], deleteStaleFingerprintBandsSQL, wantBand[staleFingerprintReapChunkSize:])

	want := fingerprintReap{
		staleFingerprintEntities: 3,
		staleBandEntities:        staleFingerprintReapChunkSize + 1,
		fingerprintRowsDeleted:   3,
		bandRowsDeleted:          96,
	}
	if reap != want || !reap.changed() {
		t.Fatalf("reap = %+v (changed %v), want %+v (changed true)", reap, reap.changed(), want)
	}
}

// TestReapStaleFingerprintsWithNothingStaleIssuesNoDelete proves the common
// case, every first generation included, costs two reads and no write.
func TestReapStaleFingerprintsWithNothingStaleIssuesNoDelete(t *testing.T) {
	t.Parallel()

	fake := &fakeExecQueryer{}
	reap, err := NewContentWriter(fake).reapStaleFingerprints(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("reapStaleFingerprints: %v", err)
	}
	if len(fake.execs) != 0 || len(fake.queries) != 2 {
		t.Fatalf("statements = %d execs, %d queries; want 0 and 2", len(fake.execs), len(fake.queries))
	}
	if reap != (fingerprintReap{}) || reap.changed() {
		t.Fatalf("reap = %+v, want zero and unchanged", reap)
	}
}

// TestReapStaleFingerprintsReportsUnchangedWhenDeletesRemoveNothing keeps the
// #6837 trigger honest: stale ids that no longer match a row are not a change.
func TestReapStaleFingerprintsReportsUnchangedWhenDeletesRemoveNothing(t *testing.T) {
	t.Parallel()

	fake := &fakeExecQueryer{
		staleFingerprintRows: map[string][][]any{staleFingerprintEntityIDsSQL: {{"fp-a"}}},
		execResults:          []sql.Result{fakeResultWithRowsAffected{rowsAffected: 0}},
	}
	reap, err := NewContentWriter(fake).reapStaleFingerprints(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("reapStaleFingerprints: %v", err)
	}
	if reap.staleFingerprintEntities != 1 || reap.changed() {
		t.Fatalf("reap = %+v (changed %v), want 1 stale id and unchanged", reap, reap.changed())
	}
}

// TestContentWriterLogsFingerprintReapCounts proves an operator can see what
// the reap found and removed on the reap_stale_fingerprints stage line.
func TestContentWriterLogsFingerprintReapCounts(t *testing.T) {
	t.Parallel()

	fake := &fakeExecQueryer{
		staleFingerprintRows: map[string][][]any{staleFingerprintBandEntityIDsSQL: {{"gone-1"}, {"gone-2"}}},
	}
	var logs bytes.Buffer
	writer := NewContentWriter(withTransactions(fake))
	writer.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	mat := content.Materialization{
		RepoID:  "repo-1",
		Records: []content.Record{{Path: "main.go", Body: "package main\n"}},
	}
	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write: %v", err)
	}
	var line string
	for _, candidate := range strings.Split(logs.String(), "\n") {
		if strings.Contains(candidate, `"stage":"reap_stale_fingerprints"`) {
			line = candidate
		}
	}
	for _, want := range []string{
		`"stale_fingerprint_entities":0`,
		`"stale_band_entities":2`,
		`"fingerprint_rows_deleted":0`,
		`"band_rows_deleted":1`,
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("reap_stale_fingerprints stage log missing %s: %q", want, line)
		}
	}
}

func assertReapDelete(t *testing.T, exec fakeExecCall, wantQuery string, wantIDs []string) {
	t.Helper()
	if exec.query != wantQuery {
		t.Fatalf("delete query = %.80q, want %.80q", exec.query, wantQuery)
	}
	if len(exec.args) != 2 || exec.args[0] != "repo-1" {
		t.Fatalf("delete args = %v, want repo-1 and an id array", exec.args)
	}
	got, ok := exec.args[1].(array.StringArray)
	if !ok || !slices.Equal([]string(got), wantIDs) {
		t.Fatalf("delete ids = %d (%T), want %d sorted ids", len(got), exec.args[1], len(wantIDs))
	}
}
