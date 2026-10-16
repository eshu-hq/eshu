// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// WindowSize is the fixed number of lexical rows a walk ranks. A walk reads
// the first WindowSize rows of the lexical (offset) order and no more; past
// them the walk ends with window_end=true, and lexical_truncated reports that
// more lexical matches exist.
const WindowSize = 200

// Tool names a route that accepts rank_cursor. A cursor minted by one tool
// is refused by the other.
const (
	ToolFindCode            = "find_code"
	ToolSearchEntityContent = "search_entity_content"
)

var toolCodes = map[string]byte{
	ToolFindCode:            1,
	ToolSearchEntityContent: 2,
}

// Lane names the read path that produced the window. find_code picks it on
// the first page exactly as an offset request does (the graph lane first, the
// content lane when the graph has no rows or no graph is configured), and
// the cursor records it; a different lane on a later page is a 409
// basis_changed. search_entity_content always uses LaneContent.
const (
	LaneGraph   = "graph"
	LaneContent = "content"
)

var laneCodes = map[string]byte{
	LaneGraph:   1,
	LaneContent: 2,
}

// Order names how a walk orders its window.
const (
	// OrderGraphName is the graph lane's own order (entity name, then id).
	// There is no ranker; the walk serves the window order.
	OrderGraphName = "graph_name"
	// OrderHybrid ranks the window by the hybrid ranker's quantized score.
	OrderHybrid = "hybrid"
	// OrderLexical serves the content lane's own order: there is no ranker,
	// exact=true asked for an identity lookup, or no window row could be
	// scored.
	OrderLexical = "lexical"
)

// laneOrders lists the orders each lane can serve.
var laneOrders = map[string]map[string]bool{
	LaneGraph:   {OrderGraphName: true},
	LaneContent: {OrderHybrid: true, OrderLexical: true},
}

// Row is one lexical window row. ID is the row's stable identity (the
// content entity id). Content is every field the ranker reads for the row;
// it enters the basis fingerprint so an edit to a row's text between pages
// is a basis change, not a silent re-score.
type Row struct {
	ID      string
	Content string
}

// Window is the candidate set of one walk: the first WindowSize rows of the
// lexical order, plus everything that decides how they rank.
type Window struct {
	// Tool is ToolFindCode or ToolSearchEntityContent.
	Tool string
	// Lane is LaneGraph or LaneContent.
	Lane string
	// Order is OrderGraphName (graph lane), or OrderHybrid or OrderLexical
	// (content lane). Scores are read only for OrderHybrid.
	Order string
	// Scope holds the request fields that select and score the window, in a
	// fixed order per tool (for example repository, query, language).
	Scope []string
	// Ranker identifies the ranker and its configuration; empty when the
	// order is not OrderHybrid.
	Ranker string
	// Rows is the window in lexical order.
	Rows []Row
	// LexicalTruncated reports that more lexical matches exist past the
	// window. They are never read by the walk.
	LexicalTruncated bool
}

// Page is one page of a walk.
type Page struct {
	// Lane and Order copy the window's.
	Lane, Order string
	// IDs are this page's row ids in walk order.
	IDs []string
	// PageCursor is the cursor this page was served from. Advance(PageCursor,
	// a prefix of IDs) mints the cursor that resumes after that prefix.
	PageCursor string
	// NextCursor resumes after this page; it is empty at the window end.
	NextCursor string
	// Position counts the rows served by the walk through this page.
	Position int
	// WindowRows is the number of walkable rows in the window.
	WindowRows int
	// Excluded counts window rows left out of the walk: a row without an id,
	// or a later duplicate of an id.
	Excluded int
	// WindowEnd reports that this page served the last window row.
	WindowEnd bool
	// LexicalTruncated copies Window.LexicalTruncated.
	LexicalTruncated bool
}

// keyedRows returns the walkable rows: rows with an id, first occurrence of
// each id, in lexical order. excluded counts the rest.
func keyedRows(rows []Row) ([]Row, int) {
	kept := make([]Row, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.ID == "" {
			continue
		}
		if _, dup := seen[row.ID]; dup {
			continue
		}
		seen[row.ID] = struct{}{}
		kept = append(kept, row)
	}
	return kept, len(rows) - len(kept)
}

// basis fingerprints the candidate set: tool, scope, ranker identity, lexical
// truncation, and every walkable row's id and content in lexical order. Each
// field is length-prefixed, so no two different windows share an encoding.
func basis(window Window, rows []Row, excluded int) [16]byte {
	hash := sha256.New()
	write := func(value string) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		hash.Write(size[:])
		hash.Write([]byte(value))
	}
	write("eshu-rank-walk-basis/v1")
	write(window.Tool)
	write(window.Lane)
	write(window.Order)
	write(window.Ranker)
	write(fmt.Sprint(len(window.Scope), window.LexicalTruncated, excluded))
	for _, field := range window.Scope {
		write(field)
	}
	for _, row := range rows {
		write(row.ID)
		write(row.Content)
	}
	var fingerprint [16]byte
	copy(fingerprint[:], hash.Sum(nil))
	return fingerprint
}

// Walk serves the page that starts at token for window, ranked by scores.
//
// token is StartToken or a cursor a previous page returned. An OrderHybrid
// window is ordered by Order(ids, scores); any other window keeps its own
// order, so its pages equal the plain offset pages. The page is the next
// limit rows after the rows already served. Walk never trusts score bits across pages: it checks that the
// cursor's lane and basis fingerprint match window (else 409 basis_changed)
// and that the rows the cursor says were served are exactly the top rows of
// this order (else 409 order_changed). When both hold, the page can
// neither repeat nor skip a row. A malformed or foreign token is a 400.
func Walk(window Window, scores map[string]float64, token string, limit int) (Page, error) {
	if limit <= 0 {
		return Page{}, invalid(ReasonInvalidLimit, "limit must be positive")
	}
	tool, ok := toolCodes[window.Tool]
	if !ok {
		return Page{}, fmt.Errorf("rank walk: unknown tool %q", window.Tool)
	}
	lane, ok := laneCodes[window.Lane]
	if !ok || !laneOrders[window.Lane][window.Order] {
		return Page{}, fmt.Errorf("rank walk: lane %q cannot serve order %q", window.Lane, window.Order)
	}
	rows, excluded := keyedRows(window.Rows)
	fingerprint := basis(window, rows, excluded)

	state := cursor{tool: tool, lane: lane, basis: fingerprint}
	if token != StartToken {
		parsed, err := parseCursor(token)
		if err != nil {
			return Page{}, err
		}
		if parsed.tool != tool {
			return Page{}, invalid(ReasonToolMismatch, "rank_cursor was returned by another tool")
		}
		if parsed.lane != lane {
			return Page{}, mismatch(ReasonBasisChanged, "the search lane changed since this cursor was returned; restart with rank_cursor=\"start\"")
		}
		if parsed.basis != fingerprint {
			return Page{}, mismatch(ReasonBasisChanged, "the ranked window changed since this cursor was returned; restart with rank_cursor=\"start\"")
		}
		state = parsed
	}

	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	order := ids
	if window.Order == OrderHybrid {
		order = Order(ids, scores)
	}
	if state.count > len(order) {
		return Page{}, invalid(ReasonMalformed, "rank_cursor is past the end of the ranked window")
	}
	if (served{}).add(order[:state.count]) != state.digest {
		return Page{}, mismatch(ReasonOrderChanged, "the ranked order changed since this cursor was returned; restart with rank_cursor=\"start\"")
	}

	end := min(state.count+limit, len(order))
	page := Page{
		Lane:             window.Lane,
		Order:            window.Order,
		IDs:              order[state.count:end],
		PageCursor:       state.encode(),
		Position:         end,
		WindowRows:       len(order),
		Excluded:         excluded,
		WindowEnd:        end == len(order),
		LexicalTruncated: window.LexicalTruncated,
	}
	if !page.WindowEnd {
		next := state
		next.digest = next.digest.add(page.IDs)
		next.count = end
		page.NextCursor = next.encode()
	}
	return page, nil
}
