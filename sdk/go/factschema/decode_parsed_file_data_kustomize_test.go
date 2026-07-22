// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import "testing"

// The fixtures below mirror the exact row shape go/internal/parser/yaml/
// kustomize_semantics.go's parseKustomization emits: "bases" is the
// producer's own normalized, merged (deprecated `bases:` + directory-shaped
// `resources:`) local-overlay-hierarchy field (collectKustomizeBaseRefs),
// already filtered to exclude remote refs and .yaml/.yml/.json-suffixed
// entries. This decode function types that field for the #5445 EXTENDS_BASE
// edge; every other producer field survives via Attributes.

// TestDecodeParsedFileDataKustomizeOverlays_TypedRows proves the
// kustomize_overlays inner key decodes into typed []KustomizeOverlay rows
// exposing Bases -- the field the EXTENDS_BASE edge resolver reads -- while
// every other producer field (namespace, resources, resource_refs,
// helm_refs, image_refs, patches, patch_targets, path, lang) survives in
// Attributes.
func TestDecodeParsedFileDataKustomizeOverlays_TypedRows(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"kustomize_overlays": []any{
			map[string]any{
				"name":          "kustomization",
				"line_number":   float64(1),
				"namespace":     "production",
				"resources":     []any{"../base"},
				"bases":         []any{"../base"},
				"resource_refs": []any{},
				"helm_refs":     []any{},
				"image_refs":    []any{},
				"patches":       []any{},
				"patch_targets": []any{},
				"path":          "overlays/prod/kustomization.yaml",
				"lang":          "yaml",
			},
		},
	}

	overlays, err := DecodeParsedFileDataKustomizeOverlays(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataKustomizeOverlays() error = %v, want nil", err)
	}
	if len(overlays) != 1 {
		t.Fatalf("len(overlays) = %d, want 1", len(overlays))
	}
	overlay := overlays[0]
	if len(overlay.Bases) != 1 || overlay.Bases[0] != "../base" {
		t.Fatalf("Bases = %#v, want [\"../base\"]", overlay.Bases)
	}
	if overlay.Attributes == nil {
		t.Fatal("Attributes = nil, want the non-read producer fields captured")
	}
	if got, ok := overlay.Attributes["namespace"].(string); !ok || got != "production" {
		t.Fatalf("Attributes[namespace] = %#v, want string \"production\"", overlay.Attributes["namespace"])
	}
	if got, ok := overlay.Attributes["path"].(string); !ok || got != "overlays/prod/kustomization.yaml" {
		t.Fatalf("Attributes[path] = %#v, want the file path", overlay.Attributes["path"])
	}
	for _, named := range []string{"bases"} {
		if _, leaked := overlay.Attributes[named]; leaked {
			t.Fatalf("named field %q leaked into Attributes; it must be a typed field", named)
		}
	}
}

// TestDecodeParsedFileDataKustomizeOverlays_Absent proves an absent
// kustomize_overlays key decodes to a nil slice with no error.
func TestDecodeParsedFileDataKustomizeOverlays_Absent(t *testing.T) {
	t.Parallel()

	overlays, err := DecodeParsedFileDataKustomizeOverlays(map[string]any{"lang": "yaml"})
	if err != nil {
		t.Fatalf("DecodeParsedFileDataKustomizeOverlays() error = %v, want nil", err)
	}
	if overlays != nil {
		t.Fatalf("overlays = %#v, want nil for an absent kustomize_overlays key", overlays)
	}
}

// TestDecodeParsedFileDataKustomizeOverlays_NoLocalBases proves a
// kustomization.yaml with only a remote base (the common case -- see the
// tests/fixtures/ecosystems/kustomize-deployable-overlay B-7 fixture) decodes
// to an empty (nil) Bases slice with no error, matching
// collectKustomizeBaseRefs' remote-ref filtering.
func TestDecodeParsedFileDataKustomizeOverlays_NoLocalBases(t *testing.T) {
	t.Parallel()

	pfd := map[string]any{
		"kustomize_overlays": []any{
			map[string]any{
				"name":  "kustomization",
				"bases": []any{},
				"path":  "kustomization.yaml",
				"lang":  "yaml",
			},
		},
	}

	overlays, err := DecodeParsedFileDataKustomizeOverlays(pfd)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataKustomizeOverlays() error = %v, want nil", err)
	}
	if len(overlays) != 1 {
		t.Fatalf("len(overlays) = %d, want 1", len(overlays))
	}
	if len(overlays[0].Bases) != 0 {
		t.Fatalf("Bases = %#v, want empty", overlays[0].Bases)
	}
}

// TestDecodeParsedFileDataKustomizeOverlays_WrongTopLevelShape proves a
// present-but-not-any-recognized-slice-shape kustomize_overlays value
// surfaces a wrapped error rather than silently decoding to an empty slice.
func TestDecodeParsedFileDataKustomizeOverlays_WrongTopLevelShape(t *testing.T) {
	t.Parallel()

	_, err := DecodeParsedFileDataKustomizeOverlays(map[string]any{
		"kustomize_overlays": "not-a-slice",
	})
	if err == nil {
		t.Fatal("DecodeParsedFileDataKustomizeOverlays() error = nil, want error for a non-slice kustomize_overlays value")
	}
}

// TestDecodeParsedFileDataKustomizeOverlays_MalformedElementSkipped proves a
// non-object element inside an otherwise well-formed kustomize_overlays slice
// is SKIPPED, not an aborting error, matching decodeParsedFileDataTolerantSlice's
// per-element tolerance.
func TestDecodeParsedFileDataKustomizeOverlays_MalformedElementSkipped(t *testing.T) {
	t.Parallel()

	overlays, err := DecodeParsedFileDataKustomizeOverlays(map[string]any{
		"kustomize_overlays": []any{
			"not-an-object",
			map[string]any{"name": "kustomization", "bases": []any{"../base"}, "path": "kustomization.yaml"},
		},
	})
	if err != nil {
		t.Fatalf("DecodeParsedFileDataKustomizeOverlays() error = %v, want nil (malformed element skipped)", err)
	}
	if len(overlays) != 1 || len(overlays[0].Bases) != 1 || overlays[0].Bases[0] != "../base" {
		t.Fatalf("overlays = %#v, want one row for the well-formed element", overlays)
	}
}
