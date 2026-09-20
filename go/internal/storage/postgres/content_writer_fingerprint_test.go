// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
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

// TestFingerprintRowCarriesShingles proves the #6837 persistence contract:
// the writer extracts the KeyShingles shingle-set hex into the narrow
// side-table row so the reducer can verify exact Jaccard from the row
// without reading source_cache. Pre-#6837 payloads without the key and
// exact-only tiers leave shingles nil; the fp row itself is unaffected.
func TestFingerprintRowCarriesShingles(t *testing.T) {
	t.Parallel()

	shingles := fingerprint.EncodeShingles([]uint64{7, 42, 99})
	full := fingerprintRowFromMetadata("e1", "r1", map[string]any{
		fingerprint.KeyExact:      "abc",
		fingerprint.KeyRenamed:    "def",
		fingerprint.KeySketch:     "00ff",
		fingerprint.KeyShingles:   shingles,
		fingerprint.KeyTokenCount: 77,
	})
	if !full.hasFingerprint {
		t.Fatal("full fingerprint metadata must yield a row")
	}
	if full.fpShingles != shingles {
		t.Fatalf("fpShingles = %v, want %q", full.fpShingles, shingles)
	}

	legacy := fingerprintRowFromMetadata("e2", "r1", map[string]any{
		fingerprint.KeyExact:      "abc",
		fingerprint.KeyRenamed:    "def",
		fingerprint.KeySketch:     "00ff",
		fingerprint.KeyTokenCount: 77,
	})
	if !legacy.hasFingerprint {
		t.Fatal("pre-#6837 metadata without shingles must still yield a row")
	}
	if legacy.fpShingles != nil {
		t.Fatalf("pre-#6837 row must leave shingles nil, got %v", legacy.fpShingles)
	}

	exactOnly := fingerprintRowFromMetadata("e3", "r1", map[string]any{
		fingerprint.KeyExact:      "abc",
		fingerprint.KeyTokenCount: 60,
	})
	if !exactOnly.hasFingerprint {
		t.Fatal("exact-only metadata must yield a row")
	}
	if exactOnly.fpShingles != nil {
		t.Fatalf("exact-only row must leave shingles nil, got %v", exactOnly.fpShingles)
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
	shinglesHex := fingerprint.EncodeShingles([]uint64{7, 42, 99})
	metadata := map[string]any{
		fingerprint.KeyExact:      "exact-1",
		fingerprint.KeyRenamed:    "renamed-1",
		fingerprint.KeySketch:     sketchHex,
		fingerprint.KeyShingles:   shinglesHex,
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

	var fpUpserts, bandUpserts, fpReaps, fpWithdrawnDeletes, bandReaps, scopedBandDeletes int
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
			if strings.Contains(exec.query, "ANY(") {
				fpWithdrawnDeletes++
			} else {
				fpReaps++
			}
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
	// The never-fingerprinted `small` entity flows through the withdrawn
	// path: one scoped fp delete and one scoped band delete, both no-ops
	// against empty side tables.
	if fpWithdrawnDeletes != 1 {
		t.Fatalf("fp withdrawn deletes = %d, want 1", fpWithdrawnDeletes)
	}
	// Three band deletes: the scoped rewrite invalidation (entity_id set),
	// the scoped withdrawn delete, plus the repo-wide stale-entity reap.
	if bandReaps != 3 || scopedBandDeletes != 2 {
		t.Fatalf("band deletes = %d (scoped %d), want 3 total with 2 scoped", bandReaps, scopedBandDeletes)
	}
	assertFingerprintArgs(t, fpArgs, "repo-fp|a.go|Function|big|10", "exact-1", "renamed-1", sketchHex, shinglesHex, 64)
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
	if changed, err := writer.upsertFingerprintBatches(ctx, []preparedFingerprintRow{rowFor(sketchFor(1))}, now); err != nil {
		t.Fatalf("first upsertFingerprintBatches: %v", err)
	} else if !changed {
		t.Fatal("fp-bearing upsert must report changed")
	}
	if changed, err := writer.upsertFingerprintBatches(ctx, []preparedFingerprintRow{rowFor(sketchFor(2))}, now); err != nil {
		t.Fatalf("second upsertFingerprintBatches: %v", err)
	} else if !changed {
		t.Fatal("fp-bearing upsert must report changed")
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

func assertFingerprintArgs(t *testing.T, args []any, entityID, exact, renamed, sketch, shingles string, tokens int) {
	t.Helper()
	if len(args) != 8 {
		t.Fatalf("fp upsert args = %d, want 8", len(args))
	}
	if args[0] != entityID || args[2] != exact || args[6] != tokens {
		t.Fatalf("fp upsert args mismatch: %v", args)
	}
	if args[3] != renamed || args[4] != sketch || args[5] != shingles {
		t.Fatalf("fp optional args mismatch: %v", args)
	}
}

// TestFingerprintWithdrawalDeletesSideRows proves the P1 withdrawal case: a
// surviving entity re-emitted without fingerprint keys (body edited below
// the floor, file gains a parse error) must shed its stale
// code_function_fingerprint row and code_fingerprint_band rows in the same
// Write. The entity still exists in content_entities, so the stale-entity
// reap cannot converge it; without an explicit withdrawn delete the #6836
// grouping path would keep reading the old exact hash, sketch, and bands
// as current truth.
func TestFingerprintWithdrawalDeletesSideRows(t *testing.T) {
	t.Parallel()

	sketch := make([]uint64, fingerprint.SketchRegs)
	for i := range sketch {
		sketch[i] = uint64(i + 1)
	}
	fingerprinted := map[string]any{
		fingerprint.KeyExact:      "exact-wd",
		fingerprint.KeyRenamed:    "renamed-wd",
		fingerprint.KeySketch:     fingerprint.EncodeSketch(sketch),
		fingerprint.KeyTokenCount: 64,
	}
	entity := func(metadata map[string]any) content.EntityRecord {
		return content.EntityRecord{
			EntityID:   "repo-wd|a.go|Function|big|10",
			Path:       "a.go",
			EntityType: "Function",
			EntityName: "big",
			StartLine:  10,
			EndLine:    40,
			Metadata:   metadata,
		}
	}
	materialization := func(metadata map[string]any) content.Materialization {
		return content.Materialization{
			RepoID:  "repo-wd",
			Records: []content.Record{{Path: "a.go", Body: "package p\n", Digest: "d1"}},
			Entities: []content.EntityRecord{
				entity(metadata),
			},
		}
	}

	fake := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(fake))
	ctx := context.Background()
	if _, err := writer.Write(ctx, materialization(fingerprinted)); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if _, err := writer.Write(ctx, materialization(nil)); err != nil {
		t.Fatalf("second Write without fingerprint keys: %v", err)
	}

	var fpWithdrawnDeletes, bandWithdrawnDeletes int
	for _, exec := range fake.execs {
		if !strings.Contains(exec.query, "ANY(") {
			continue
		}
		switch {
		case strings.HasPrefix(strings.TrimSpace(exec.query), "DELETE") &&
			strings.Contains(exec.query, "code_function_fingerprint"):
			fpWithdrawnDeletes++
			assertExecTargetsEntity(t, exec.args, "repo-wd|a.go|Function|big|10")
		case strings.HasPrefix(strings.TrimSpace(exec.query), "DELETE") &&
			strings.Contains(exec.query, "code_fingerprint_band"):
			bandWithdrawnDeletes++
			assertExecTargetsEntity(t, exec.args, "repo-wd|a.go|Function|big|10")
		}
	}
	if fpWithdrawnDeletes < 1 {
		t.Fatal("re-emitting an entity without fingerprint keys must delete its stale code_function_fingerprint row")
	}
	if bandWithdrawnDeletes < 1 {
		t.Fatal("re-emitting an entity without fingerprint keys must delete its stale code_fingerprint_band rows")
	}
}

// TestFingerprintSketchLossInvalidatesBands proves the sketch-loss case: an
// entity rewritten from sketch-bearing to sketch-less (exact-only tier after
// a tier demotion, or an undecodable sketch) keeps its fingerprint row but
// yields zero band rows, so its prior code_fingerprint_band rows must still
// be invalidated. Without the delete the #6836 grouping path would read
// bands that no longer derive from the persisted sketch.
func TestFingerprintSketchLossInvalidatesBands(t *testing.T) {
	t.Parallel()

	sketch := make([]uint64, fingerprint.SketchRegs)
	for i := range sketch {
		sketch[i] = uint64(i + 1)
	}
	sketchHex := fingerprint.EncodeSketch(sketch)
	rowFor := func(sketchHex string) preparedFingerprintRow {
		row := preparedFingerprintRow{
			entityID:       "repo-sl|a.go|Function|big|10",
			repoID:         "repo-sl",
			fpExact:        "exact-sl",
			tokenCount:     64,
			hasFingerprint: true,
		}
		if sketchHex != "" {
			row.fpSketch = sketchHex
		}
		return row
	}

	now := time.Now()
	fake := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(fake))
	ctx := context.Background()
	if changed, err := writer.upsertFingerprintBatches(ctx, []preparedFingerprintRow{rowFor(sketchHex)}, now); err != nil {
		t.Fatalf("sketch-bearing upsertFingerprintBatches: %v", err)
	} else if !changed {
		t.Fatal("fp-bearing upsert must report changed")
	}
	if changed, err := writer.upsertFingerprintBatches(ctx, []preparedFingerprintRow{rowFor("")}, now); err != nil {
		t.Fatalf("sketch-less upsertFingerprintBatches: %v", err)
	} else if !changed {
		t.Fatal("fp row without sketch still (re)publishes the fp row")
	}

	var fpUpserts, bandInserts, scopedBandDeletes int
	for _, exec := range fake.execs {
		switch {
		case strings.Contains(exec.query, "INSERT INTO code_function_fingerprint"):
			fpUpserts++
		case strings.Contains(exec.query, "INSERT INTO code_fingerprint_band"):
			bandInserts++
		case strings.HasPrefix(strings.TrimSpace(exec.query), "DELETE") &&
			strings.Contains(exec.query, "code_fingerprint_band") &&
			strings.Contains(exec.query, "ANY("):
			assertExecTargetsEntity(t, exec.args, "repo-sl|a.go|Function|big|10")
			scopedBandDeletes++
		}
	}
	if fpUpserts != 2 {
		t.Fatalf("fp upserts = %d, want 2 (the fp row persists across the sketch loss)", fpUpserts)
	}
	if bandInserts < 1 {
		t.Fatal("first generation must insert band rows")
	}
	// Rewrite invalidation plus sketch-loss invalidation: the second
	// generation yields no bands, so its scoped delete is the only proof
	// the prior bands were shed.
	if scopedBandDeletes != 2 {
		t.Fatalf("scoped band deletes = %d, want 2 (rewrite + sketch-loss invalidation)", scopedBandDeletes)
	}
}

// TestDeleteWithdrawnFingerprintsChunksAtFileBatchSize proves the repo-scale
// bound on the withdrawn path: withdrawn holds every non-fingerprinted
// entity in the Write (classes, variables, below-floor functions — not just
// lost fingerprints), so one unbounded text[] per repo would ship
// multi-megabyte parameters and hundreds of thousands of no-op index probes
// on every full-sync Write. Scoped deletes chunk at contentFileBatchSize,
// matching the sibling delete convention in Write().
func TestDeleteWithdrawnFingerprintsChunksAtFileBatchSize(t *testing.T) {
	t.Parallel()

	const perRepo = contentFileBatchSize + 150 // forces two chunks per repo
	rows := make([]preparedFingerprintRow, 0, 2*perRepo)
	want := map[string]bool{}
	for r := 0; r < 2; r++ {
		repoID := "repo-chunk-wd-a"
		if r == 1 {
			repoID = "repo-chunk-wd-b"
		}
		for i := 0; i < perRepo; i++ {
			id := repoID + "|a.go|Class|C" + itoa(i)
			want[id] = true
			rows = append(rows, preparedFingerprintRow{entityID: id, repoID: repoID})
		}
	}

	fake := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(fake))
	if _, err := writer.deleteWithdrawnFingerprints(context.Background(), rows); err != nil {
		t.Fatalf("deleteWithdrawnFingerprints: %v", err)
	}

	var fpDeletes, bandDeletes int
	covered := map[string]bool{}
	for _, exec := range fake.execs {
		q := strings.TrimSpace(exec.query)
		if !strings.HasPrefix(q, "DELETE") || !strings.Contains(exec.query, "ANY(") {
			continue
		}
		var isFP, isBand bool
		switch {
		case strings.Contains(exec.query, "code_function_fingerprint"):
			isFP = true
			fpDeletes++
		case strings.Contains(exec.query, "code_fingerprint_band"):
			isBand = true
			bandDeletes++
		}
		if !isFP && !isBand {
			continue
		}
		ids := deleteArgIDs(t, exec.args)
		if len(ids) > contentFileBatchSize {
			t.Fatalf("scoped delete carries %d IDs, want at most %d", len(ids), contentFileBatchSize)
		}
		for _, id := range ids {
			if !want[id] {
				t.Fatalf("scoped delete targets unexpected entity %q", id)
			}
			covered[id] = true
		}
	}
	// Two repos × two chunks × two tables.
	if fpDeletes != 4 || bandDeletes != 4 {
		t.Fatalf("scoped deletes fp=%d band=%d, want 4 and 4", fpDeletes, bandDeletes)
	}
	if len(covered) != len(want) {
		t.Fatalf("covered %d of %d withdrawn entities", len(covered), len(want))
	}
}

// TestDeleteFingerprintBandsForEntitiesChunksAtFileBatchSize proves the same
// bound for the rewrite invalidation: a repo-scale Write can rewrite more
// than contentFileBatchSize fingerprinted entities, so the pre-insert shed
// chunks instead of one unbounded text[].
func TestDeleteFingerprintBandsForEntitiesChunksAtFileBatchSize(t *testing.T) {
	t.Parallel()

	rows := make([]preparedFingerprintRow, 0, contentFileBatchSize+50)
	want := map[string]bool{}
	for i := 0; i < contentFileBatchSize+50; i++ {
		id := "repo-chunk-bi|a.go|Function|big" + itoa(i)
		want[id] = true
		rows = append(rows, preparedFingerprintRow{
			entityID:       id,
			repoID:         "repo-chunk-bi",
			fpExact:        "exact-" + itoa(i),
			tokenCount:     64,
			hasFingerprint: true,
		})
	}

	fake := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(fake))
	if _, err := writer.deleteFingerprintBandsForEntities(context.Background(), rows); err != nil {
		t.Fatalf("deleteFingerprintBandsForEntities: %v", err)
	}

	var deletes int
	covered := map[string]bool{}
	for _, exec := range fake.execs {
		if !strings.HasPrefix(strings.TrimSpace(exec.query), "DELETE") ||
			!strings.Contains(exec.query, "code_fingerprint_band") ||
			!strings.Contains(exec.query, "ANY(") {
			continue
		}
		deletes++
		ids := deleteArgIDs(t, exec.args)
		if len(ids) > contentFileBatchSize {
			t.Fatalf("scoped delete carries %d IDs, want at most %d", len(ids), contentFileBatchSize)
		}
		for _, id := range ids {
			covered[id] = true
		}
	}
	if deletes != 2 {
		t.Fatalf("scoped band deletes = %d, want 2", deletes)
	}
	if len(covered) != len(want) {
		t.Fatalf("covered %d of %d rewritten entities", len(covered), len(want))
	}
}

// deleteArgIDs extracts the entity text[] argument of a scoped
// (repo_id, entities) delete for bound and coverage assertions.
func deleteArgIDs(t *testing.T, args []any) []string {
	t.Helper()
	for _, arg := range args[1:] {
		if ids, ok := arg.(pgarray.StringArray); ok {
			return []string(ids)
		}
	}
	t.Fatalf("scoped delete carries no entity text[] arg: %v", args)
	return nil
}

// assertExecTargetsEntity proves a scoped (repo, entity-set) delete carries
// the withdrawn entity: args are (repo_id, entity text[]) per the shared
// scoped-delete shape.
func assertExecTargetsEntity(t *testing.T, args []any, entityID string) {
	t.Helper()
	if len(args) < 2 {
		t.Fatalf("scoped delete args = %v, want (repo_id, entities)", args)
	}
	found := false
	for _, arg := range args[1:] {
		ids, ok := arg.(pgarray.StringArray)
		if !ok {
			continue
		}
		for _, id := range ids {
			if id == entityID {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("scoped delete must target %q, got args %v", entityID, args)
	}
}

// TestWriteReportsFingerprintsChanged proves the #6837 trigger contract:
// Write reports whether this call (re)published fingerprint side-table
// truth, so the projector enqueues exactly one drifted intent per
// fingerprint-bearing generation. A fingerprinted entity marks changed; a
// fingerprint-free write stays quiet; a withdrawn entity whose deletes hit
// rows marks changed so stale findings retire.
func TestWriteReportsFingerprintsChanged(t *testing.T) {
	t.Parallel()

	write := func(entities []content.EntityRecord, zeroDeletes bool) content.Result {
		fake := &fakeExecQueryer{}
		if zeroDeletes {
			// Over-provision zero-affected results: the default fake
			// reports 1 affected row per Exec, which would mark every
			// delete leg changed. Queued results drain FIFO; leftovers
			// are ignored, so the count need not match the exec count.
			for i := 0; i < 64; i++ {
				fake.execResults = append(fake.execResults, fakeResultWithRowsAffected{})
			}
		}
		writer := NewContentWriter(withTransactions(fake))
		res, err := writer.Write(context.Background(), content.Materialization{
			RepoID:       "repo-fc",
			ScopeID:      "repo:repo-fc",
			GenerationID: "gen-1",
			Records:      []content.Record{{Path: "a.go", Body: "package p\n", Digest: "d1"}},
			Entities:     entities,
		})
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		return res
	}

	fingerprinted := []content.EntityRecord{{
		EntityID:   "repo-fc|a.go|Function|big|10",
		Path:       "a.go",
		EntityType: "Function",
		EntityName: "big",
		StartLine:  10,
		EndLine:    40,
		Metadata: map[string]any{
			fingerprint.KeyExact:      "exact-fc",
			fingerprint.KeyTokenCount: 64,
		},
	}}
	if res := write(fingerprinted, false); !res.FingerprintsChanged {
		t.Fatal("fingerprinted Write must report FingerprintsChanged")
	}

	plain := []content.EntityRecord{{
		EntityID:   "repo-fc|a.go|Class|C|1",
		Path:       "a.go",
		EntityType: "Class",
		EntityName: "C",
		StartLine:  1,
		EndLine:    5,
	}}
	// Zero-affected deletes model the already-clean side table: no stale
	// rows disappear, so nothing changed.
	if res := write(plain, true); res.FingerprintsChanged {
		t.Fatal("fingerprint-free Write must stay quiet")
	}
}

// TestWithdrawnDeletesReportAffected proves the withdrawal leg of the #6837
// trigger signal: a withdrawn entity whose scoped deletes hit stale rows
// counts as changed (stale findings must retire), while no-op deletes on an
// already-clean side table stay quiet.
func TestWithdrawnDeletesReportAffected(t *testing.T) {
	t.Parallel()

	rows := []preparedFingerprintRow{{entityID: "repo-wd|a.go|Function|f|1", repoID: "repo-wd"}}

	quiet := &fakeExecQueryer{}
	for i := 0; i < 8; i++ {
		quiet.execResults = append(quiet.execResults, fakeResultWithRowsAffected{})
	}
	writer := NewContentWriter(withTransactions(quiet))
	if affected, err := writer.deleteWithdrawnFingerprints(context.Background(), rows); err != nil {
		t.Fatalf("deleteWithdrawnFingerprints: %v", err)
	} else if affected != 0 {
		t.Fatalf("no-op deletes affected = %d, want 0", affected)
	}

	hit := &fakeExecQueryer{execResults: []sql.Result{
		fakeResultWithRowsAffected{rowsAffected: 1},
		fakeResultWithRowsAffected{rowsAffected: 2},
	}}
	writer = NewContentWriter(withTransactions(hit))
	if affected, err := writer.deleteWithdrawnFingerprints(context.Background(), rows); err != nil {
		t.Fatalf("deleteWithdrawnFingerprints: %v", err)
	} else if affected != 3 {
		t.Fatalf("hit deletes affected = %d, want 3 (fp + band)", affected)
	}
}
