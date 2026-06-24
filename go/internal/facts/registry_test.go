// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"
	"testing"
)

func TestCoreFactKindRegistryIncludesKnownFamilies(t *testing.T) {
	t.Parallel()

	kinds := CoreFactKinds()
	for _, want := range []string{
		AWSResourceFactKind,
		TerraformStateResourceFactKind,
		ServiceCatalogScorecardResultFactKind,
		SemanticCodeHintFactKind,
		VulnerabilitySuppressionFactKind,
	} {
		if !slices.Contains(kinds, want) {
			t.Fatalf("CoreFactKinds() missing %q: %v", want, kinds)
		}
		if !IsCoreFactKind(want) {
			t.Fatalf("IsCoreFactKind(%q) = false, want true", want)
		}
	}
	if IsCoreFactKind("dev.example.collector.finding") {
		t.Fatal("IsCoreFactKind(custom kind) = true, want false")
	}
}

func TestCoreFactKindRegistryIsStableAndImmutable(t *testing.T) {
	t.Parallel()

	kinds := CoreFactKinds()
	if !slices.IsSorted(kinds) {
		t.Fatalf("CoreFactKinds() = %v, want sorted order", kinds)
	}
	seen := map[string]struct{}{}
	for _, kind := range kinds {
		if _, ok := seen[kind]; ok {
			t.Fatalf("CoreFactKinds() contains duplicate %q: %v", kind, kinds)
		}
		seen[kind] = struct{}{}
	}
	if len(kinds) == 0 {
		t.Fatal("CoreFactKinds() is empty")
	}
	kinds[0] = "mutated"
	if slices.Contains(CoreFactKinds(), "mutated") {
		t.Fatal("CoreFactKinds() returned mutable backing storage")
	}
}
