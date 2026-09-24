// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !race

// The race detector's instrumentation allocates on map iteration, so an
// allocation count is only meaningful in a non-race build. The behavioral
// string-row tests stay in index_key_guard_string_rows_test.go and run under
// -race as usual.

package graph

import (
	"fmt"
	"testing"
)

// TestGuardIndexKeyWritesStringRowsDoNotAllocateWhenNothingDropped pins the
// steady-state cost of the []map[string]string arm: sizes are read straight
// off the string maps, so a guarded write with nothing oversized allocates
// nothing per row.
func TestGuardIndexKeyWritesStringRowsDoNotAllocateWhenNothingDropped(t *testing.T) {
	rows := make([]map[string]string, 500)
	for i := range rows {
		rows[i] = stringFunctionRow(fmt.Sprintf("content-entity:e_%012d", i), fmt.Sprintf("handler%d", i), "/repo/src/service/handlers.go")
	}
	params := map[string]any{"rows": rows}
	GuardIndexKeyWrites(semanticFunctionShape, params) // warm the plan cache

	allocs := testing.AllocsPerRun(20, func() {
		if _, dropped, _ := GuardIndexKeyWrites(semanticFunctionShape, params); dropped != nil {
			t.Fatal("unexpected drop")
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs per guarded []map[string]string write = %v, want 0", allocs)
	}
}
