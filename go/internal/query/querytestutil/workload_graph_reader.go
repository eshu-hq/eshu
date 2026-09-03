// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"strings"
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
