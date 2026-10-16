// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inheritance

import "testing"

func TestMaterializedEdgeTypesReturnsCopy(t *testing.T) {
	t.Parallel()

	first := MaterializedEdgeTypes()
	if len(first) != 4 {
		t.Fatalf("MaterializedEdgeTypes() has %d entries, want 4", len(first))
	}
	delete(first, "INHERITS")

	if _, ok := MaterializedEdgeTypes()["INHERITS"]; !ok {
		t.Error("deleting from the returned map mutated the package registry")
	}
}
