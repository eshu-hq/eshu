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

func TestContentWriterBatchesFileInserts(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
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
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d (reference delete + batched insert)", got, want)
	}
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
	writer := NewContentWriter(db)
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

	// Should have exactly 1 exec call (batched insert)
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d (batched)", got, want)
	}

	// Query should be a multi-row INSERT
	query := db.execs[0].query
	if !strings.Contains(query, "INSERT INTO content_entities") {
		t.Fatalf("query should contain content_entities insert: %s", query)
	}
	valueGroups := strings.Count(query, "($")
	if got, want := valueGroups, 2; got != want {
		t.Fatalf("value groups = %d, want %d (one per entity)", got, want)
	}
}

func TestContentWriterLogsStageTimings(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	var logs bytes.Buffer
	writer := NewContentWriter(db)
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

func TestContentWriterDeletesAreNotBatched(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	// Create 1 deleted record
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
	if got, want := len(db.execs), 3; got != want {
		t.Fatalf("exec count = %d, want %d (3 deletes)", got, want)
	}
	if !strings.Contains(db.execs[0].query, "DELETE FROM content_entities") {
		t.Fatalf("first query should delete entities: %s", db.execs[0].query)
	}
	if !strings.Contains(db.execs[1].query, "DELETE FROM content_file_references") {
		t.Fatalf("second query should delete file references: %s", db.execs[1].query)
	}
	if !strings.Contains(db.execs[2].query, "DELETE FROM content_files") {
		t.Fatalf("third query should delete files: %s", db.execs[2].query)
	}
}

func TestContentWriterMaterializesHostnameReferences(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
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

	if got, want := len(db.execs), 3; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
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
	writer := NewContentWriter(db)
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

	if got, want := len(db.execs), 3; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
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
	writer := NewContentWriter(db)
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
	if got, want := len(db.execs), 4; got != want {
		t.Fatalf("exec count = %d, want %d (reference delete + insert per batch)", got, want)
	}

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
	writer := NewContentWriter(db)
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

	// Should have exactly 2 exec calls (2 batches of 300)
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d (2 batches)", got, want)
	}

	// Both queries should be multi-row INSERTs
	for i, exec := range db.execs {
		if !strings.Contains(exec.query, "INSERT INTO content_entities") {
			t.Fatalf("query %d should contain content_entities insert", i)
		}
		valueGroups := strings.Count(exec.query, "($")
		if got, want := valueGroups, 300; got != want {
			t.Fatalf("batch %d: value groups = %d, want %d", i, got, want)
		}
	}
}

func TestContentWriterUsesCustomEntityBatchSize(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(db).WithEntityBatchSize(200)
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
	if got, want := len(db.execs), 3; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	// Entity batches now fan out to runConcurrentBatches; the absolute order
	// of value-group counts in db.execs is not deterministic. Assert the
	// sorted multiset of batch sizes instead so the test still gates batch
	// sizing without baking in the prior serial-order implementation detail.
	got := make([]int, len(db.execs))
	for i, exec := range db.execs {
		got[i] = strings.Count(exec.query, "($")
	}
	sort.Ints(got)
	want := []int{50, 200, 200}
	if len(got) != len(want) {
		t.Fatalf("batch size set = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("batch size set = %v, want %v", got, want)
		}
	}
}
