// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"errors"
	"fmt"
	"testing"
)

// TestParseCursorRefusesACountPastTheWindow pins the n <= WindowSize
// hardening: a token whose checksum is valid but whose served count exceeds
// the window is malformed.
func TestParseCursorRefusesACountPastTheWindow(t *testing.T) {
	t.Parallel()
	token := cursor{tool: toolCodes[ToolFindCode], lane: laneCodes[LaneContent], count: WindowSize + 1}.encode()
	_, err := parseCursor(token)
	var rankErr *Error
	if !errors.As(err, &rankErr) || rankErr.Reason != ReasonMalformed {
		t.Fatalf("parseCursor(count=%d) error = %v, want malformed", WindowSize+1, err)
	}
	if _, err := parseCursor(cursor{tool: toolCodes[ToolFindCode], lane: laneCodes[LaneContent], count: WindowSize}.encode()); err != nil {
		t.Fatalf("parseCursor(count=%d) error = %v, want nil", WindowSize, err)
	}
}

// TestAdvanceRefusesToPassTheWindow pins that Advance cannot mint a cursor
// past WindowSize rows.
func TestAdvanceRefusesToPassTheWindow(t *testing.T) {
	t.Parallel()
	token := cursor{tool: toolCodes[ToolFindCode], lane: laneCodes[LaneContent], count: 150}.encode()
	ids := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		ids = append(ids, fmt.Sprintf("x%02d", i))
	}
	_, err := Advance(token, ids)
	var rankErr *Error
	if !errors.As(err, &rankErr) || rankErr.Reason != ReasonMalformed {
		t.Fatalf("Advance(150 + 60) error = %v, want malformed", err)
	}
	if _, err := Advance(token, ids[:50]); err != nil {
		t.Fatalf("Advance(150 + 50) error = %v, want nil", err)
	}
}

// TestWalkRefusesAWindowPastWindowSize pins the one producer of an
// unbounded served count: a window of more than WindowSize walkable rows is a
// caller error, so no cursor with a count past the window, which the uint16
// count field could wrap, is ever minted.
func TestWalkRefusesAWindowPastWindowSize(t *testing.T) {
	t.Parallel()
	rows := make([]Row, 0, WindowSize+1)
	for i := 0; i <= WindowSize; i++ {
		rows = append(rows, Row{ID: fmt.Sprintf("r%03d", i)})
	}
	window := Window{Tool: ToolFindCode, Lane: LaneGraph, Order: OrderGraphName, Rows: rows}
	page, err := Walk(window, nil, StartToken, WindowSize+1)
	var rankErr *Error
	if err == nil || errors.As(err, &rankErr) {
		t.Fatalf("Walk(%d rows) = %d ids, error %v; want a non-rank caller error", len(rows), len(page.IDs), err)
	}
	window.Rows = rows[:WindowSize]
	if _, err := Walk(window, nil, StartToken, WindowSize); err != nil {
		t.Fatalf("Walk(%d rows) error = %v, want nil", WindowSize, err)
	}
}
