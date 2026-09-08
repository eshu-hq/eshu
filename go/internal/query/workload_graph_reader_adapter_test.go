// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// fakeWorkloadGraphReader adapts querytestutil.FakeWorkloadGraphReader to the
// field names this package's tests already use, the same shape
// fakeRepoGraphReader uses in repository_context_test.go. The dispatch rules
// live once in querytestutil.FakeWorkloadGraphReader and are not duplicated
// here.
//
// FakeWorkloadGraphReader is a distinct type from FakeRepoGraphReader, not an
// alias for it: it has no single-entry RunSingle fallback, and this adapter
// must not grow one just because fakeRepoGraphReader's sibling has it.
//
// The adapter lived in workload_context_test.go until #6060 lane B B5 moved
// that file's tests to entity/; it stays here because the staying root
// service-context tests still construct it. See #6060.
type fakeWorkloadGraphReader struct {
	runSingleByMatch map[string]map[string]any
	runByMatch       map[string][]map[string]any
	run              func(context.Context, string, map[string]any) ([]map[string]any, error)
	runSingle        func(context.Context, string, map[string]any) (map[string]any, error)
}

// delegate builds the shared fake from this adapter's fields.
func (f fakeWorkloadGraphReader) delegate() querytestutil.FakeWorkloadGraphReader {
	return querytestutil.FakeWorkloadGraphReader{
		RunSingleByMatch: f.runSingleByMatch,
		RunByMatch:       f.runByMatch,
		RunFn:            f.run,
		RunSingleFn:      f.runSingle,
	}
}

func (f fakeWorkloadGraphReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return f.delegate().Run(ctx, cypher, params)
}

func (f fakeWorkloadGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	return f.delegate().RunSingle(ctx, cypher, params)
}
