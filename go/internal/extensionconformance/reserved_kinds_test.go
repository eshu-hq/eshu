// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionconformance

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestReservedFactKindsUnreservesOnlyDeclaredKinds pins the grant boundary of
// the conformance harness: the kinds a loaded manifest declares are the kinds a
// live producer grant covers, so those and only those leave the reserved set.
// Every other core-owned kind must stay reserved, or a component could emit a
// core kind its grant never named and still pass conformance.
func TestReservedFactKindsUnreservesOnlyDeclaredKinds(t *testing.T) {
	t.Parallel()

	core := facts.CoreFactKinds()
	if len(core) < 4 {
		t.Fatalf("CoreFactKinds() = %d kinds, want at least 4 for this test", len(core))
	}
	granted, alsoGranted, undeclared := core[0], core[1], core[2]

	tests := []struct {
		name     string
		declared []string
		want     []string
	}{
		{name: "no declared kinds reserves every core kind", declared: nil, want: core},
		{name: "a namespaced kind reserves every core kind", declared: []string{"dev.example.record"}, want: core},
		{name: "one declared core kind is un-reserved", declared: []string{granted}, want: without(core, granted)},
		{name: "two declared core kinds are un-reserved", declared: []string{granted, alsoGranted}, want: without(core, granted, alsoGranted)},
		{name: "surrounding whitespace on a declared kind still matches it", declared: []string{"  " + granted + " "}, want: without(core, granted)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var manifest component.Manifest
			for _, kind := range tt.declared {
				manifest.Spec.EmittedFacts = append(manifest.Spec.EmittedFacts, component.FactFamily{Kind: kind})
			}
			got := reservedFactKinds(manifest)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("reservedFactKinds() = %v, want %v", got, tt.want)
			}
			if !slices.Contains(got, undeclared) {
				t.Fatalf("reservedFactKinds() dropped the undeclared core kind %q", undeclared)
			}
		})
	}
}

// without returns kinds minus the listed kinds, leaving the input untouched.
func without(kinds []string, drop ...string) []string {
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		if !slices.Contains(drop, kind) {
			out = append(out, kind)
		}
	}
	return out
}
