// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "testing"

// TestRouterFieldsEmptyUntilCurated pins the scaffold contract: Intents and
// Canonical exist on Annotation and Entry but carry no data, so Parse/Annotate
// output is unchanged. The lane that fills the overlay (#8754) deletes this
// test in the same PR and asserts its own coverage instead.
func TestRouterFieldsEmptyUntilCurated(t *testing.T) {
	t.Parallel()
	for name, a := range annotations() {
		if len(a.Intents) != 0 || a.Canonical {
			t.Fatalf("annotation %q carries router fields %v/%v before #8754", name, a.Intents, a.Canonical)
		}
	}
	cat, err := Parse([]byte(sampleInventory))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cat.Annotate()
	for _, e := range cat.Entries() {
		if len(e.Intents) != 0 || e.Canonical {
			t.Fatalf("entry %q carries router fields %v/%v before #8754", e.Name, e.Intents, e.Canonical)
		}
	}
}
