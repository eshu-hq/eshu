package graph

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBatchMergeEntitiesUIDRows(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}
	rows := []BatchEntityRow{
		{
			FilePath:   "/repo/main.go",
			Name:       "main",
			LineNumber: 1,
			UID:        "func:main",
			Extra:      map[string]any{"lang": "go"},
		},
		{
			FilePath:   "/repo/main.go",
			Name:       "init",
			LineNumber: 5,
			UID:        "func:init",
			Extra:      map[string]any{"lang": "go"},
		},
	}

	err := BatchMergeEntities(context.Background(), executor, "Function", rows, DefaultBatchSize)
	if err != nil {
		t.Fatalf("BatchMergeEntities() error = %v, want nil", err)
	}

	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MERGE (n:Function {uid: row.uid})") {
		t.Fatalf("cypher missing UID merge: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "n.`lang` = row.`lang`") {
		t.Fatalf("cypher missing extra property: %s", executor.calls[0].Cypher)
	}

	batchRows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows type = %T, want []map[string]any", executor.calls[0].Parameters["rows"])
	}
	if len(batchRows) != 2 {
		t.Fatalf("batch rows = %d, want 2", len(batchRows))
	}
}

func TestBatchMergeEntitiesNameRows(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}
	rows := []BatchEntityRow{
		{
			FilePath:   "/repo/app.py",
			Name:       "MyClass",
			LineNumber: 10,
		},
	}

	err := BatchMergeEntities(context.Background(), executor, "Class", rows, DefaultBatchSize)
	if err != nil {
		t.Fatalf("BatchMergeEntities() error = %v, want nil", err)
	}

	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "name: row.name, path: row.file_path, line_number: row.line_number") {
		t.Fatalf("cypher missing name identity merge: %s", executor.calls[0].Cypher)
	}
}

func TestBatchMergeEntitiesMixedIdentity(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}
	rows := []BatchEntityRow{
		{FilePath: "/repo/a.go", Name: "Foo", LineNumber: 1, UID: "func:Foo"},
		{FilePath: "/repo/b.go", Name: "Bar", LineNumber: 2},
	}

	err := BatchMergeEntities(context.Background(), executor, "Function", rows, DefaultBatchSize)
	if err != nil {
		t.Fatalf("BatchMergeEntities() error = %v, want nil", err)
	}

	// Should produce two separate queries: one for UID, one for name.
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
}

func TestBatchMergeEntitiesChunking(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}
	rows := make([]BatchEntityRow, 5)
	for i := range rows {
		rows[i] = BatchEntityRow{
			FilePath:   "/repo/main.go",
			Name:       "fn",
			LineNumber: i + 1,
			UID:        "uid",
		}
	}

	// Batch size of 2 should create 3 chunks: [2, 2, 1].
	err := BatchMergeEntities(context.Background(), executor, "Function", rows, 2)
	if err != nil {
		t.Fatalf("BatchMergeEntities() error = %v, want nil", err)
	}

	if got, want := len(executor.calls), 3; got != want {
		t.Fatalf("executor calls = %d, want %d (chunked)", got, want)
	}
}

func TestBatchMergeEntitiesEmptyRows(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}
	err := BatchMergeEntities(context.Background(), executor, "Function", nil, DefaultBatchSize)
	if err != nil {
		t.Fatalf("error = %v, want nil for empty rows", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %d, want 0", len(executor.calls))
	}
}

func TestBatchMergeEntitiesRejectsInvalidLabel(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}
	err := BatchMergeEntities(context.Background(), executor, "bad label", []BatchEntityRow{
		{FilePath: "/f", Name: "n", LineNumber: 1},
	}, DefaultBatchSize)
	if err == nil {
		t.Fatal("error = nil, want non-nil for invalid label")
	}
}

func TestBatchMergeEntitiesRequiresExecutor(t *testing.T) {
	t.Parallel()

	err := BatchMergeEntities(context.Background(), nil, "Function", []BatchEntityRow{
		{FilePath: "/f", Name: "n", LineNumber: 1},
	}, DefaultBatchSize)
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "executor is required") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBatchMergeEntitiesPropagatesError(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{errAtCall: errors.New("write failed")}
	err := BatchMergeEntities(context.Background(), executor, "Function", []BatchEntityRow{
		{FilePath: "/f", Name: "n", LineNumber: 1},
	}, DefaultBatchSize)
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBatchMergeEntitiesDefaultBatchSize(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}
	rows := []BatchEntityRow{
		{FilePath: "/f", Name: "n", LineNumber: 1},
	}

	// Pass batchSize=0 to trigger default.
	err := BatchMergeEntities(context.Background(), executor, "Function", rows, 0)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("executor calls = %d, want 1", len(executor.calls))
	}
}

func TestBatchMergeFiles(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}
	rows := []BatchFileRow{
		{
			FilePath:     "/repo/main.go",
			Name:         "main.go",
			RelativePath: "main.go",
			Language:     "go",
			IsDependency: false,
		},
		{
			FilePath:     "/repo/lib/util.go",
			Name:         "util.go",
			RelativePath: "lib/util.go",
			Language:     "go",
			IsDependency: true,
		},
	}

	err := BatchMergeFiles(context.Background(), executor, rows, DefaultBatchSize)
	if err != nil {
		t.Fatalf("BatchMergeFiles() error = %v, want nil", err)
	}

	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MERGE (f:File {path: row.file_path})") {
		t.Fatalf("cypher missing File merge: %s", executor.calls[0].Cypher)
	}
}

func TestBatchMergeFilesEmpty(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}
	err := BatchMergeFiles(context.Background(), executor, nil, DefaultBatchSize)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %d, want 0", len(executor.calls))
	}
}

func TestBatchMergeFilesRequiresExecutor(t *testing.T) {
	t.Parallel()

	err := BatchMergeFiles(context.Background(), nil, []BatchFileRow{
		{FilePath: "/f", Name: "f"},
	}, DefaultBatchSize)
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
}

func TestBatchMergeRelationships(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}
	rows := []BatchRelationshipRow{
		{
			SourceLabel: "Class",
			SourceKey:   map[string]any{"name": "Child", "path": "/repo/a.py"},
			TargetLabel: "Class",
			TargetKey:   map[string]any{"name": "Parent", "path": "/repo/b.py"},
			RelType:     "INHERITS",
		},
	}

	err := BatchMergeRelationships(context.Background(), executor, rows, DefaultBatchSize)
	if err != nil {
		t.Fatalf("BatchMergeRelationships() error = %v, want nil", err)
	}

	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MERGE (src)-[rel:INHERITS]->(tgt)") {
		t.Fatalf("cypher missing INHERITS merge: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (src:Class") {
		t.Fatalf("cypher missing source match: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (tgt:Class") {
		t.Fatalf("cypher missing target match: %s", executor.calls[0].Cypher)
	}
}

func TestBatchMergeRelationshipsWithProperties(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}
	rows := []BatchRelationshipRow{
		{
			SourceLabel: "Function",
			SourceKey:   map[string]any{"name": "caller", "path": "/repo/a.go"},
			TargetLabel: "Function",
			TargetKey:   map[string]any{"name": "callee", "path": "/repo/b.go"},
			RelType:     "CALLS",
			RelProps:    map[string]any{"confidence": 0.95},
		},
	}

	err := BatchMergeRelationships(context.Background(), executor, rows, DefaultBatchSize)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}

	if !strings.Contains(executor.calls[0].Cypher, "rel.`confidence` = row.rel_confidence") {
		t.Fatalf("cypher missing rel property SET: %s", executor.calls[0].Cypher)
	}
}

func TestBatchMergeRelationshipsEmpty(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}
	err := BatchMergeRelationships(context.Background(), executor, nil, DefaultBatchSize)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %d, want 0", len(executor.calls))
	}
}

func TestBatchMergeRelationshipsRequiresExecutor(t *testing.T) {
	t.Parallel()

	err := BatchMergeRelationships(context.Background(), nil, []BatchRelationshipRow{
		{
			SourceLabel: "A",
			SourceKey:   map[string]any{"name": "a"},
			TargetLabel: "B",
			TargetKey:   map[string]any{"name": "b"},
			RelType:     "REL",
		},
	}, DefaultBatchSize)
	if err == nil {
		t.Fatal("error = nil, want non-nil")
	}
}

func TestBatchMergeRelationshipsRejectsInvalidLabels(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}

	tests := []struct {
		name        string
		sourceLabel string
		targetLabel string
		relType     string
	}{
		{"bad source", "bad label", "Target", "REL"},
		{"bad target", "Source", "bad label", "REL"},
		{"bad reltype", "Source", "Target", "bad rel"},
	}
	for _, tt := range tests {
		err := BatchMergeRelationships(context.Background(), executor, []BatchRelationshipRow{
			{
				SourceLabel: tt.sourceLabel,
				SourceKey:   map[string]any{"name": "a"},
				TargetLabel: tt.targetLabel,
				TargetKey:   map[string]any{"name": "b"},
				RelType:     tt.relType,
			},
		}, DefaultBatchSize)
		if err == nil {
			t.Errorf("%s: error = nil, want non-nil", tt.name)
		}
	}
}

func TestBatchMergeRelationshipsChunking(t *testing.T) {
	t.Parallel()

	executor := &batchRecordingExecutor{}
	rows := make([]BatchRelationshipRow, 5)
	for i := range rows {
		rows[i] = BatchRelationshipRow{
			SourceLabel: "A",
			SourceKey:   map[string]any{"name": "a"},
			TargetLabel: "B",
			TargetKey:   map[string]any{"name": "b"},
			RelType:     "REL",
		}
	}

	err := BatchMergeRelationships(context.Background(), executor, rows, 2)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got, want := len(executor.calls), 3; got != want {
		t.Fatalf("executor calls = %d, want %d (chunked)", got, want)
	}
}

func TestCollectExtraKeys(t *testing.T) {
	t.Parallel()

	rows := []BatchEntityRow{
		{Extra: map[string]any{"lang": "go", "source": "code"}},
		{Extra: map[string]any{"lang": "py", "docstring": "doc"}},
		{Extra: nil},
	}
	keys := collectExtraKeys(rows)
	if len(keys) != 3 {
		t.Fatalf("len = %d, want 3", len(keys))
	}
	// Should be sorted.
	if keys[0] != "docstring" || keys[1] != "lang" || keys[2] != "source" {
		t.Fatalf("keys = %v, want [docstring lang source]", keys)
	}
}

func TestSortStringSet(t *testing.T) {
	t.Parallel()

	result := sortStringSet(map[string]struct{}{
		"zebra": {},
		"alpha": {},
		"mid":   {},
	})
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	if result[0] != "alpha" || result[1] != "mid" || result[2] != "zebra" {
		t.Fatalf("result = %v", result)
	}
}

func TestSortStringSetEmpty(t *testing.T) {
	t.Parallel()

	result := sortStringSet(nil)
	if result != nil {
		t.Fatalf("result = %v, want nil", result)
	}
}

type batchRecordingExecutor struct {
	calls     []CypherStatement
	errAtCall error
}

func (r *batchRecordingExecutor) ExecuteCypher(_ context.Context, stmt CypherStatement) error {
	r.calls = append(r.calls, stmt)
	if r.errAtCall != nil {
		return r.errAtCall
	}
	return nil
}
