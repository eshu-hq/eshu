// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"sort"
	"testing"
)

// TestAssetTypeInventoryIsSortedAndNonEmpty proves the documented invariant
// (asset_types.go's doc comment claims the inventory is "sorted") actually
// holds, and that the exported accessor returns a non-empty vocabulary for a
// maintainer's parity tooling to iterate.
func TestAssetTypeInventoryIsSortedAndNonEmpty(t *testing.T) {
	inventory := AssetTypeInventory()
	if len(inventory) == 0 {
		t.Fatal("AssetTypeInventory() returned an empty slice")
	}
	if !sort.StringsAreSorted(inventory) {
		t.Fatal("AssetTypeInventory() is not sorted")
	}
}

// TestAssetTypeInventoryReturnsDefensiveCopy proves a caller mutating the
// returned slice cannot corrupt the package's internal inventory — the
// documented "defensive copy" contract on AssetTypeInventory.
func TestAssetTypeInventoryReturnsDefensiveCopy(t *testing.T) {
	first := AssetTypeInventory()
	if len(first) == 0 {
		t.Fatal("AssetTypeInventory() returned an empty slice")
	}
	original := first[0]
	first[0] = "mutated-value"

	second := AssetTypeInventory()
	if second[0] != original {
		t.Fatalf("mutating a returned slice corrupted the internal inventory: got %q, want %q", second[0], original)
	}
}

// TestAssetTypeInventoryHasNoDuplicates proves every asset type is listed
// exactly once, so buildResourceFacts's round-robin cycling never
// double-weights one asset type over another.
func TestAssetTypeInventoryHasNoDuplicates(t *testing.T) {
	seen := make(map[string]bool, len(assetTypeInventory))
	for _, assetType := range assetTypeInventory {
		if seen[assetType] {
			t.Errorf("asset type %q appears more than once in assetTypeInventory", assetType)
		}
		seen[assetType] = true
	}
}
