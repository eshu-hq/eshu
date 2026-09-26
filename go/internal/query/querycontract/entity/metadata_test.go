// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
)

// TestFingerprintMetadataKeysListsEveryEntityMetadataKey pins the strip list the query
// layer removes from API rows to the constants the parsers write, so a sixth
// fingerprint key added to parser/fingerprint/attach.go cannot be forgotten by the strip.
func TestFingerprintMetadataKeysListsEveryEntityMetadataKey(t *testing.T) {
	t.Parallel()

	want := []string{fingerprint.KeyExact, fingerprint.KeyRenamed, fingerprint.KeySketch, fingerprint.KeyTokenCount, fingerprint.KeyShingles}
	got := FingerprintMetadataKeys()
	if len(got) != len(want) {
		t.Fatalf("len(FingerprintMetadataKeys()) = %d, want %d: %v", len(got), len(want), got)
	}
	for _, key := range want {
		if !slices.Contains(got, key) {
			t.Errorf("FingerprintMetadataKeys() = %v, missing %q", got, key)
		}
	}
	if slices.Contains(got, fingerprint.StatsKey) {
		t.Errorf("FingerprintMetadataKeys() = %v contains %q, which is a payload key, not entity metadata", got, fingerprint.StatsKey)
	}
}

// TestFingerprintMetadataKeysReturnsIndependentCopy proves a caller mutating the result
// cannot corrupt the list every later caller sees.
func TestFingerprintMetadataKeysReturnsIndependentCopy(t *testing.T) {
	t.Parallel()

	first := FingerprintMetadataKeys()
	first[0] = "mutated"
	if got := FingerprintMetadataKeys()[0]; got == "mutated" {
		t.Fatalf("FingerprintMetadataKeys()[0] = %q after caller mutation, want an independent copy", got)
	}
}

// TestStripFingerprintMetadataRemovesOnlyFingerprintKeys proves the strip deletes every
// listed key, keeps everything else, and tolerates a nil map.
func TestStripFingerprintMetadataRemovesOnlyFingerprintKeys(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{"docstring": "kept", "body_hash": "kept"}
	for _, key := range FingerprintMetadataKeys() {
		metadata[key] = "x"
	}
	StripFingerprintMetadata(metadata)
	if len(metadata) != 2 || metadata["docstring"] != "kept" || metadata["body_hash"] != "kept" {
		t.Fatalf("StripFingerprintMetadata() left %#v, want only the two non-fingerprint keys", metadata)
	}
	StripFingerprintMetadata(nil)
}
