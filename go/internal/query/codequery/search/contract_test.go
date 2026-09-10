// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"testing"
)

func TestMergeMetadataPrefersGraphKeys(t *testing.T) {
	merged := MergeMetadata(map[string]any{"a": "graph", "b": "graph"}, map[string]any{"b": "content", "c": "content"})
	if merged["a"] != "graph" || merged["b"] != "graph" || merged["c"] != "content" {
		t.Fatalf("merged = %v, want graph keys to win", merged)
	}
}

func TestMergeMetadataAbsentStaysNil(t *testing.T) {
	if MergeMetadata(nil, nil) != nil {
		t.Fatal("MergeMetadata(nil, nil) != nil, want nil absent shape")
	}
	if got := MergeMetadata(nil, map[string]any{}); got != nil {
		t.Fatalf("MergeMetadata(nil, {}) = %v, want nil", got)
	}
}

func TestMergeMetadataClonesSides(t *testing.T) {
	content := map[string]any{"c": "content"}
	merged := MergeMetadata(nil, content)
	merged["c"] = "mutated"
	if content["c"] != "content" {
		t.Fatal("merge aliased the content side, want a copy")
	}
}
