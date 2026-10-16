// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank_test

import (
	"errors"
	"math"
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
	page, err := rank.MapPage(byID, map[string]float64{"a": 1}, []string{"b", "a"})
	if err != nil {
		t.Fatalf("MapPage error = %v", err)
	}
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

// TestMapPageRefusesAnIDWithNoRow pins that MapPage refuses a page id with
// no row: no rows, ErrPageRowMissing, and no nil-map write. A short page
// would count the missing row as served and lose it from the walk.
func TestMapPageRefusesAnIDWithNoRow(t *testing.T) {
	t.Parallel()
	byID := map[string]map[string]any{"a": {"entity_id": "a"}, "nil": nil}
	for _, ids := range [][]string{{"ghost", "a"}, {"a", "nil"}} {
		page, err := rank.MapPage(byID, map[string]float64{"a": 1, "ghost": 1, "nil": 1}, ids)
		if !errors.Is(err, rank.ErrPageRowMissing) || page != nil {
			t.Fatalf("MapPage(%v) = %v, %v; want no rows and ErrPageRowMissing", ids, page, err)
		}
	}
}

// TestMapPageLabelsOnlyFinitelyScoredRows pins one definition of scored:
// a NaN or infinite score orders a row in the unscored tail (rank.Order), so
// it must not carry search_backend=hybrid either.
func TestMapPageLabelsOnlyFinitelyScoredRows(t *testing.T) {
	t.Parallel()
	byID := map[string]map[string]any{"a": {"entity_id": "a"}, "b": {"entity_id": "b"}, "c": {"entity_id": "c"}}
	page, err := rank.MapPage(byID, map[string]float64{"a": 0.5, "b": math.NaN(), "c": math.Inf(1)}, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("MapPage error = %v", err)
	}
	if page[0]["search_backend"] != "hybrid" {
		t.Fatalf("row a = %v, want search_backend=hybrid", page[0])
	}
	for _, row := range page[1:] {
		if _, labelled := row["search_backend"]; labelled {
			t.Fatalf("non-finite row %v carries search_backend", row)
		}
	}
}

func TestScoredMeansFinitelyScored(t *testing.T) {
	t.Parallel()
	scores := map[string]float64{"a": 0.5, "b": math.NaN(), "c": math.Inf(-1)}
	for id, want := range map[string]bool{"a": true, "b": false, "c": false, "missing": false} {
		if got := rank.Scored(scores, id); got != want {
			t.Fatalf("Scored(%q) = %v, want %v", id, got, want)
		}
	}
	if rank.AnyScored(map[string]float64{"b": math.NaN(), "c": math.Inf(1)}) {
		t.Fatal("AnyScored(all non-finite) = true, want false")
	}
	if !rank.AnyScored(scores) || rank.AnyScored(nil) {
		t.Fatal("AnyScored mismatch")
	}
}
