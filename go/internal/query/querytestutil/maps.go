// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import "testing"

// MustMapField returns parent[key] as a map[string]any, failing the test when
// the key is absent or holds a different type. It is used to walk decoded
// OpenAPI and JSON envelope documents one level at a time, so that a failure
// names the exact key that broke rather than surfacing a nil-map panic several
// frames later.
func MustMapField(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()

	value, ok := parent[key]
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	typed, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("key %q type = %T, want map[string]any", key, value)
	}
	return typed
}
