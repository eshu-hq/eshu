// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// fakeGraphReader is this package's adapter onto querytestutil.FakeGraphReader.
// The behavior lives there so handler families moving out of package query
// (#6060, epic #6053) can reach it -- a _test.go symbol is not importable
// across a package boundary. codequery keeps its own identically-shaped copy
// (code_relationships_graph_test.go) for the same reason, once removed from
// this package by the #6060 CodeHandler move: the field names stay lowercase
// and unchanged here so the many root files that build this double with
// keyed literals did not have to be rewritten.
type fakeGraphReader struct {
	run         func(context.Context, string, map[string]any) ([]map[string]any, error)
	runIncoming func(context.Context, string, map[string]any) ([]map[string]any, error)
	runSingle   func(context.Context, string, map[string]any) (map[string]any, error)
}

// delegate builds the shared fake from this adapter's fields.
func (f fakeGraphReader) delegate() querytestutil.FakeGraphReader {
	return querytestutil.FakeGraphReader{
		RunFn:         f.run,
		RunIncomingFn: f.runIncoming,
		RunSingleFn:   f.runSingle,
	}
}

func (f fakeGraphReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return f.delegate().Run(ctx, cypher, params)
}

func (f fakeGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	return f.delegate().RunSingle(ctx, cypher, params)
}
