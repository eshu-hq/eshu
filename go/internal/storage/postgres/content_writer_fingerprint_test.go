// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
)

// TestFingerprintRowFromMetadata proves the writer extracts the four
// fingerprint metadata keys into the narrow side-table row shape, and that
// entities without fingerprints (absent means "not fingerprinted") yield no
// row. Exact-only tiers carry no renamed hash or sketch.
func TestFingerprintRowFromMetadata(t *testing.T) {
	t.Parallel()

	full := fingerprintRowFromMetadata("e1", "r1", map[string]any{
		fingerprint.KeyExact:      "abc",
		fingerprint.KeyRenamed:    "def",
		fingerprint.KeySketch:     "00ff",
		fingerprint.KeyTokenCount: 77,
	})
	if !full.hasFingerprint {
		t.Fatal("full fingerprint metadata must yield a row")
	}
	if full.fpExact != "abc" || full.tokenCount != 77 {
		t.Fatalf("wrong row extracted: %+v", full)
	}
	if full.fpRenamed != "def" || full.fpSketch != "00ff" {
		t.Fatalf("wrong optional fields: %+v", full)
	}

	exactOnly := fingerprintRowFromMetadata("e2", "r1", map[string]any{
		fingerprint.KeyExact:      "abc",
		fingerprint.KeyTokenCount: 60,
	})
	if !exactOnly.hasFingerprint {
		t.Fatal("exact-only metadata must yield a row")
	}
	if exactOnly.fpRenamed != nil || exactOnly.fpSketch != nil {
		t.Fatalf("exact-only row must leave renamed/sketch nil: %+v", exactOnly)
	}

	for _, metadata := range []map[string]any{
		nil,
		{},
		{fingerprint.KeyExact: "abc"},
		{fingerprint.KeyTokenCount: 77},
		{fingerprint.KeyExact: "abc", fingerprint.KeyTokenCount: "77"},
	} {
		if row := fingerprintRowFromMetadata("e3", "r1", metadata); row.hasFingerprint {
			t.Fatalf("metadata %v must yield no row, got %+v", metadata, row)
		}
	}
}

// TestContentWriterPersistsFingerprintSideTables proves a Write carrying
// fingerprinted function entities upserts the narrow fp row plus one band
// row per sketch band, and reaps side rows whose entity vanished — without
// touching source_cache. The fake records every statement for inspection.
func TestContentWriterPersistsFingerprintSideTables(t *testing.T) {
	t.Parallel()

	sketch := make([]uint64, fingerprint.SketchRegs)
	for i := range sketch {
		sketch[i] = uint64(i + 1)
	}
	sketchHex := fingerprint.EncodeSketch(sketch)
	metadata := map[string]any{
		fingerprint.KeyExact:      "exact-1",
		fingerprint.KeyRenamed:    "renamed-1",
		fingerprint.KeySketch:     sketchHex,
		fingerprint.KeyTokenCount: 64,
	}

	fake := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(fake))
	materialization := content.Materialization{
		RepoID: "repo-fp",
		Records: []content.Record{
			{Path: "a.go", Body: "package p\n", Digest: "d1"},
		},
		Entities: []content.EntityRecord{
			{
				EntityID:   "repo-fp|a.go|Function|big|10",
				Path:       "a.go",
				EntityType: "Function",
				EntityName: "big",
				StartLine:  10,
				EndLine:    40,
				Metadata:   metadata,
			},
			{
				EntityID:   "repo-fp|a.go|Function|small|50",
				Path:       "a.go",
				EntityType: "Function",
				EntityName: "small",
				StartLine:  50,
				EndLine:    52,
			},
		},
	}
	if _, err := writer.Write(context.Background(), materialization); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var fpUpserts, bandUpserts, fpReaps, bandReaps, scopedBandDeletes int
	var fpArgs []any
	for _, exec := range fake.execs {
		switch {
		case strings.Contains(exec.query, "INSERT INTO code_function_fingerprint"):
			fpUpserts++
			fpArgs = exec.args
			if strings.Contains(exec.query, "source_cache") {
				t.Fatalf("fingerprint upsert must never touch source_cache: %.200s", exec.query)
			}
		case strings.Contains(exec.query, "INSERT INTO code_fingerprint_band"):
			bandUpserts++
			assertBandBatch(t, exec.args)
			if strings.Contains(exec.query, "source_cache") {
				t.Fatalf("band upsert must never touch source_cache: %.200s", exec.query)
			}
		case strings.Contains(exec.query, "DELETE FROM code_function_fingerprint"):
			fpReaps++
		case strings.Contains(exec.query, "DELETE FROM code_fingerprint_band"):
			bandReaps++
			if strings.Contains(exec.query, "ANY(") {
				scopedBandDeletes++
			}
		}
	}
	if fpUpserts != 1 {
		t.Fatalf("fp upserts = %d, want 1 (only the fingerprinted entity)", fpUpserts)
	}
	if bandUpserts < 1 {
		t.Fatal("expected at least one band batch upsert")
	}
	if fpReaps != 1 {
		t.Fatalf("fp reaps = %d, want 1", fpReaps)
	}
	// Two band deletes: the scoped rewrite invalidation (entity_id set) plus
	// the repo-wide stale-entity reap.
	if bandReaps != 2 || scopedBandDeletes != 1 {
		t.Fatalf("band deletes = %d (scoped %d), want 2 total with 1 scoped", bandReaps, scopedBandDeletes)
	}
	assertFingerprintArgs(t, fpArgs, "repo-fp|a.go|Function|big|10", "exact-1", "renamed-1", sketchHex, 64)
}

// TestFingerprintRewriteInvalidatesBands proves re-fingerprinting an entity
// with a new sketch removes its prior band rows before inserting the fresh
// ones: band storage stays a pure function of the persisted fp row, so edited
// functions cannot accumulate orphan bands (false LSH candidates for #6837).
func TestFingerprintRewriteInvalidatesBands(t *testing.T) {
	t.Parallel()

	sketchFor := func(base uint64) string {
		sketch := make([]uint64, fingerprint.SketchRegs)
		for i := range sketch {
			sketch[i] = base + uint64(i)*0x9e3779b97f4a7c15
		}
		return fingerprint.EncodeSketch(sketch)
	}
	rowFor := func(sketchHex string) preparedFingerprintRow {
		return preparedFingerprintRow{
			entityID:       "repo-rw|a.go|Function|big|10",
			repoID:         "repo-rw",
			fpExact:        "exact-rw",
			fpRenamed:      "renamed-rw",
			fpSketch:       sketchHex,
			tokenCount:     64,
			hasFingerprint: true,
		}
	}

	now := time.Now()
	fake := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(fake))
	ctx := context.Background()
	if err := writer.upsertFingerprintBatches(ctx, []preparedFingerprintRow{rowFor(sketchFor(1))}, now); err != nil {
		t.Fatalf("first upsertFingerprintBatches: %v", err)
	}
	if err := writer.upsertFingerprintBatches(ctx, []preparedFingerprintRow{rowFor(sketchFor(2))}, now); err != nil {
		t.Fatalf("second upsertFingerprintBatches: %v", err)
	}

	var bandInserts []int
	var scopedDeletes []int
	for i, exec := range fake.execs {
		if !strings.Contains(exec.query, "code_fingerprint_band") {
			continue
		}
		switch {
		case strings.HasPrefix(strings.TrimSpace(exec.query), "DELETE") && strings.Contains(exec.query, "ANY("):
			scopedDeletes = append(scopedDeletes, i)
		case strings.Contains(exec.query, "INSERT INTO"):
			bandInserts = append(bandInserts, i)
		}
	}
	if len(bandInserts) < 2 {
		t.Fatalf("band INSERTs = %d, want >= 2 (one per rewrite generation)", len(bandInserts))
	}
	// Each upsert generation deletes before it inserts; the rewrite
	// generation's delete must therefore precede its own inserts while
	// following the first generation's inserts.
	if len(scopedDeletes) != 2 {
		t.Fatalf("scoped deletes = %d, want 2 (one per upsert generation)", len(scopedDeletes))
	}
	last := bandInserts[len(bandInserts)-1]
	if scopedDeletes[1] < bandInserts[0] || scopedDeletes[1] > last {
		t.Fatalf("rewrite scoped delete (stmt %d) must fall between the first (%d) and last (%d) band inserts",
			scopedDeletes[1], bandInserts[0], last)
	}
}

func assertBandBatch(t *testing.T, args []any) {
	t.Helper()
	if len(args)%4 != 0 {
		t.Fatalf("band batch args = %d, want a multiple of 4", len(args))
	}
	seenBands := map[int]bool{}
	for i := 0; i < len(args); i += 4 {
		bandNo, ok := args[i+1].(int)
		if !ok {
			t.Fatalf("band_no arg = %T, want int", args[i+1])
		}
		seenBands[bandNo] = true
	}
	if len(seenBands) != fingerprint.LSHBands {
		t.Fatalf("distinct bands = %d, want %d", len(seenBands), fingerprint.LSHBands)
	}
}

func assertFingerprintArgs(t *testing.T, args []any, entityID, exact, renamed, sketch string, tokens int) {
	t.Helper()
	if len(args) != 7 {
		t.Fatalf("fp upsert args = %d, want 7", len(args))
	}
	if args[0] != entityID || args[2] != exact || args[5] != tokens {
		t.Fatalf("fp upsert args mismatch: %v", args)
	}
	if args[3] != renamed || args[4] != sketch {
		t.Fatalf("fp optional args mismatch: %v", args)
	}
}
