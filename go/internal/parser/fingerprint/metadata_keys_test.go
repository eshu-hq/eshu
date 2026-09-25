// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fingerprint

import (
	"slices"
	"testing"
)

// TestMetadataKeysListsEveryEntityMetadataKey pins the strip list the query
// layer removes from API rows to the constants the parsers write, so a sixth
// fingerprint key added to attach.go cannot be forgotten by the strip.
func TestMetadataKeysListsEveryEntityMetadataKey(t *testing.T) {
	t.Parallel()

	want := []string{KeyExact, KeyRenamed, KeySketch, KeyTokenCount, KeyShingles}
	got := MetadataKeys()
	if len(got) != len(want) {
		t.Fatalf("len(MetadataKeys()) = %d, want %d: %v", len(got), len(want), got)
	}
	for _, key := range want {
		if !slices.Contains(got, key) {
			t.Errorf("MetadataKeys() = %v, missing %q", got, key)
		}
	}
	if slices.Contains(got, StatsKey) {
		t.Errorf("MetadataKeys() = %v contains %q, which is a payload key, not entity metadata", got, StatsKey)
	}
}

// TestMetadataKeysReturnsIndependentCopy proves a caller mutating the result
// cannot corrupt the list every later caller sees.
func TestMetadataKeysReturnsIndependentCopy(t *testing.T) {
	t.Parallel()

	first := MetadataKeys()
	first[0] = "mutated"
	if got := MetadataKeys()[0]; got == "mutated" {
		t.Fatalf("MetadataKeys()[0] = %q after caller mutation, want an independent copy", got)
	}
}

// TestStripMetadataRemovesOnlyFingerprintKeys proves the strip deletes every
// listed key, keeps everything else, and tolerates a nil map.
func TestStripMetadataRemovesOnlyFingerprintKeys(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{"docstring": "kept", "body_hash": "kept"}
	for _, key := range MetadataKeys() {
		metadata[key] = "x"
	}
	StripMetadata(metadata)
	if len(metadata) != 2 || metadata["docstring"] != "kept" || metadata["body_hash"] != "kept" {
		t.Fatalf("StripMetadata() left %#v, want only the two non-fingerprint keys", metadata)
	}
	StripMetadata(nil)
}
