// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
)

// boltPostureExistenceReader adapts the bolt test runner to
// PostureExistenceReader so the live posture-reader benchmarks exercise the
// exact production read seam (issue #5652), not a hand-rolled mock.
type boltPostureExistenceReader struct {
	runner *boltRetractTestRunner
}

func (r *boltPostureExistenceReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return r.runner.runCypher(ctx, cypher, params)
}
