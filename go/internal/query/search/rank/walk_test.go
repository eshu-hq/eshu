// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank_test

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
)

// testWindow returns a find_code window of count rows in lexical order
// r000, r001, ... with stable content fingerprints.
func testWindow(count int) rank.Window {
	rows := make([]rank.Row, 0, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("r%03d", i)
		rows = append(rows, rank.Row{ID: id, Content: "body of " + id})
	}
	return rank.Window{
		Tool:  rank.ToolFindCode,
		Scope: []string{"repo-1", "handler", ""},
		Rows:  rows,
	}
}

// reverseScores scores rows so relevance is the reverse of the lexical order:
// the last lexical row is the most relevant. Under offset paging with a
// per-page re-rank, the best rows would sit on the last page.
func reverseScores(window rank.Window) map[string]float64 {
	scores := make(map[string]float64, len(window.Rows))
	for i, row := range window.Rows {
		scores[row.ID] = 1.0 / float64(60+len(window.Rows)-i)
	}
	return scores
}

// walkAll pages through a window with one score map per page (scoresFor is
// called with the page number) and returns every served id in order.
func walkAll(t *testing.T, window rank.Window, limit int, scoresFor func(page int) map[string]float64) ([]string, error) {
	t.Helper()
	var served []string
	token := rank.StartToken
	for page := 0; page < 1000; page++ {
		result, err := rank.Walk(window, scoresFor(page), token, limit)
		if err != nil {
			return served, err
		}
		served = append(served, result.IDs...)
		if result.NextCursor == "" {
			if !result.WindowEnd {
				t.Fatalf("page %d: no next cursor but WindowEnd=false", page)
			}
			return served, nil
		}
		if result.WindowEnd {
			t.Fatalf("page %d: WindowEnd=true with a next cursor", page)
		}
		token = result.NextCursor
	}
	t.Fatal("walk did not end")
	return nil, nil
}

func TestWalkReadsEveryRowOnceInGlobalRelevanceOrder(t *testing.T) {
	t.Parallel()
	window := testWindow(rank.WindowSize)
	scores := reverseScores(window)
	want := rank.Order(windowIDs(window), scores)
	if len(want) != rank.WindowSize || want[0] != "r199" || want[len(want)-1] != "r000" {
		t.Fatalf("Order = %v, want [r199 ... r000]", want)
	}
	for _, limit := range []int{1, 37, 50, 200} {
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			t.Parallel()
			got, err := walkAll(t, window, limit, func(int) map[string]float64 { return scores })
			if err != nil {
				t.Fatalf("walk error = %v", err)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("walk order differs from global relevance order:\n got %v\nwant %v", got, want)
			}
			assertExactlyOnce(t, got, windowIDs(window))
		})
	}
}

func TestWalkFirstPageHoldsTheGloballyBestRows(t *testing.T) {
	t.Parallel()
	window := testWindow(120)
	page, err := rank.Walk(window, reverseScores(window), rank.StartToken, 50)
	if err != nil {
		t.Fatalf("Walk error = %v", err)
	}
	if len(page.IDs) != 50 {
		t.Fatalf("first page has %d rows, want 50", len(page.IDs))
	}
	// Lexical rows r070..r119 are the 50 most relevant; offset paging would
	// have served r000..r049 first.
	for _, id := range page.IDs {
		var n int
		if _, err := fmt.Sscanf(id, "r%03d", &n); err != nil || n < 70 {
			t.Fatalf("first page holds %s, want only r070..r119", id)
		}
	}
	if page.Served != 50 || page.WindowRows != 120 || page.WindowEnd {
		t.Fatalf("page = served %d rows %d end %v, want 50 120 false", page.Served, page.WindowRows, page.WindowEnd)
	}
}

func TestWalkDropsDuplicateAndUnkeyedRows(t *testing.T) {
	t.Parallel()
	window := testWindow(4)
	window.Rows = append(window.Rows, rank.Row{ID: "r001", Content: "dup"}, rank.Row{ID: "", Content: "no id"})
	page, err := rank.Walk(window, nil, rank.StartToken, 10)
	if err != nil {
		t.Fatalf("Walk error = %v", err)
	}
	if want := []string{"r000", "r001", "r002", "r003"}; !slices.Equal(page.IDs, want) {
		t.Fatalf("IDs = %v, want %v", page.IDs, want)
	}
	if page.Excluded != 2 || page.WindowRows != 4 || !page.WindowEnd || page.NextCursor != "" {
		t.Fatalf("page = excluded %d rows %d end %v next %q, want 2 4 true \"\"",
			page.Excluded, page.WindowRows, page.WindowEnd, page.NextCursor)
	}
}

func TestOrderBreaksScoreTiesByIDAndPutsUnscoredRowsLast(t *testing.T) {
	t.Parallel()
	ids := []string{"e", "d", "c", "b", "a", "z", "y"}
	scores := map[string]float64{"e": 0.5, "c": 0.5, "a": 0.5, "d": 0.9, "b": 0.1}
	got := rank.Order(ids, scores)
	want := []string{"d", "a", "c", "e", "b", "y", "z"}
	if !slices.Equal(got, want) {
		t.Fatalf("Order = %v, want %v", got, want)
	}
}

func TestOrderTreatsNonFiniteScoresAsUnscored(t *testing.T) {
	t.Parallel()
	ids := []string{"b", "a", "c", "d"}
	got := rank.Order(ids, map[string]float64{"b": math.NaN(), "c": 0.2, "d": math.Inf(1)})
	if want := []string{"c", "a", "b", "d"}; !slices.Equal(got, want) {
		t.Fatalf("Order = %v, want %v", got, want)
	}
}

func TestWalkCandidateSetChangeBetweenPagesIsConflict(t *testing.T) {
	t.Parallel()
	base := testWindow(80)
	first, err := rank.Walk(base, reverseScores(base), rank.StartToken, 30)
	if err != nil {
		t.Fatalf("first page error = %v", err)
	}
	changes := map[string]func(rank.Window) rank.Window{
		"row content edited": func(w rank.Window) rank.Window {
			w.Rows = slices.Clone(w.Rows)
			w.Rows[40].Content = "edited"
			return w
		},
		"row added": func(w rank.Window) rank.Window {
			w.Rows = append(slices.Clone(w.Rows), rank.Row{ID: "r999", Content: "new"})
			return w
		},
		"lexical order changed": func(w rank.Window) rank.Window {
			w.Rows = slices.Clone(w.Rows)
			w.Rows[0], w.Rows[1] = w.Rows[1], w.Rows[0]
			return w
		},
		"lexical truncation changed": func(w rank.Window) rank.Window {
			w.LexicalTruncated = true
			return w
		},
		"query changed": func(w rank.Window) rank.Window {
			w.Scope = []string{"repo-1", "other", ""}
			return w
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			changed := change(base)
			_, err := rank.Walk(changed, reverseScores(changed), first.NextCursor, 30)
			assertRankError(t, err, http.StatusConflict, rank.ErrorCodeBasisMismatch, rank.ReasonCandidateSetChanged)
		})
	}
}

func TestWalkOrderFlipAcrossServedBoundaryIsConflict(t *testing.T) {
	t.Parallel()
	window := testWindow(60)
	scores := reverseScores(window)
	first, err := rank.Walk(window, scores, rank.StartToken, 20)
	if err != nil {
		t.Fatalf("first page error = %v", err)
	}
	// Another replica ranks one unserved row above a served one: the served
	// set is no longer the top of its order.
	flipped := cloneScores(scores)
	flipped[first.IDs[19]], flipped["r000"] = flipped["r000"], flipped[first.IDs[19]]
	_, err = rank.Walk(window, flipped, first.NextCursor, 20)
	assertRankError(t, err, http.StatusConflict, rank.ErrorCodeBasisMismatch, rank.ReasonRankOrderChanged)
}

func TestWalkOrderFlipInsideServedRowsIsHarmless(t *testing.T) {
	t.Parallel()
	window := testWindow(60)
	scores := reverseScores(window)
	first, err := rank.Walk(window, scores, rank.StartToken, 20)
	if err != nil {
		t.Fatalf("first page error = %v", err)
	}
	swapped := cloneScores(scores)
	swapped[first.IDs[0]], swapped[first.IDs[5]] = swapped[first.IDs[5]], swapped[first.IDs[0]]
	second, err := rank.Walk(window, swapped, first.NextCursor, 20)
	if err != nil {
		t.Fatalf("second page error = %v, want the walk to continue", err)
	}
	if slices.ContainsFunc(second.IDs, func(id string) bool { return slices.Contains(first.IDs, id) }) {
		t.Fatalf("second page repeats a served row: %v", second.IDs)
	}
}

func TestWalkRejectsACursorFromAnotherTool(t *testing.T) {
	t.Parallel()
	window := testWindow(30)
	first, err := rank.Walk(window, nil, rank.StartToken, 10)
	if err != nil {
		t.Fatalf("first page error = %v", err)
	}
	other := window
	other.Tool = rank.ToolSearchEntityContent
	_, err = rank.Walk(other, nil, first.NextCursor, 10)
	assertRankError(t, err, http.StatusBadRequest, rank.ErrorCodeInvalid, rank.ReasonToolMismatch)
}

func TestWalkRejectsANonPositiveLimit(t *testing.T) {
	t.Parallel()
	_, err := rank.Walk(testWindow(3), nil, rank.StartToken, 0)
	assertRankError(t, err, http.StatusBadRequest, rank.ErrorCodeInvalid, rank.ReasonInvalidLimit)
}

func cloneScores(scores map[string]float64) map[string]float64 {
	clone := make(map[string]float64, len(scores))
	for id, score := range scores {
		clone[id] = score
	}
	return clone
}

func windowIDs(window rank.Window) []string {
	ids := make([]string, 0, len(window.Rows))
	for _, row := range window.Rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func assertExactlyOnce(t *testing.T, served, all []string) {
	t.Helper()
	seen := make(map[string]int, len(served))
	for _, id := range served {
		seen[id]++
	}
	for _, id := range all {
		if seen[id] != 1 {
			t.Fatalf("row %s served %d times, want exactly once", id, seen[id])
		}
	}
	if len(served) != len(all) {
		t.Fatalf("served %d rows, want %d", len(served), len(all))
	}
}

func assertRankError(t *testing.T, err error, status int, code querycontract.ErrorCode, reason string) {
	t.Helper()
	var rankErr *rank.Error
	if !errors.As(err, &rankErr) {
		t.Fatalf("error = %v, want *rank.Error", err)
	}
	if rankErr.Status != status || rankErr.Code != code || rankErr.Reason != reason {
		t.Fatalf("error = status %d code %q reason %q, want %d %q %q",
			rankErr.Status, rankErr.Code, rankErr.Reason, status, code, reason)
	}
}
