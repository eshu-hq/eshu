// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// fakeGraphReader adapts querytestutil.FakeGraphReader to the field names the
// codequery tests already use. Neither read is reimplemented here: both live
// in querytestutil, the single home for the dispatch rules, so this adapter
// cannot drift from the fake the moved leaf families use. A symbol declared
// in a _test.go file cannot be imported across a package boundary, which is
// why the relationships leaf keeps its own twin of this adapter rather than
// importing this one.
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
