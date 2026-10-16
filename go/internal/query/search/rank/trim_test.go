// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank_test

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
)

// decodedPage returns a page block and rows as an MCP client sees them: the
// handler's response round-tripped through JSON.
func decodedPage(t *testing.T, page rank.Page) (any, []any) {
	t.Helper()
	rows := make([]map[string]any, 0, len(page.IDs))
	for _, id := range page.IDs {
		rows = append(rows, map[string]any{"entity_id": id, "source_cache": "x"})
	}
	encoded, err := json.Marshal(map[string]any{"rank_walk": page.Block(), "results": rows})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return decoded["rank_walk"], decoded["results"].([]any)
}

func TestPageBlockCarriesTheWalkState(t *testing.T) {
	t.Parallel()
	window := testWindow(30)
	window.LexicalTruncated = true
	page, err := rank.Walk(window, reverseScores(window), rank.StartToken, 10)
	if err != nil {
		t.Fatalf("Walk error = %v", err)
	}
	block := page.Block()
	for key, want := range map[string]any{
		"lane":              rank.LaneContent,
		"order":             rank.OrderHybrid,
		"ranked":            true,
		"page_cursor":       page.PageCursor,
		"next_cursor":       page.NextCursor,
		"position":          10,
		"window_rows":       30,
		"window_size":       rank.WindowSize,
		"window_end":        false,
		"lexical_truncated": true,
		"excluded_rows":     0,
	} {
		if block[key] != want {
			t.Fatalf("block[%q] = %v, want %v", key, block[key], want)
		}
	}
}

func TestPageBlockOmitsTheNextCursorAtWindowEnd(t *testing.T) {
	t.Parallel()
	page, err := rank.Walk(testWindow(5), nil, rank.StartToken, 10)
	if err != nil {
		t.Fatalf("Walk error = %v", err)
	}
	block := page.Block()
	if _, present := block["next_cursor"]; present {
		t.Fatalf("block carries next_cursor at window end: %v", block)
	}
	if block["window_end"] != true || block["ranked"] != true {
		t.Fatalf("window_end = %v, want true", block["window_end"])
	}
}

func TestTrimWalkResumesAfterTheKeptRows(t *testing.T) {
	t.Parallel()
	window := testWindow(100)
	scores := reverseScores(window)
	page, err := rank.Walk(window, scores, rank.StartToken, 40)
	if err != nil {
		t.Fatalf("Walk error = %v", err)
	}
	walk, rows := decodedPage(t, page)
	trimmed, ok := rank.TrimWalk(walk, rows[:15])
	if !ok {
		t.Fatal("TrimWalk not ok")
	}
	next, _ := trimmed["next_cursor"].(string)
	if next == "" || trimmed["position"] != 15 || trimmed["window_end"] != false {
		t.Fatalf("trimmed block = %v, want position 15 with a next cursor", trimmed)
	}
	resumed, err := rank.Walk(window, scores, next, 40)
	if err != nil {
		t.Fatalf("resume error = %v", err)
	}
	if resumed.IDs[0] != page.IDs[15] {
		t.Fatalf("resume starts at %s, want %s (the first dropped row)", resumed.IDs[0], page.IDs[15])
	}
}

func TestTrimWalkRefusesWhatItCannotProve(t *testing.T) {
	t.Parallel()
	window := testWindow(20)
	page, err := rank.Walk(window, nil, rank.StartToken, 10)
	if err != nil {
		t.Fatalf("Walk error = %v", err)
	}
	walk, rows := decodedPage(t, page)
	if _, ok := rank.TrimWalk(nil, rows[:3]); ok {
		t.Fatal("TrimWalk(nil) ok, want false")
	}
	if _, ok := rank.TrimWalk(map[string]any{"page_cursor": "garbage"}, rows[:3]); ok {
		t.Fatal("TrimWalk(garbage cursor) ok, want false")
	}
	if _, ok := rank.TrimWalk(walk, []any{map[string]any{"name": "no id"}}); ok {
		t.Fatal("TrimWalk(row without entity_id) ok, want false")
	}
	if _, ok := rank.TrimWalk(walk, []any{"not a row"}); ok {
		t.Fatal("TrimWalk(non-object row) ok, want false")
	}
}
