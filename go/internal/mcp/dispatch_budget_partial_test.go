// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/contentread"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// partialWalkStore models the unscoped entity walk for the byte-cap test: all
// rows match, and one call visits at most visit rows after the cursor. A call
// that finds offset+limit+1 matches inside them is exact; one that reaches the
// end of the rows is exhausted; any other call is a partial whose cursor is the
// last visited row.
type partialWalkStore struct {
	content.FakePortContentStore
	rows  []querycontract.EntityContent
	visit int
}

func (s *partialWalkStore) SearchEntitiesUnscoped(
	_ context.Context, _ string, limit, offset int, cursor querycontract.SearchCursor,
) (querycontract.EntitySearchPage, error) {
	from := 0
	if !cursor.IsZero() {
		for from < len(s.rows) && s.rows[from].EntityID <= cursor.EntityID {
			from++
		}
	}
	to := min(from+s.visit, len(s.rows))
	matches := s.rows[from:to]
	page := func(rows []querycontract.EntityContent) []querycontract.EntityContent {
		lo := min(offset, len(rows))
		return rows[lo:min(offset+limit, len(rows))]
	}
	if want := offset + limit + 1; len(matches) >= want || to == len(s.rows) {
		return querycontract.EntitySearchPage{Entities: page(matches), More: len(matches) >= want}, nil
	}
	last := matches[len(matches)-1]
	return querycontract.EntitySearchPage{Entities: page(matches), Partial: &querycontract.SearchPartial{
		Reason:      querycontract.SearchPartialCandidateBudgetExceeded,
		RowsMatched: len(matches),
		Cursor: querycontract.SearchCursor{
			RepoID: last.RepoID, RelativePath: last.RelativePath, StartLine: last.StartLine, EntityID: last.EntityID,
		},
		BudgetMS: 800, ElapsedMS: 790, Progressed: true, Hint: querycontract.SearchPartialHint,
	}}, nil
}

// entityPage is the part of one search_entity_content reply the precedence
// test reads.
type entityPage struct {
	ids        []string
	nextOffset *int
	partial    *querycontract.SearchPartial
	budgetPage bool
}

func readEntityPage(t *testing.T, result *dispatchResult) entityPage {
	t.Helper()
	if result.IsError || result.Envelope == nil || result.Envelope.Error != nil {
		t.Fatalf("dispatch returned an error envelope: %+v", result)
	}
	raw, err := json.Marshal(result.Envelope.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var data struct {
		NextOffset *int                         `json:"next_offset"`
		Partial    *querycontract.SearchPartial `json:"partial"`
		BudgetPage map[string]any               `json:"budget_page"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	return entityPage{ids: pageRowIDs(t, result), nextOffset: data.NextOffset, partial: data.Partial, budgetPage: data.BudgetPage != nil}
}

// A partial unscoped entity page can also be trimmed by the MCP byte cap, so
// one reply carries both partial and budget_page with next_offset. The tool
// text states the precedence: page with next_offset and the same request
// cursor first, and follow partial.cursor (offset - rows_matched) only from a
// page the cap did not trim. Following that order reads every row exactly
// once; following partial.cursor from a trimmed page skips the trimmed rows,
// because rows_matched counts them (#7009 ruling, 2.4).
func TestBudgetPageAndPartialOnOnePageFollowTheStatedPrecedence(t *testing.T) {
	t.Parallel()

	store := &partialWalkStore{rows: newBudgetPageEntities(100), visit: 40}
	mux := http.NewServeMux()
	(&contentread.ContentHandler{Profile: querycontract.ProfileLocalAuthoritative, Content: store}).Mount(mux)
	const budget = 100_000
	call := func(offset int, cursor map[string]any) entityPage {
		args := map[string]any{"query": "decode", "limit": 50, "offset": offset}
		if cursor != nil {
			args["cursor"] = cursor
		}
		return readEntityPage(t, dispatchBudgetPage(t, mux, "search_entity_content", args, budget))
	}

	first := call(0, nil)
	if first.partial == nil || !first.budgetPage || first.nextOffset == nil {
		t.Fatalf("first page partial=%v budget_page=%v next_offset=%v, want all three", first.partial != nil, first.budgetPage, first.nextOffset)
	}
	if *first.nextOffset != len(first.ids) || first.partial.RowsMatched <= len(first.ids) {
		t.Fatalf("next_offset=%d rows=%d rows_matched=%d, want next_offset = rows < rows_matched",
			*first.nextOffset, len(first.ids), first.partial.RowsMatched)
	}

	// The wrong order: partial.cursor straight from the trimmed page skips
	// the rows between the trim and rows_matched.
	cursorOf := func(p *querycontract.SearchPartial) map[string]any {
		return map[string]any{"repo_id": p.Cursor.RepoID, "relative_path": p.Cursor.RelativePath, "start_line": p.Cursor.StartLine, "entity_id": p.Cursor.EntityID}
	}
	if skipped := call(0, cursorOf(first.partial)); skipped.ids[0] != store.rows[first.partial.RowsMatched].EntityID {
		t.Fatalf("partial.cursor resumed at %s, want row %d (the trimmed rows lie before it)", skipped.ids[0], first.partial.RowsMatched)
	}

	// The stated order reads every row once.
	gathered := append([]string(nil), first.ids...)
	offset, cursor, current := *first.nextOffset, map[string]any(nil), first
	for calls := 1; ; calls++ {
		if calls > 40 {
			t.Fatalf("no end after %d calls", calls)
		}
		switch {
		case current.nextOffset != nil:
			offset = *current.nextOffset
		case current.partial != nil:
			cursor = cursorOf(current.partial)
			offset = max(0, offset-current.partial.RowsMatched)
		default:
			var want []string
			for _, row := range store.rows {
				want = append(want, row.EntityID)
			}
			if len(gathered) != len(want) {
				t.Fatalf("gathered %d rows, want %d", len(gathered), len(want))
			}
			for i := range want {
				if gathered[i] != want[i] {
					t.Fatalf("row %d = %s, want %s", i, gathered[i], want[i])
				}
			}
			return
		}
		current = call(offset, cursor)
		gathered = append(gathered, current.ids...)
	}
}
