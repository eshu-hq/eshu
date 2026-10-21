// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"reflect"
	"testing"
)

// TestNamedRowsToMapsRendersRowMaps pins the stored path's merge input: one
// row map per named row with exactly the four merge-identity keys, so stored
// rows merge and dedup exactly as live rows do.
func TestNamedRowsToMapsRendersRowMaps(t *testing.T) {
	t.Parallel()

	got := NamedRowsToMaps([]NamedRow{
		{Path: "overlays/prod/kustomization.yaml", SourceRepo: "svc", RelativePath: "overlays/prod/kustomization.yaml", EvidenceKind: "kustomize"},
		{Path: "", SourceRepo: "", RelativePath: "", EvidenceKind: ""},
	})
	want := []map[string]any{
		{"path": "overlays/prod/kustomization.yaml", "source_repo": "svc", "relative_path": "overlays/prod/kustomization.yaml", "evidence_kind": "kustomize"},
		{"path": "", "source_repo": "", "relative_path": "", "evidence_kind": ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NamedRowsToMaps = %#v, want %#v", got, want)
	}
	for _, row := range got {
		if len(row) != len(configPathKeyOrder) {
			t.Fatalf("NamedRowsToMaps row has %d keys, want exactly the %d merge-identity keys: %#v", len(row), len(configPathKeyOrder), row)
		}
	}
}

// TestNamedRowsToMapsEmptyIsNil keeps the merge's nil contract: no rows in
// means no rows out, never an empty non-nil list.
func TestNamedRowsToMapsEmptyIsNil(t *testing.T) {
	t.Parallel()

	if got := NamedRowsToMaps(nil); got != nil {
		t.Fatalf("NamedRowsToMaps(nil) = %#v, want nil", got)
	}
	if got := NamedRowsToMaps([]NamedRow{}); got != nil {
		t.Fatalf("NamedRowsToMaps(empty) = %#v, want nil", got)
	}
}

// TestNamedRowsToMapsMergeParity is the unit half of the stored identical-row
// proof: named rows converted and merged equal the same rows merged as maps,
// so the converter cannot skew the one merge and dedup.
func TestNamedRowsToMapsMergeParity(t *testing.T) {
	t.Parallel()

	named := []NamedRow{
		{Path: "b.yaml", SourceRepo: "svc", RelativePath: "b.yaml", EvidenceKind: "ansible"},
		{Path: "a.yaml", SourceRepo: "svc", RelativePath: "a.yaml", EvidenceKind: "ansible"},
		{Path: "a.yaml", SourceRepo: "svc", RelativePath: "a.yaml", EvidenceKind: "ansible"},
	}
	maps := []map[string]any{
		{"path": "b.yaml", "source_repo": "svc", "relative_path": "b.yaml", "evidence_kind": "ansible"},
		{"path": "a.yaml", "source_repo": "svc", "relative_path": "a.yaml", "evidence_kind": "ansible"},
		{"path": "a.yaml", "source_repo": "svc", "relative_path": "a.yaml", "evidence_kind": "ansible"},
	}
	viaConverter := MergeConfigPaths(NamedRowsToMaps(named))
	direct := MergeConfigPaths(maps)
	if !reflect.DeepEqual(viaConverter, direct) {
		t.Fatalf("MergeConfigPaths(NamedRowsToMaps) = %#v, want %#v", viaConverter, direct)
	}
}
