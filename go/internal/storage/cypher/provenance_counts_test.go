// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubProvenanceReader is a test double for ProvenanceCountReader that returns
// the same rows for every query.
type stubProvenanceReader struct {
	rows []map[string]any
	err  error
}

func (s *stubProvenanceReader) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return s.rows, s.err
}

// cypherAwareReader returns rows only for queries whose text contains a given
// substring, and records every query it saw. It lets the edge tests model the
// per-relationship-type aggregation (one query per verb) precisely.
type cypherAwareReader struct {
	match   string
	rows    []map[string]any
	queries []string
	lastErr error
}

func (c *cypherAwareReader) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	c.queries = append(c.queries, cypher)
	if c.lastErr != nil {
		return nil, c.lastErr
	}
	if c.match == "" || strings.Contains(cypher, c.match) {
		return c.rows, nil
	}
	return nil, nil
}

// TestEdgesBySourceToolNilReader reports an error when the reader is nil.
func TestEdgesBySourceToolNilReader(t *testing.T) {
	t.Parallel()
	store := &ProvenanceCountStore{}
	_, err := store.EdgesBySourceTool(context.Background())
	if err == nil {
		t.Fatal("expected error for nil reader, got nil")
	}
}

// TestFilesByLanguageNilReader reports an error when the reader is nil.
func TestFilesByLanguageNilReader(t *testing.T) {
	t.Parallel()
	store := &ProvenanceCountStore{}
	_, err := store.FilesByLanguage(context.Background())
	if err == nil {
		t.Fatal("expected error for nil reader, got nil")
	}
}

// TestEdgesBySourceToolReaderError propagates the reader error.
func TestEdgesBySourceToolReaderError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("graph unavailable")
	store := NewProvenanceCountStore(&stubProvenanceReader{err: wantErr})
	_, err := store.EdgesBySourceTool(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("EdgesBySourceTool() error = %v, want wrapping %v", err, wantErr)
	}
}

// TestFilesByLanguageReaderError propagates the reader error.
func TestFilesByLanguageReaderError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("graph unavailable")
	store := NewProvenanceCountStore(&stubProvenanceReader{err: wantErr})
	_, err := store.FilesByLanguage(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("FilesByLanguage() error = %v, want wrapping %v", err, wantErr)
	}
}

// TestEdgesBySourceToolCounts aggregates exact per-verb counts keyed by
// source_tool. Rows are returned only for the DEPENDS_ON aggregate.
func TestEdgesBySourceToolCounts(t *testing.T) {
	t.Parallel()
	reader := &cypherAwareReader{
		match: "[r:DEPENDS_ON]",
		rows: []map[string]any{
			{"source_tool": "terraform", "cnt": int64(42)},
			{"source_tool": "ansible", "cnt": int64(7)},
		},
	}
	store := NewProvenanceCountStore(reader)
	got, err := store.EdgesBySourceTool(context.Background())
	if err != nil {
		t.Fatalf("EdgesBySourceTool() error = %v", err)
	}
	if got["terraform"] != 42 {
		t.Errorf("terraform count = %d, want 42", got["terraform"])
	}
	if got["ansible"] != 7 {
		t.Errorf("ansible count = %d, want 7", got["ansible"])
	}
}

// TestEdgesBySourceToolMergesAcrossVerbs proves counts for the same tool are
// summed across the Tier-2 verbs (a tool appears on multiple relationship types).
func TestEdgesBySourceToolMergesAcrossVerbs(t *testing.T) {
	t.Parallel()
	// Return one terraform edge for EVERY verb query, so the merged total equals
	// the number of source_tool-bearing verbs.
	reader := &cypherAwareReader{rows: []map[string]any{{"source_tool": "terraform", "cnt": int64(1)}}}
	store := NewProvenanceCountStore(reader)
	got, err := store.EdgesBySourceTool(context.Background())
	if err != nil {
		t.Fatalf("EdgesBySourceTool() error = %v", err)
	}
	if want := int64(len(sourceToolEdgeVerbs)); got["terraform"] != want {
		t.Errorf("terraform merged count = %d, want %d (one per verb)", got["terraform"], want)
	}
}

// TestEdgesBySourceToolCypherIsTypeAnchored is the bounded-read guard: the edge
// query must be relationship-type anchored (index-answered) and must NOT use the
// forbidden unanchored all-edge scan or a row-sampling LIMIT.
func TestEdgesBySourceToolCypherIsTypeAnchored(t *testing.T) {
	t.Parallel()
	reader := &cypherAwareReader{}
	store := NewProvenanceCountStore(reader)
	if _, err := store.EdgesBySourceTool(context.Background()); err != nil {
		t.Fatalf("EdgesBySourceTool() error = %v", err)
	}
	if len(reader.queries) != len(sourceToolEdgeVerbs) {
		t.Fatalf("ran %d queries, want one per verb (%d)", len(reader.queries), len(sourceToolEdgeVerbs))
	}
	for _, q := range reader.queries {
		if strings.Contains(q, "MATCH ()-[r]->") {
			t.Errorf("edge query uses the forbidden unanchored all-edge scan:\n%s", q)
		}
		if strings.Contains(q, "LIMIT") {
			t.Errorf("edge query must not row-sample with LIMIT (counts must be exact):\n%s", q)
		}
		if !strings.Contains(q, "[r:") {
			t.Errorf("edge query must be relationship-type anchored:\n%s", q)
		}
	}
}

// TestFilesByLanguageCounts returns exact File counts keyed by language.
func TestFilesByLanguageCounts(t *testing.T) {
	t.Parallel()
	reader := &stubProvenanceReader{
		rows: []map[string]any{
			{"language": "go", "cnt": int64(100)},
			{"language": "python", "cnt": int64(30)},
		},
	}
	store := NewProvenanceCountStore(reader)
	got, err := store.FilesByLanguage(context.Background())
	if err != nil {
		t.Fatalf("FilesByLanguage() error = %v", err)
	}
	if got["go"] != 100 {
		t.Errorf("go count = %d, want 100", got["go"])
	}
	if got["python"] != 30 {
		t.Errorf("python count = %d, want 30", got["python"])
	}
}

// TestFilesByLanguageCypherIsLabelAnchoredGroupLimited guards the file query:
// File-label anchored, groups by language, and the LIMIT bounds the returned
// GROUPS (after RETURN), not the rows counted — so per-language counts stay exact.
func TestFilesByLanguageCypherIsLabelAnchoredGroupLimited(t *testing.T) {
	t.Parallel()
	reader := &cypherAwareReader{}
	store := NewProvenanceCountStore(reader)
	if _, err := store.FilesByLanguage(context.Background()); err != nil {
		t.Fatalf("FilesByLanguage() error = %v", err)
	}
	q := reader.queries[0]
	if !strings.Contains(q, "MATCH (f:File)") {
		t.Errorf("file query must be File-label anchored:\n%s", q)
	}
	// LIMIT must follow RETURN (group cap), not a pre-aggregation `WITH ... LIMIT`
	// row sample.
	if strings.Contains(q, "WITH f.language") && strings.Contains(q, "WITH f.language AS language, f\nLIMIT") {
		t.Errorf("file query must not row-sample before grouping:\n%s", q)
	}
	if !strings.Contains(q, "count(f) AS cnt\nORDER BY cnt DESC\nLIMIT $limit") {
		t.Errorf("file query must group-then-LIMIT (cap returned groups, not rows):\n%s", q)
	}
}

// TestEdgesBySourceToolEmptyRows returns an empty map when no rows returned.
func TestEdgesBySourceToolEmptyRows(t *testing.T) {
	t.Parallel()
	store := NewProvenanceCountStore(&stubProvenanceReader{rows: nil})
	got, err := store.EdgesBySourceTool(context.Background())
	if err != nil {
		t.Fatalf("EdgesBySourceTool() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty map, got %v", got)
	}
}

// TestProvenanceCountStoreGroupLimitDefault verifies the default group limit
// applies to the file query when GroupLimit is not set.
func TestProvenanceCountStoreGroupLimitDefault(t *testing.T) {
	t.Parallel()
	var capturedParams map[string]any
	reader := &captureParamsReader{
		onRun: func(_ string, params map[string]any) ([]map[string]any, error) {
			capturedParams = params
			return nil, nil
		},
	}
	store := NewProvenanceCountStore(reader)
	if _, err := store.FilesByLanguage(context.Background()); err != nil {
		t.Fatalf("FilesByLanguage() error = %v", err)
	}
	if capturedParams["limit"] != defaultProvenanceGroupLimit {
		t.Errorf("limit param = %v, want %d", capturedParams["limit"], defaultProvenanceGroupLimit)
	}
}

// TestProvenanceCountStoreCustomGroupLimit verifies GroupLimit is forwarded.
func TestProvenanceCountStoreCustomGroupLimit(t *testing.T) {
	t.Parallel()
	var capturedParams map[string]any
	reader := &captureParamsReader{
		onRun: func(_ string, params map[string]any) ([]map[string]any, error) {
			capturedParams = params
			return nil, nil
		},
	}
	store := &ProvenanceCountStore{Reader: reader, GroupLimit: 500}
	if _, err := store.FilesByLanguage(context.Background()); err != nil {
		t.Fatalf("FilesByLanguage() error = %v", err)
	}
	if capturedParams["limit"] != 500 {
		t.Errorf("limit param = %v, want 500", capturedParams["limit"])
	}
}

// TestFilesByLanguageSkipsEmptyKeys skips rows with an empty language key.
func TestFilesByLanguageSkipsEmptyKeys(t *testing.T) {
	t.Parallel()
	reader := &stubProvenanceReader{
		rows: []map[string]any{
			{"language": "", "cnt": int64(5)},
			{"language": "go", "cnt": int64(10)},
		},
	}
	store := NewProvenanceCountStore(reader)
	got, err := store.FilesByLanguage(context.Background())
	if err != nil {
		t.Fatalf("FilesByLanguage() error = %v", err)
	}
	if _, ok := got[""]; ok {
		t.Error("empty language key must not appear in the result map")
	}
	if got["go"] != 10 {
		t.Errorf("go count = %d, want 10", got["go"])
	}
}

// captureParamsReader captures the last query parameters passed to Run.
type captureParamsReader struct {
	onRun func(cypher string, params map[string]any) ([]map[string]any, error)
}

func (c *captureParamsReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return c.onRun(cypher, params)
}
