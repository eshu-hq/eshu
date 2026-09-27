// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// FakeWorkloadGraphReader is a graph-read double for getWorkloadContext
// tests. It dispatches on Cypher fragment content, the same way
// FakeRepoGraphReader does for getRepositoryContext.
//
// FakeWorkloadGraphReader and FakeRepoGraphReader are near-identical and
// deliberately separate types. FakeRepoGraphReader's RunSingle falls back to a
// sole registered row for the narrow single-repository lookup;
// FakeWorkloadGraphReader has no such lookup and no such fallback. Do not add
// one here, and do not merge the two types behind a shared implementation with
// a flag -- that would give every workload test the repository fallback's
// behavior too, and workload tests would keep compiling, and most would keep
// passing, while silently asserting on rows the fake invented rather than the
// rows the test registered.
type FakeWorkloadGraphReader struct {
	// RunSingleByMatch maps a Cypher fragment to the row RunSingle returns
	// when that fragment is the longest match against the query text.
	RunSingleByMatch map[string]map[string]any
	// RunByMatch maps a Cypher fragment to the rows Run returns when that
	// fragment is the longest match against the query text.
	RunByMatch map[string][]map[string]any
	// RunFn, when set, answers every Run call directly and RunByMatch is not
	// consulted.
	RunFn func(context.Context, string, map[string]any) ([]map[string]any, error)
	// RunSingleFn, when set, answers every RunSingle call directly and
	// RunSingleByMatch is not consulted.
	RunSingleFn func(context.Context, string, map[string]any) (map[string]any, error)
}

// Run dispatches to RunFn when set, and otherwise returns the rows for the
// longest RunByMatch fragment contained in cypher. The longest match wins so a
// test can register both a general and a more specific fragment without the
// general one shadowing the specific one. Two matching fragments of EQUAL
// length are unspecified: the comparison is strictly greater-than, so the
// winner is whichever Go's randomized map iteration reaches first. Register
// fragments of distinct lengths rather than relying on that order.
func (f FakeWorkloadGraphReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if f.RunFn != nil {
		return f.RunFn(ctx, cypher, params)
	}
	var (
		bestRows []map[string]any
		bestLen  int
	)
	for fragment, rows := range f.RunByMatch {
		if strings.Contains(cypher, fragment) && len(fragment) > bestLen {
			bestRows = rows
			bestLen = len(fragment)
		}
	}
	return bestRows, nil
}

// RunSingle dispatches to RunSingleFn when set, and otherwise returns the row
// for the longest RunSingleByMatch fragment contained in cypher.
//
// Unlike FakeRepoGraphReader, an unmatched cypher returns nil here regardless
// of how many rows RunSingleByMatch holds. getWorkloadContext has no narrow
// single-entity lookup for a fallback to stand in for, and adding one would
// hand a workload test a row it never registered for that query.
func (f FakeWorkloadGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	if f.RunSingleFn != nil {
		return f.RunSingleFn(ctx, cypher, params)
	}
	var (
		bestRow map[string]any
		bestLen int
	)
	for fragment, row := range f.RunSingleByMatch {
		if strings.Contains(cypher, fragment) && len(fragment) > bestLen {
			bestRow = row
			bestLen = len(fragment)
		}
	}
	return bestRow, nil
}

// OCIBoundedStatementFixture configures one bounded OCI registry-truth
// statement (#6590) for OCIBoundedFakeReader: which Cypher fragment selects
// it, which IN-list parameter carries its batch keys, which row field is the
// statement's ORDER BY anchor, and every row the graph holds per key.
type OCIBoundedStatementFixture struct {
	// CypherContains selects this statement: the fixture whose fragment the
	// queried Cypher text contains wins.
	CypherContains string
	// KeyParam is the Cypher parameter name holding the batch's IN-list keys
	// ("image_refs" or "digests").
	KeyParam string
	// KeyField is the row field the statement's ORDER BY sorts on first
	// (image_ref or digest).
	KeyField string
	// RowsByKey maps one IN-list key to every row the graph holds for it,
	// in the statement's own tie-break order (this fixture sorts by
	// KeyField only and keeps RowsByKey's order for rows sharing a key).
	RowsByKey map[string][]map[string]any
}

// OCIBoundedFakeReader is a graph-read double for the bounded OCI
// registry-truth statements (ociTagObservationByRefCypher,
// ociImageByDigestCypher). It answers from the OCIBoundedStatementFixture
// whose CypherContains matches, sorted ascending by KeyField and sliced to
// $row_limit -- the same shape advanceOCIBoundedRead expects from the real
// backend. It fails the test if $row_limit is absent from params, or is not
// an int, or no fixture matches: every OCI registry-truth statement carries
// LIMIT $row_limit (#6590), and a silent zero-row answer would hide that
// regression rather than fail the test that depends on it.
type OCIBoundedFakeReader struct {
	T          *testing.T
	Statements []OCIBoundedStatementFixture
}

// Run implements querycontract.GraphQuery.
func (f OCIBoundedFakeReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	f.T.Helper()
	for _, stmt := range f.Statements {
		if !strings.Contains(cypher, stmt.CypherContains) {
			continue
		}
		limitRaw, ok := params["row_limit"]
		if !ok {
			f.T.Fatalf("OCIBoundedFakeReader: row_limit parameter is absent from params for statement %q", stmt.CypherContains)
			return nil, nil
		}
		limit, ok := limitRaw.(int)
		if !ok {
			f.T.Fatalf("OCIBoundedFakeReader: row_limit = %#v, want int", limitRaw)
			return nil, nil
		}
		keys, ok := params[stmt.KeyParam].([]string)
		if !ok {
			f.T.Fatalf("OCIBoundedFakeReader: %s param = %#v, want []string", stmt.KeyParam, params[stmt.KeyParam])
			return nil, nil
		}
		rows := make([]map[string]any, 0, len(keys))
		for _, key := range keys {
			rows = append(rows, stmt.RowsByKey[key]...)
		}
		sort.SliceStable(rows, func(i, j int) bool {
			return querycontract.StringVal(rows[i], stmt.KeyField) < querycontract.StringVal(rows[j], stmt.KeyField)
		})
		if len(rows) > limit {
			rows = rows[:limit]
		}
		return rows, nil
	}
	return nil, nil
}

// RunSingle implements querycontract.GraphQuery by taking the first row Run
// would return.
func (f OCIBoundedFakeReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := f.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}
