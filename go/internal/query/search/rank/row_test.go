// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank_test

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
)

func TestMapRowKeysTheIDAndEveryNamedField(t *testing.T) {
	t.Parallel()
	base := map[string]any{"entity_id": "e1", "source_cache": "body", "start_line": 3}
	row := rank.MapRow(base, "source_cache", "start_line", "language")
	if row.ID != "e1" {
		t.Fatalf("ID = %q, want e1", row.ID)
	}
	variants := []map[string]any{
		{"entity_id": "e1", "source_cache": "body2", "start_line": 3},
		{"entity_id": "e1", "source_cache": "body", "start_line": 4},
		{"entity_id": "e1", "source_cache": "body", "start_line": 3, "language": ""},
		// A field present as JSON null differs from an absent field.
		{"entity_id": "e1", "source_cache": "body", "start_line": 3, "language": nil},
	}
	for _, variant := range variants {
		if rank.MapRow(variant, "source_cache", "start_line", "language").Content == row.Content {
			t.Fatalf("MapRow(%v) content equals the base row", variant)
		}
	}
	if got := rank.MapRow(map[string]any{"entity_id": 7}).ID; got != "" {
		t.Fatalf("non-string id = %q, want empty", got)
	}
}

func TestMapRowsAndMapPageKeepTheFirstRowPerID(t *testing.T) {
	t.Parallel()
	rows := []map[string]any{
		{"entity_id": "a", "page_position": 0},
		{"entity_id": "b"},
		{"entity_id": "a", "note": "duplicate"},
	}
	windowRows, byID := rank.MapRows(rows, "note")
	if len(windowRows) != 3 || windowRows[2].ID != "a" {
		t.Fatalf("window rows = %v, want all three in lexical order", windowRows)
	}
	page := rank.MapPage(byID, map[string]float64{"a": 1}, []string{"b", "a"})
	if ids := []any{page[0]["entity_id"], page[1]["entity_id"]}; !slices.Equal(ids, []any{"b", "a"}) {
		t.Fatalf("page ids = %v, want [b a]", ids)
	}
	if page[1]["note"] != nil || page[1]["search_backend"] != "hybrid" {
		t.Fatalf("row a = %v, want the first occurrence marked hybrid", page[1])
	}
	if _, present := page[1]["page_position"]; present {
		t.Fatalf("row a keeps page_position: %v", page[1])
	}
	if _, present := page[0]["search_backend"]; present {
		t.Fatalf("unscored row b marked: %v", page[0])
	}
}
