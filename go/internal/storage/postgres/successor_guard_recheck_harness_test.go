// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"fmt"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// guardTxState is what the commit transaction sees for the guard's in-tx
// prior recheck (#8750 window): the committed same-scope state at the scope
// lock, which a concurrent commit or Ack may have changed since the pre-tx
// sizing. Unset fields fall back to the base connection's answers.
type guardTxState struct {
	candidate        []any
	candidateMissing bool
	candidateErr     error
	// activeID overrides the active read when activeSet is true; empty
	// then means no active generation.
	activeID  string
	activeSet bool
	activeErr error
	targets   map[string][]any
	bases     map[string][]any
	counts    map[string][]any
	countErrs map[string]error
}

// errTxConnBusy is what pgx returns for a statement issued on a transaction
// while an earlier cursor on the same connection is still open.
var errTxConnBusy = errors.New("conn busy")

// txCursor tracks an open guard-read cursor on the commit transaction: one
// connection, so a nested read before Close fails as pgx does.
type txCursor struct {
	db.Rows
	open   *int
	closed bool
}

func (c *txCursor) Close() error {
	if !c.closed {
		c.closed = true
		*c.open--
	}
	return c.Rows.Close()
}

// txGuardRead answers the guard's reads on the commit transaction: the
// recheck's candidate and active reads, and the re-size's count and
// delta-union reads. routed is false for any other query. A read issued
// while another guard cursor is open fails with errTxConnBusy.
func (g *guardRoutingDB) txGuardRead(query string, args []any) (db.Rows, bool, error) {
	if !slices.Contains([]string{
		unchangedGenerationCandidateQuery, successorGuardActiveGenerationQuery,
		successorGuardCountQuery, deltaUnionTargetQuery, deltaUnionBaseQuery, deltaUnionBasePrunedQuery,
	}, query) {
		return nil, false, nil
	}
	g.txQueries = append(g.txQueries, fakeQueryCall{query: query, args: args})
	if g.txOpenCursors > 0 {
		return nil, true, errTxConnBusy
	}
	rows, err := g.txGuardAnswer(query, args)
	if err != nil {
		return nil, true, err
	}
	g.txOpenCursors++
	return &txCursor{Rows: rows, open: &g.txOpenCursors}, true, nil
}

// txGuardAnswer stages one in-tx guard read from inTx over the base rows.
func (g *guardRoutingDB) txGuardAnswer(query string, args []any) (db.Rows, error) {
	state := g.inTx
	if state == nil {
		state = &guardTxState{}
	}
	switch query {
	case unchangedGenerationCandidateQuery:
		if state.candidateErr != nil {
			return nil, state.candidateErr
		}
		if state.candidateMissing || (state.candidate == nil && g.candidateMissing) {
			return &queueFakeRows{}, nil
		}
		row := state.candidate
		if row == nil {
			row = g.candidate
		}
		return &queueFakeRows{rows: [][]any{row}}, nil
	case successorGuardActiveGenerationQuery:
		if state.activeErr != nil {
			return nil, state.activeErr
		}
		activeID := g.activeID
		if state.activeSet {
			activeID = state.activeID
		}
		if activeID == "" {
			return &queueFakeRows{}, nil
		}
		return &queueFakeRows{rows: [][]any{{activeID}}}, nil
	case successorGuardCountQuery:
		key := fmt.Sprint(args[1])
		if err := state.countErrs[key]; err != nil {
			return nil, err
		}
		return g.rowsFor(mergeGuardTable(g.counts, state.counts), args[1], false), nil
	case deltaUnionTargetQuery:
		return g.rowsFor(withUnionBaseColumn(mergeGuardTable(g.targets, state.targets)), args[1], false), nil
	case deltaUnionBaseQuery:
		return g.rowsFor(mergeGuardTable(g.bases, state.bases), args[1], true), nil
	default: // deltaUnionBasePrunedQuery: no retention event, the base is intact
		return &queueFakeRows{}, nil
	}
}

// withUnionBaseColumn pads {is_delta, baseline} target rows with an empty
// #8780 union-base column, the shape of a delta stored before the column
// existed; a three-column row passes through.
func withUnionBaseColumn(targets map[string][]any) map[string][]any {
	padded := make(map[string][]any, len(targets))
	for key, row := range targets {
		if len(row) == 2 {
			row = append(append([]any(nil), row...), "")
		}
		padded[key] = row
	}
	return padded
}

// mergeGuardTable overlays the in-tx rows on the base rows.
func mergeGuardTable(base, overlay map[string][]any) map[string][]any {
	merged := make(map[string][]any, len(base)+len(overlay))
	for key, row := range base {
		merged[key] = row
	}
	for key, row := range overlay {
		merged[key] = row
	}
	return merged
}

// countTxQueries counts the guard reads of one query identity on the commit
// transaction.
func (g *guardRoutingDB) countTxQueries(query string) int {
	n := 0
	for _, call := range g.txQueries {
		if call.query == query {
			n++
		}
	}
	return n
}

// txCountedGenerations returns how many in-tx successorGuardCountQuery reads
// ran per generation id.
func (g *guardRoutingDB) txCountedGenerations() map[string]int {
	counted := map[string]int{}
	for _, call := range g.txQueries {
		if call.query == successorGuardCountQuery {
			counted[fmt.Sprint(call.args[1])]++
		}
	}
	return counted
}

// baseGuardReadsAfterBegin counts the guard's sizing reads that ran on the
// base connection after the commit transaction opened. The re-size must run
// on the transaction, so this is zero.
func (g *guardRoutingDB) baseGuardReadsAfterBegin() int {
	n := 0
	for _, call := range g.queries[g.beginAt:] {
		switch call.query {
		case unchangedGenerationCandidateQuery, successorGuardActiveGenerationQuery,
			successorGuardCountQuery, deltaUnionTargetQuery, deltaUnionBaseQuery:
			n++
		}
	}
	return n
}

// orderIndex returns the position of the first tx statement equal to query,
// or -1.
func (g *guardRoutingDB) orderIndex(query string) int {
	return slices.Index(g.order, query)
}
