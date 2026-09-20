// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
)

// assertBoltCount duplicates the live-backend count assertion from the
// edge/writer leaf (edge/writer/retract_repo_live_test.go). Test helpers
// cannot be imported across the package split, so each side carries its
// own copy.

func assertBoltCount(
	t *testing.T,
	ctx context.Context,
	runner *boltRetractTestRunner,
	query string,
	params map[string]any,
	want int64,
	label string,
) {
	t.Helper()
	got, err := boltCount(ctx, runner, query, params)
	if err != nil {
		t.Fatalf("count %s: %v", label, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", label, got, want)
	}
}
