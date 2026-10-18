// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
)

// The fakes below are copies of test doubles that moved to the cloud leaves
// with their writer families (#6694). Test fakes cannot be imported across
// the package split, so each side carries its own copy; keep the copies
// behavior-identical.

// noopGroupExecutor records nothing and returns nil so the benchmark measures
// statement construction and batching cost, not backend round trips.
type noopGroupExecutor struct{}

func (noopGroupExecutor) Execute(context.Context, Statement) error {
	return nil
}

func (noopGroupExecutor) ExecuteGroup(context.Context, []Statement) error {
	return nil
}

// boltPostureExistenceReader adapts the bolt test runner to
// PostureExistenceReader so the live posture-reader benchmarks exercise the
// exact production read seam (issue #5652), not a hand-rolled mock.
type boltPostureExistenceReader struct {
	runner *boltRetractTestRunner
}

func (r *boltPostureExistenceReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return r.runner.runCypher(ctx, cypher, params)
}
