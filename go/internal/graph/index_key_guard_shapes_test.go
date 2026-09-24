// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"strings"
	"sync"
	"testing"
)

// oversizedRows returns one row map carrying a 9000-byte value under field.
func oversizedRows(field string) map[string]any {
	return map[string]any{"rows": []map[string]any{{"uid": "u", field: strings.Repeat("x", 9000)}}}
}

// TestGuardIndexKeyWritesHandlesRecognizedSpellings pins the write spellings
// the analyzer reads beyond the canonical uppercase form: each writes a 9000
// byte indexed value and must drop the row.
func TestGuardIndexKeyWritesHandlesRecognizedSpellings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		cypher string
		params map[string]any
	}{
		{
			name:   "lowercase keywords",
			cypher: "unwind $rows as row\nmerge (m:Module {name: row.name})",
			params: oversizedRows("name"),
		},
		{
			name:   "backtick label",
			cypher: "UNWIND $rows AS row\nMERGE (m:`Module` {name: row.name})",
			params: oversizedRows("name"),
		},
		{
			name:   "WITH row AS alias",
			cypher: "UNWIND $rows AS row\nWITH row AS r\nMERGE (m:Module {name: r.name})",
			params: oversizedRows("name"),
		},
		{
			name:   "WITH row.props AS map alias",
			cypher: "UNWIND $rows AS row\nWITH row, row.props AS p\nMERGE (n:Function {uid: row.uid})\nSET n += p",
			params: map[string]any{"rows": []map[string]any{{"uid": "u", "props": map[string]any{"name": strings.Repeat("x", 9000)}}}},
		},
		{
			name:   "label added with SET after unlabeled MERGE",
			cypher: "UNWIND $rows AS row\nMERGE (m {uid: row.uid})\nSET m:Module, m.name = row.name",
			params: oversizedRows("name"),
		},
		{
			name:   "string literal concatenated into the key",
			cypher: "UNWIND $rows AS row\nMERGE (m:Module {name: 'prefix:' + row.name})",
			params: map[string]any{"rows": []map[string]any{{"name": strings.Repeat("x", 7995)}}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, dropped, skip := GuardIndexKeyWrites(tc.cypher, tc.params)
			if skip || len(dropped) != 1 {
				t.Fatalf("skip=%v dropped=%+v, want the oversized row dropped", skip, dropped)
			}
			if refs, _ := UnanalyzedIndexWrites(tc.cypher); len(refs) != 0 {
				t.Fatalf("recognized shape reported unanalyzed: %+v", refs)
			}
		})
	}
}

// TestGuardIndexKeyWritesKeepsLiteralConcatenationUnderLimit proves the
// literal bytes are counted, not just the row fields.
func TestGuardIndexKeyWritesKeepsLiteralConcatenationUnderLimit(t *testing.T) {
	t.Parallel()

	cypher := "UNWIND $rows AS row\nMERGE (m:Module {name: 'prefix:' + row.name})"
	params := map[string]any{"rows": []map[string]any{{"name": strings.Repeat("x", 7990)}}}
	if _, dropped, _ := GuardIndexKeyWrites(cypher, params); len(dropped) != 0 {
		t.Fatalf("dropped = %+v, want 7997-byte key kept", dropped)
	}
}

// TestUnanalyzedIndexWritesReportsUnhandledShapes pins the fail-loud path:
// write shapes the analyzer cannot read for a schema-indexed label are
// reported instead of passing through silently.
func TestUnanalyzedIndexWritesReportsUnhandledShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		cypher     string
		wantLabel  string
		wantReason string
	}{
		{
			name:       "nested UNWIND element",
			cypher:     "UNWIND $rows AS row\nUNWIND row.params AS p\nMERGE (x:Parameter {name: p.name, path: row.file_path, function_line_number: 1})",
			wantLabel:  "Parameter",
			wantReason: UnanalyzedReasonUnresolvedValue,
		},
		{
			name:       "map alias the analyzer cannot resolve",
			cypher:     "UNWIND $rows AS row\nWITH row, other.props AS p\nMERGE (n:Function {uid: row.uid})\nSET n += p",
			wantLabel:  "Function",
			wantReason: UnanalyzedReasonUnresolvedValue,
		},
		{
			name:       "backtick property key",
			cypher:     "UNWIND $rows AS row\nMERGE (m:Module {`name`: row.name})",
			wantLabel:  "Module",
			wantReason: UnanalyzedReasonUnparsedWrite,
		},
		{
			name:       "unbalanced property map",
			cypher:     "UNWIND $rows AS row\nMERGE (m:Module {name: row.name",
			wantLabel:  "Module",
			wantReason: UnanalyzedReasonUnparsedWrite,
		},
		{
			name:       "unrecognized SET item on a labeled node",
			cypher:     "UNWIND $rows AS row\nMATCH (m:Module {uid: row.uid})\nSET m['name'] = row.name",
			wantLabel:  "Module",
			wantReason: UnanalyzedReasonUnparsedWrite,
		},
		{
			name:       "node pattern the reader cannot see",
			cypher:     "UNWIND $rows AS row\nMERGE (módulo:Module {name: row.name})",
			wantLabel:  "Module",
			wantReason: UnanalyzedReasonUnboundLabel,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			refs, _ := UnanalyzedIndexWrites(tc.cypher)
			for _, ref := range refs {
				if ref.Label == tc.wantLabel && ref.Reason == tc.wantReason {
					return
				}
			}
			t.Fatalf("refs = %+v, want %s/%s", refs, tc.wantLabel, tc.wantReason)
		})
	}
}

// TestUnanalyzedIndexWritesIgnoresUnindexedAndAnalyzedWrites keeps the loud
// path quiet for statements the analyzer fully understands, including
// relationship-only MERGEs over MATCHed endpoints.
func TestUnanalyzedIndexWritesIgnoresUnindexedAndAnalyzedWrites(t *testing.T) {
	t.Parallel()

	quiet := []string{
		semanticFunctionShape,
		"UNWIND $rows AS row\nMATCH (p:Directory {path: row.parent_path})\nMATCH (d:Directory {path: row.path})\nMERGE (p)-[rel:CONTAINS]->(d)\nSET rel.evidence_source = 'projector/canonical'",
		"UNWIND $rows AS row\nMERGE (n:Function {uid: row.entity_id})\nSET n += row.props",
		"MATCH (n:Function) RETURN n.name",
		"MERGE (m:Module {name: 'fixed'})",
		// A schema label written with a property no schema index covers is
		// fully understood: there is no indexed value to measure.
		"UNWIND $rows AS row\nMATCH (n:Function {uid: row.uid})\nSET n.touched = row.at",
	}
	for _, cypher := range quiet {
		if refs, _ := UnanalyzedIndexWrites(cypher); len(refs) != 0 {
			t.Errorf("statement reported unanalyzed %+v:\n%s", refs, cypher)
		}
	}
}

// TestUnanalyzedIndexWritesFirstSightingOnce proves the WARN gate: the plan
// cache tells the caller exactly once that a statement is new.
func TestUnanalyzedIndexWritesFirstSightingOnce(t *testing.T) {
	t.Parallel()

	cypher := "UNWIND $rows AS row\nMERGE (módulo:Module {name: row.name}) // first-sighting"
	refs, first := UnanalyzedIndexWrites(cypher)
	if len(refs) == 0 || !first {
		t.Fatalf("first call refs=%+v first=%v, want refs and first=true", refs, first)
	}
	if refs, first = UnanalyzedIndexWrites(cypher); len(refs) == 0 || first {
		t.Fatalf("second call refs=%+v first=%v, want refs and first=false", refs, first)
	}
}

// TestGuardIndexKeyWritesUnhandledShapeStillPassesThrough documents that the
// loud path reports, and does not drop, rows the analyzer cannot reason about.
func TestGuardIndexKeyWritesUnhandledShapeStillPassesThrough(t *testing.T) {
	t.Parallel()

	cypher := "UNWIND $rows AS row\nUNWIND row.params AS p\nMERGE (x:Parameter {name: p.name, path: row.file_path, function_line_number: 1})"
	params := map[string]any{"rows": []map[string]any{{"file_path": "a", "params": []any{map[string]any{"name": strings.Repeat("x", 9000)}}}}}
	out, dropped, skip := GuardIndexKeyWrites(cypher, params)
	if skip || len(dropped) != 0 || len(out["rows"].([]map[string]any)) != 1 {
		t.Fatalf("unhandled shape must pass through: skip=%v dropped=%+v", skip, dropped)
	}
}

// BenchmarkUnanalyzedIndexWrites measures the per-statement lookup
// InstrumentedExecutor adds beside GuardIndexKeyWrites: a plan-cache hit for a
// statement with nothing to report, which is the steady state.
func BenchmarkUnanalyzedIndexWrites(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if refs, _ := UnanalyzedIndexWrites(semanticFunctionShape); refs != nil {
			b.Fatal("unexpected report")
		}
	}
}

// TestUnanalyzedIndexWritesFirstSightingUnderConcurrency proves only one of
// many concurrent callers is told a statement is new, so the WARN fires once.
func TestUnanalyzedIndexWritesFirstSightingUnderConcurrency(t *testing.T) {
	t.Parallel()

	cypher := "UNWIND $rows AS row\nMERGE (módulo:Module {name: row.name}) // concurrent-sighting"
	const callers = 32
	firsts := make(chan bool, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, first := UnanalyzedIndexWrites(cypher)
			firsts <- first
		}()
	}
	wg.Wait()
	close(firsts)
	n := 0
	for first := range firsts {
		if first {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d callers saw first=true, want exactly 1", n)
	}
}
