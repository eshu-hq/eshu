// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// assertExecCountWithFingerprintReaps asserts a Write issued exactly
// wantBase statements plus the 2 unconditional fingerprint side-table reaps
// (reapStaleFingerprints runs on every Write, even with zero fingerprint
// rows) plus wantScopedFP / wantScopedBand scoped deletes (entities
// rewritten without fingerprint keys shed their side rows in the same call;
// a Write with no entities, or whose entities all carry fingerprint keys,
// issues none). It pins the +2 to the intended repo-wide reap statements —
// every scoped delete carries an ANY() predicate and never matches — so a
// stray statement cannot hide behind the bumped counts.
func assertExecCountWithFingerprintReaps(t *testing.T, execs []fakeExecCall, wantBase, wantScopedFP, wantScopedBand int, what string) {
	t.Helper()
	reaps := 0
	scopedFP := 0
	scopedBand := 0
	for _, exec := range execs {
		isFP := strings.Contains(exec.query, "DELETE FROM code_function_fingerprint")
		isBand := strings.Contains(exec.query, "DELETE FROM code_fingerprint_band")
		scoped := strings.Contains(exec.query, "ANY(")
		switch {
		case (isFP || isBand) && !scoped:
			reaps++
		case isFP && scoped:
			scopedFP++
		case isBand && scoped:
			// Scoped band deletes serve two owners sharing one statement
			// shape: rewrite invalidation and withdrawn converges. No
			// current caller rewrites, so every scoped band delete here is
			// a withdrawn converge; a future rewrite caller must fold its
			// invalidation into wantScopedBand.
			scopedBand++
		}
	}
	if reaps != 2 {
		t.Fatalf("fingerprint reap statements = %d, want 2 (%s)", reaps, what)
	}
	if scopedFP != wantScopedFP {
		t.Fatalf("scoped fp deletes = %d, want %d (%s)", scopedFP, wantScopedFP, what)
	}
	if scopedBand != wantScopedBand {
		t.Fatalf("scoped band deletes = %d, want %d (%s)", scopedBand, wantScopedBand, what)
	}
	if got, want := len(execs), wantBase+2+wantScopedFP+wantScopedBand; got != want {
		t.Fatalf("exec count = %d, want %d (%s + 2 fingerprint reaps + %d scoped fp + %d scoped band)", got, want, what, wantScopedFP, wantScopedBand)
	}
}

func TestContentWriterBatchesFileInserts(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(db))
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	// Create 3 file records (small batch, should result in 1 query)
	records := []content.Record{
		{Path: "file1.go", Body: "content1", Metadata: map[string]string{"language": "go"}},
		{Path: "file2.go", Body: "content2", Metadata: map[string]string{"language": "go"}},
		{Path: "file3.go", Body: "content3", Metadata: map[string]string{"language": "go"}},
	}

	mat := content.Materialization{
		RepoID:       "test-repo",
		ScopeID:      "test-scope",
		GenerationID: "test-gen",
		Records:      records,
	}

	result, err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if got, want := result.RecordCount, 3; got != want {
		t.Fatalf("RecordCount = %d, want %d", got, want)
	}

	// Should have one stale-reference delete and one batched file insert.
	assertExecCountWithFingerprintReaps(t, db.execs, 2, 0, 0, "reference delete + batched insert")
	if !strings.Contains(db.execs[0].query, "DELETE FROM content_file_references") {
		t.Fatalf("first query should delete stale file references: %s", db.execs[0].query)
	}

	// Query should be a multi-row INSERT
	query := db.execs[1].query
	if !strings.Contains(query, "INSERT INTO content_files") {
		t.Fatalf("query should contain content_files insert: %s", query)
	}
	// Count the number of value placeholders - should have 3 sets
	valueGroups := strings.Count(query, "($")
	if got, want := valueGroups, 3; got != want {
		t.Fatalf("value groups = %d, want %d (one per record)", got, want)
	}
}

func TestContentWriterBatchesEntityInserts(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(db))
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	// Create 2 entity records
	entities := []content.EntityRecord{
		{
			EntityID:   "entity-1",
			Path:       "main.go",
			EntityType: "function",
			EntityName: "main",
			StartLine:  1,
			EndLine:    10,
		},
		{
			EntityID:   "entity-2",
			Path:       "util.go",
			EntityType: "function",
			EntityName: "helper",
			StartLine:  5,
			EndLine:    15,
		},
	}

	mat := content.Materialization{
		RepoID:       "test-repo",
		ScopeID:      "test-scope",
		GenerationID: "test-gen",
		Entities:     entities,
	}

	result, err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if got, want := result.EntityCount, 2; got != want {
		t.Fatalf("EntityCount = %d, want %d", got, want)
	}

	// Batched insert + the stale-entity reap (#5329 content_entities reap).
	assertExecCountWithFingerprintReaps(t, db.execs, 2, 1, 1, "batched insert + reap")

	query := db.execs[0].query
	if !strings.Contains(query, "INSERT INTO content_entities") {
		t.Fatalf("query should contain content_entities insert: %s", query)
	}
	valueGroups := strings.Count(query, "($")
	if got, want := valueGroups, 2; got != want {
		t.Fatalf("value groups = %d, want %d (one per entity)", got, want)
	}

	// Reap DELETE, anti-joined against both fresh ids so neither is reaped.
	// It runs fourth: the two withdrawn fingerprint deletes (entities carry
	// no fingerprint keys) precede it in the same Write.
	reapQuery := db.execs[3].query
	if !strings.Contains(reapQuery, "DELETE FROM content_entities") || !strings.Contains(reapQuery, "entity_id <> ALL") {
		t.Fatalf("fourth query should be the stale-entity reap: %s", reapQuery)
	}
}

func TestContentWriterLogsStageTimings(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	var logs bytes.Buffer
	writer := NewContentWriter(withTransactions(db))
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }
	writer.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	mat := content.Materialization{
		RepoID:       "test-repo",
		ScopeID:      "test-scope",
		GenerationID: "test-gen",
		Records: []content.Record{
			{Path: "main.go", Body: "package main\n", Metadata: map[string]string{"language": "go"}},
		},
		Entities: []content.EntityRecord{
			{
				EntityID:   "entity-1",
				Path:       "main.go",
				EntityType: "function",
				EntityName: "main",
				StartLine:  1,
				EndLine:    2,
			},
		},
	}

	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	got := logs.String()
	for _, want := range []string{
		`"msg":"content writer stage completed"`,
		`"stage":"prepare_files"`,
		`"stage":"upsert_files"`,
		`"stage":"prepare_entities"`,
		`"stage":"upsert_entities"`,
		`"scope_id":"test-scope"`,
		`"generation_id":"test-gen"`,
		`"repo_id":"test-repo"`,
		`"row_count":1`,
		`"batch_count":1`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("logs missing %s:\n%s", want, got)
		}
	}
}

func TestContentWriterBatchesSmallTombstoneDelete(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(db))
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	// Create 1 deleted record — the batched path still issues one DELETE
	// per table (the batch is just 1 row), preserving the same entity→
	// reference→file order.
	records := []content.Record{
		{Path: "deleted.go", Deleted: true},
	}

	mat := content.Materialization{
		RepoID:       "test-repo",
		ScopeID:      "test-scope",
		GenerationID: "test-gen",
		Records:      records,
	}

	result, err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if got, want := result.DeletedCount, 1; got != want {
		t.Fatalf("DeletedCount = %d, want %d", got, want)
	}

	// Deletes should remove entities, indexed file references, and the file row.
	assertExecCountWithFingerprintReaps(t, db.execs, 3, 0, 0, "3 batched deletes")
	if !strings.Contains(db.execs[0].query, "DELETE FROM content_entities") {
		t.Fatalf("first query should delete entities: %s", db.execs[0].query)
	}
	if !strings.Contains(db.execs[1].query, "DELETE FROM content_file_references") {
		t.Fatalf("second query should delete file references: %s", db.execs[1].query)
	}
	if !strings.Contains(db.execs[2].query, "DELETE FROM content_files") {
		t.Fatalf("third query should delete files: %s", db.execs[2].query)
	}

	// Each tombstone DELETE must use the batched IN (...) syntax (the
	// fingerprint reap trailer uses anti-joins instead, asserted above).
	for _, e := range db.execs[:3] {
		if !strings.Contains(e.query, "IN (") {
			t.Fatalf("DELETE query should use batched IN (...) syntax: %s", e.query)
		}
	}
}

func TestContentWriterMaterializesHostnameReferences(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(db))
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	_, err := writer.Write(context.Background(), content.Materialization{
		RepoID:       "repo-service",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		Records: []content.Record{
			{
				Path: "deploy/ingress.yaml",
				Body: "rules:\n- host: api.qa.example.test\n- host: docs.example.test\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	assertExecCountWithFingerprintReaps(t, db.execs, 3, 0, 0, "reference delete + file insert + reference upsert")
	if !strings.Contains(db.execs[0].query, "DELETE FROM content_file_references") {
		t.Fatalf("first query = %q, want stale content_file_references delete", db.execs[0].query)
	}
	if !strings.Contains(db.execs[1].query, "INSERT INTO content_files") {
		t.Fatalf("second query = %q, want content_files upsert", db.execs[1].query)
	}
	if !strings.Contains(db.execs[2].query, "INSERT INTO content_file_references") {
		t.Fatalf("third query = %q, want content_file_references upsert", db.execs[2].query)
	}
	if got, want := strings.Count(db.execs[2].query, "($"), 2; got != want {
		t.Fatalf("reference value groups = %d, want %d", got, want)
	}
	args := db.execs[2].args
	for _, want := range []string{"repo-service", "deploy/ingress.yaml", "hostname", "api.qa.example.test", "docs.example.test"} {
		if !fakeExecArgsContain(args, want) {
			t.Fatalf("reference upsert args missing %q: %#v", want, args)
		}
	}
}

func TestContentWriterMaterializesServiceNameReferences(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(db))
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	_, err := writer.Write(context.Background(), content.Materialization{
		RepoID:       "repo-service",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		Records: []content.Record{
			{
				Path: "deploy/values.yaml",
				Body: "service:\n  name: sample-service-api\n  url: https://sample-service-api.qa.example.test\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	assertExecCountWithFingerprintReaps(t, db.execs, 3, 0, 0, "reference delete + file insert + reference upsert")
	if !strings.Contains(db.execs[2].query, "INSERT INTO content_file_references") {
		t.Fatalf("third query = %q, want content_file_references upsert", db.execs[2].query)
	}
	args := db.execs[2].args
	for _, want := range []string{"hostname", "sample-service-api.qa.example.test", "service_name", "sample-service-api"} {
		if !fakeExecArgsContain(args, want) {
			t.Fatalf("reference upsert args missing %q: %#v", want, args)
		}
	}
}

func TestContentWriterBatchesLargeFileSet(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(db))
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	// Create 1000 file records (should result in 2 batches: 500 + 500)
	records := make([]content.Record, 1000)
	for i := 0; i < 1000; i++ {
		records[i] = content.Record{
			Path:     "file" + strings.Repeat("x", i%10) + ".go",
			Body:     "content" + strings.Repeat("x", i%10),
			Metadata: map[string]string{"language": "go"},
		}
	}

	mat := content.Materialization{
		RepoID:       "test-repo",
		ScopeID:      "test-scope",
		GenerationID: "test-gen",
		Records:      records,
	}

	result, err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if got, want := result.RecordCount, 1000; got != want {
		t.Fatalf("RecordCount = %d, want %d", got, want)
	}

	// Should have a stale-reference delete and file insert per batch.
	assertExecCountWithFingerprintReaps(t, db.execs, 4, 0, 0, "reference delete + insert per batch")

	// Both queries should be multi-row INSERTs
	for i, execIndex := range []int{1, 3} {
		exec := db.execs[execIndex]
		if !strings.Contains(exec.query, "INSERT INTO content_files") {
			t.Fatalf("query %d should contain content_files insert", i)
		}
		valueGroups := strings.Count(exec.query, "($")
		if got, want := valueGroups, 500; got != want {
			t.Fatalf("batch %d: value groups = %d, want %d", i, got, want)
		}
	}
}

func fakeExecArgsContain(args []any, want string) bool {
	for _, arg := range args {
		if value, ok := arg.(string); ok && value == want {
			return true
		}
	}
	return false
}

func TestContentWriterBatchesLargeEntitySet(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(db))
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	// Create 600 entity records (should result in 2 batches: 300 + 300)
	entities := make([]content.EntityRecord, 600)
	for i := 0; i < 600; i++ {
		entities[i] = content.EntityRecord{
			// One unique entity_id per row so the batch-fan-out gate exercises
			// real batch boundaries instead of being collapsed by the dedup pass
			// in ContentWriter.Write (see deduplicateEntityRows in
			// content_writer_batch.go).
			EntityID:   fmt.Sprintf("entity-%d", i),
			Path:       "file.go",
			EntityType: "function",
			EntityName: "func" + strings.Repeat("x", i%10),
			StartLine:  i + 1,
			EndLine:    i + 10,
		}
	}

	mat := content.Materialization{
		RepoID:       "test-repo",
		ScopeID:      "test-scope",
		GenerationID: "test-gen",
		Entities:     entities,
	}

	result, err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if got, want := result.EntityCount, 600; got != want {
		t.Fatalf("EntityCount = %d, want %d", got, want)
	}

	// 2 insert batches of 300 + 1 reap DELETE (600 entities share one path).
	// All 600 carry no fingerprint keys, so the withdrawn converges split
	// 500+100 at contentFileBatchSize: 2 scoped fp deletes + 2 scoped band
	// deletes (see TestDeleteWithdrawnFingerprintsChunksAtFileBatchSize for
	// the bound pin; this test pins the Write-level statement shape).
	assertExecCountWithFingerprintReaps(t, db.execs, 3, 2, 2, "2 insert batches + reap + 500/100 withdrawn chunks")

	for i, exec := range db.execs[:2] {
		if !strings.Contains(exec.query, "INSERT INTO content_entities") {
			t.Fatalf("query %d should contain content_entities insert", i)
		}
		valueGroups := strings.Count(exec.query, "($")
		if got, want := valueGroups, 300; got != want {
			t.Fatalf("batch %d: value groups = %d, want %d", i, got, want)
		}
	}

	// The stale-entity reap runs seventh: two entity insert batches, then
	// the four withdrawn fingerprint deletes (fp+band per 500/100 chunk;
	// entities carry no keys). Chunk deletes run serially after the insert
	// batches join, so the index is deterministic.
	reapQuery := db.execs[6].query
	if !strings.Contains(reapQuery, "DELETE FROM content_entities") || !strings.Contains(reapQuery, "entity_id <> ALL") {
		t.Fatalf("fifth query should be the stale-entity reap: %s", reapQuery)
	}
}

func TestContentWriterUsesCustomEntityBatchSize(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(withTransactions(db)).WithEntityBatchSize(200)
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	entities := make([]content.EntityRecord, 450)
	for i := 0; i < 450; i++ {
		entities[i] = content.EntityRecord{
			// One unique entity_id per row so the batch-fan-out gate exercises
			// real batch boundaries instead of being collapsed by the dedup pass
			// in ContentWriter.Write (see deduplicateEntityRows in
			// content_writer_batch.go).
			EntityID:   fmt.Sprintf("entity-%d", i),
			Path:       "file.go",
			EntityType: "function",
			EntityName: "func" + strings.Repeat("x", i%10),
			StartLine:  i + 1,
			EndLine:    i + 10,
		}
	}

	_, err := writer.Write(context.Background(), content.Materialization{
		RepoID:       "test-repo",
		ScopeID:      "test-scope",
		GenerationID: "test-gen",
		Entities:     entities,
	})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	// 3 insert batches (200, 200, 50) + 1 reap DELETE (#5329 content_entities
	// reap; all 450 entities share one path, reaped in a single chunk).
	assertExecCountWithFingerprintReaps(t, db.execs, 4, 1, 1, "3 insert batches + 1 reap DELETE")
	// Entity batches fan out to runConcurrentBatches (nondeterministic
	// order): assert the sorted multiset of insert batch sizes, and the
	// reap DELETE separately since its $2/$3 array params read as "($2"/
	// "($3" substrings that would otherwise pollute the "($" count.
	var insertBatchSizes []int
	reapCount := 0
	for _, exec := range db.execs {
		switch {
		case strings.Contains(exec.query, "INSERT INTO content_entities"):
			insertBatchSizes = append(insertBatchSizes, strings.Count(exec.query, "($"))
		case strings.Contains(exec.query, "DELETE FROM content_entities") && strings.Contains(exec.query, "entity_id <> ALL"):
			reapCount++
		case strings.Contains(exec.query, "DELETE FROM code_function_fingerprint"),
			strings.Contains(exec.query, "DELETE FROM code_fingerprint_band"):
			// Unconditional fingerprint side-table reaps; counted by the
			// helper above, not by this batch-shape switch.
		default:
			t.Fatalf("unexpected query: %s", exec.query)
		}
	}
	if reapCount != 1 {
		t.Fatalf("reap exec count = %d, want 1", reapCount)
	}
	sort.Ints(insertBatchSizes)
	want := []int{50, 200, 200}
	if len(insertBatchSizes) != len(want) {
		t.Fatalf("insert batch size set = %v, want %v", insertBatchSizes, want)
	}
	for i, w := range want {
		if insertBatchSizes[i] != w {
			t.Fatalf("insert batch size set = %v, want %v", insertBatchSizes, want)
		}
	}
}
