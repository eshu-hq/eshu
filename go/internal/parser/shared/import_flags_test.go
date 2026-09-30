// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import "testing"

// TestImportFlagKeysAreTheWireStrings pins the three import-entry flag keys.
// They are a wire contract: the parsers emit them on "imports" entries, the typed
// codegraph/v1 Import view decodes them by json tag, and the projector folds the
// decoded fields onto IMPORTS edges (#7345). A rename here silently drops the flag
// on the other two sides, so it has to be an explicit change to this test as
// well as to the SDK tags; the projector's key-drift tests bind the SDK side.
func TestImportFlagKeysAreTheWireStrings(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"ImportFlagTypeOnly": "type_only",
		"ImportFlagDeferred": "deferred",
		"ImportFlagInferred": "inferred",
	}
	got := map[string]string{
		"ImportFlagTypeOnly": ImportFlagTypeOnly,
		"ImportFlagDeferred": ImportFlagDeferred,
		"ImportFlagInferred": ImportFlagInferred,
	}
	seen := make(map[string]string, len(got))
	for name, value := range got {
		if value != want[name] {
			t.Errorf("%s = %q, want %q", name, value, want[name])
		}
		if other, dup := seen[value]; dup {
			t.Errorf("%s and %s share the key %q; each flag needs its own", name, other, value)
		}
		seen[value] = name
	}
}
