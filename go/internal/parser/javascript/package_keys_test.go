// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"maps"
	"slices"
	"testing"
)

// TestPackageKeyCallKindsAreRealCallsOnly pins the kind filter itself. Type
// references stay unkeyed today partly because the type-reference pass runs
// after annotatePackageImportCalls; this test keeps them unkeyed even if that
// order changes. Type references are needed for type liveness, not as calls.
func TestPackageKeyCallKindsAreRealCallsOnly(t *testing.T) {
	t.Parallel()

	got := slices.Sorted(maps.Keys(packageKeyCallKinds))
	want := []string{"constructor_call", "function_call", "jsx_component"}
	if !slices.Equal(got, want) {
		t.Fatalf("packageKeyCallKinds = %v, want exactly %v", got, want)
	}
	for _, kind := range []string{"typescript.type_reference", "javascript.function_value_reference", "javascript.hapi_route_handler_reference"} {
		if _, ok := packageKeyCallKinds[kind]; ok {
			t.Errorf("packageKeyCallKinds contains %q; only real runtime calls may carry a package key", kind)
		}
	}
}
