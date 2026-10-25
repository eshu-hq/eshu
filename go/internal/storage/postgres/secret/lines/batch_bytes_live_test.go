// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// bulkyRecords is twelve files of about 101 KB each plus one of about 807 KB, all
// with a finding, so a window of them is large in bytes and small in files.
func bulkyRecords() []content.Record {
	padding := strings.Repeat("// filler line that carries no finding\n", 2600)
	records := make([]content.Record, 0, 13)
	for i := 0; i < 12; i++ {
		records = append(records, secretRecord(fmt.Sprintf("big/f_%04d.go", i),
			padding+fmt.Sprintf("password = \"hunter%08d\"\n", i), "go"))
	}
	huge := strings.Repeat("// filler line that carries no finding\n", 20700)
	records = append(records, secretRecord("big/zz_huge.go", huge+"password = \"hunter99999999\"\n", "go"))
	return records
}

// TestFinalizeBoundsABatchByContentBytesLive: a window of few, large files is
// cut by bytes, not only by file count. The derivation holds FOR SHARE locks for
// the whole batch, so a batch of 500 large files made a live writer on one of
// them wait about 11 s (measured on generated Go modules); the byte budget keeps
// a batch near half a second. Twelve 101 KB files under a 320 KB budget batch
// three at a time, the 807 KB file (over the budget alone) is a batch of one,
// and no file is skipped: 12/3 + 1 = 5 batches, 13 files, parity 0/0. The
// window is not full (13 of 500) yet cut by bytes, so ending the repository on
// "window not full" alone would skip the files after the first cut.
func TestFinalizeBoundsABatchByContentBytesLive(t *testing.T) {
	ctx, db, config := openDatabase(t)
	sqlDB := postgres.SQLDB{DB: db}
	epoch, err := BeginDeferral(ctx, sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	writeRecords(ctx, t, openDeferredPool(t, config), "repo-a", bulkyRecords())

	result, err := Finalize(ctx, sqlDB, epoch, Options{Workers: 1, BatchSize: 500, MaxBatchBytes: 320_000})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || result.Files != 13 || result.Batches != 5 {
		t.Fatalf("Finalize = %+v, want published, 13 files in 5 batches (byte-bounded)", result)
	}
	requireParity(ctx, t, db, "after byte-bounded batches")
}
