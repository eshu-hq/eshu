// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"strings"
)

// FakeRepoGraphReader is a graph-read double for getRepositoryContext tests.
// It dispatches on Cypher fragment content, so one fake can answer every
// query getRepositoryContext issues with data controlled per test.
//
// It lives here rather than in a package query test file for the same reason
// as FakeGraphReader: a symbol declared in a _test.go file is not part of the
// importable package, so the handler family that moves getRepositoryContext's
// tests out of root for #6060 could not otherwise reach this fake.
//
// FakeRepoGraphReader and FakeWorkloadGraphReader look alike and are NOT the
// same type. FakeRepoGraphReader's RunSingle has a single-entry fallback (see
// RunSingle) that FakeWorkloadGraphReader deliberately lacks. Unifying them
// behind a shared type -- even with a flag -- would change what every
// workload test observes, silently, because they would keep compiling and
// most would keep passing.
type FakeRepoGraphReader struct {
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
	// RunSingleByMatch (including its single-entry fallback) is not consulted.
	RunSingleFn func(context.Context, string, map[string]any) (map[string]any, error)
}

// Run dispatches to RunFn when set, and otherwise returns the rows for the
// longest RunByMatch fragment contained in cypher. The longest match wins so a
// test can register both a general and a more specific fragment without the
// general one shadowing the specific one. Two matching fragments of EQUAL
// length are unspecified: the comparison is strictly greater-than, so the
// winner is whichever Go's randomized map iteration reaches first. Register
// fragments of distinct lengths rather than relying on that order.
func (f FakeRepoGraphReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
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
// When no fragment matches, cypher is the narrow single-repository lookup
// (`MATCH (r:Repository {id: $repo_id})`), and exactly one row is registered
// in RunSingleByMatch, RunSingle returns that row. A test that registers only
// the repository row -- and does not bother keying it to the exact lookup
// fragment -- still gets it back. This fallback exists only here:
// FakeWorkloadGraphReader has no single-repository query to fall back to, and
// must not gain one by copying this method.
func (f FakeRepoGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
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
	if bestRow == nil && strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") && len(f.RunSingleByMatch) == 1 {
		for _, row := range f.RunSingleByMatch {
			return row, nil
		}
	}
	return bestRow, nil
}
