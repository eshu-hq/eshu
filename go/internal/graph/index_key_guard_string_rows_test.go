// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func stringFunctionRow(id, name, path string) map[string]string {
	return map[string]string{
		"entity_id": id, "entity_name": name, "file_path": path,
		"repo_id": "repo-1", "semantic_kind": "function", "evidence_source": "test",
	}
}

// TestGuardIndexKeyWritesStringRowsDoNotAllocateWhenNothingDropped pins the
// steady-state cost of the []map[string]string arm: sizes are read straight
// off the string maps, so a guarded write with nothing oversized allocates
// nothing per row.
func TestGuardIndexKeyWritesStringRowsDoNotAllocateWhenNothingDropped(t *testing.T) {
	rows := make([]map[string]string, 500)
	for i := range rows {
		rows[i] = stringFunctionRow(fmt.Sprintf("content-entity:e_%012d", i), fmt.Sprintf("handler%d", i), "/repo/src/service/handlers.go")
	}
	params := map[string]any{"rows": rows}
	GuardIndexKeyWrites(semanticFunctionShape, params) // warm the plan cache

	allocs := testing.AllocsPerRun(20, func() {
		if _, dropped, _ := GuardIndexKeyWrites(semanticFunctionShape, params); dropped != nil {
			t.Fatal("unexpected drop")
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs per guarded []map[string]string write = %v, want 0", allocs)
	}
}

func TestGuardIndexKeyWritesDropsOversizedStringRows(t *testing.T) {
	t.Parallel()

	rows := []map[string]string{
		stringFunctionRow("ok", "handler", "src/a.go"),
		stringFunctionRow("big-name", strings.Repeat("n", 9000), "src/b.go"),
		stringFunctionRow("big-composite", strings.Repeat("n", 4000), strings.Repeat("p", 4001)),
		stringFunctionRow("at-limit", strings.Repeat("n", 4000), strings.Repeat("p", 4000)),
	}
	out, dropped, skip := GuardIndexKeyWrites(semanticFunctionShape, map[string]any{"rows": rows})
	if skip {
		t.Fatal("skip = true, want row-level drops only")
	}
	kept, ok := out["rows"].([]map[string]string)
	if !ok {
		t.Fatalf("rows type = %T, want []map[string]string", out["rows"])
	}
	var ids []string
	for _, r := range kept {
		ids = append(ids, r["entity_id"])
	}
	if want := []string{"ok", "at-limit"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("kept rows = %v, want %v", ids, want)
	}
	if len(dropped) != 2 {
		t.Fatalf("dropped = %d records, want 2: %+v", len(dropped), dropped)
	}
	if d := dropped[0]; d.Label != "Function" || d.Property != "name" || d.KeyBytes != 9008 || d.EntityID != "big-name" || d.RepoID != "repo-1" || d.FilePath != "src/b.go" {
		t.Fatalf("dropped[0] = %+v, want Function/name 9008 bytes for big-name with triage context", d)
	}
	if len(rows) != 4 {
		t.Fatalf("caller rows mutated: len = %d, want 4", len(rows))
	}
}

func TestGuardIndexKeyWritesReturnsSameStringRowsWhenNothingOversized(t *testing.T) {
	t.Parallel()

	params := map[string]any{"rows": []map[string]string{stringFunctionRow("a", "f", "p")}}
	out, dropped, skip := GuardIndexKeyWrites(semanticFunctionShape, params)
	if skip || dropped != nil {
		t.Fatalf("skip=%v dropped=%v, want neither", skip, dropped)
	}
	if reflect.ValueOf(out).Pointer() != reflect.ValueOf(params).Pointer() {
		t.Fatal("params map copied although nothing was dropped")
	}
}

// TestIndexValuePrefixCutsOnRuneBoundary covers the exported helper the
// canonical projector shares with the statement guard: a multi-byte rune that
// straddles the byte bound must be dropped whole, never split.
func TestIndexValuePrefixCutsOnRuneBoundary(t *testing.T) {
	t.Parallel()

	short := strings.Repeat("a", 10)
	if got := IndexValuePrefix(short); got != short {
		t.Fatalf("IndexValuePrefix(short) = %q, want unchanged", got)
	}
	exact := strings.Repeat("a", IndexValuePrefixBytes)
	if got := IndexValuePrefix(exact); got != exact {
		t.Fatalf("IndexValuePrefix(exactly bound) len = %d, want unchanged %d", len(got), IndexValuePrefixBytes)
	}

	// 63 ASCII bytes then "é" (2 bytes, spans bytes 63-64) then more text: the
	// bound falls inside the rune.
	split := strings.Repeat("a", IndexValuePrefixBytes-1) + "é" + strings.Repeat("b", 50)
	got := IndexValuePrefix(split)
	if !utf8.ValidString(got) {
		t.Fatalf("prefix %q is not valid UTF-8", got)
	}
	if len(got) > IndexValuePrefixBytes {
		t.Fatalf("prefix len = %d, want <= %d", len(got), IndexValuePrefixBytes)
	}
	if want := strings.Repeat("a", IndexValuePrefixBytes-1); got != want {
		t.Fatalf("prefix = %q, want the split rune dropped: %q", got, want)
	}

	// A rune that ends exactly on the bound is kept whole.
	fit := strings.Repeat("a", IndexValuePrefixBytes-2) + "é" + strings.Repeat("b", 50)
	if got, want := IndexValuePrefix(fit), strings.Repeat("a", IndexValuePrefixBytes-2)+"é"; got != want {
		t.Fatalf("prefix = %q, want the rune ending on the bound kept: %q", got, want)
	}
}
