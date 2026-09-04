// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import "context"

// FakeGraphReaderWithSingle is a graph-read double that scripts Run and
// RunSingle independently, so the dependency-path and resource-to-code
// fallback paths can be exercised separately.
//
// It is intentionally separate from FakeGraphReader: that double routes on
// the query text (incoming-edge traversals and the dead-code probe get
// special answers), while this one answers every Run from RunFn and every
// RunSingle from RunSingleFn with no routing. Tests that script one answer
// for all queries of a kind depend on that plain dispatch; repointing them
// at FakeGraphReader would silently change what the handler under test sees.
// The zero value is usable: nil funcs answer with no rows rather than
// panicking.
type FakeGraphReaderWithSingle struct {
	RunFn       func(context.Context, string, map[string]any) ([]map[string]any, error)
	RunSingleFn func(context.Context, string, map[string]any) (map[string]any, error)
}

// Run answers a multi-row read from RunFn, or with no rows when it is nil.
func (f FakeGraphReaderWithSingle) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if f.RunFn == nil {
		return nil, nil
	}
	return f.RunFn(ctx, cypher, params)
}

// RunSingle answers a single-row read from RunSingleFn, or with no row when
// it is nil.
func (f FakeGraphReaderWithSingle) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	if f.RunSingleFn == nil {
		return nil, nil
	}
	return f.RunSingleFn(ctx, cypher, params)
}
