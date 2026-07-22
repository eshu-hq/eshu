// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// This file types the parsed_file_data "kustomize_overlays" inner key
// (Contract System v1 §7 incremental migration, issue #5445 slice 3),
// following the exact pattern parsed_file_data_terraform.go established:
// name only the field a consumer reads, pass every other producer field
// through untyped via Attributes.
//
// Single producer: go/internal/parser/yaml/kustomize_semantics.go's
// parseKustomization, one row per kustomization.yaml file (isKustomization).

// KustomizeOverlay is the typed view of the single parsed_file_data
// "kustomize_overlays" inner-slice entry a kustomization.yaml file produces.
// Only Bases is named -- the field the EXTENDS_BASE edge resolver
// (go/internal/storage/cypher) reads to link an overlay to the local base
// directories it declares. Bases is parseKustomization's own normalized,
// merged view of the deprecated `bases:` field and any directory-shaped
// `resources:` entry (collectKustomizeBaseRefs): remote refs (containing
// "://") and .yaml/.yml/.json-suffixed entries are already filtered out by
// the producer, so every value here is a same-repo relative directory
// reference. Every other producer field (name, line_number, namespace,
// resources, resource_refs, helm_refs, image_refs, patches, patch_targets,
// path, lang) survives in Attributes, preserving each value's JSON-native Go
// type.
type KustomizeOverlay struct {
	// Bases is the overlay's local (non-remote) base directory references,
	// already deduplicated and sorted by the producer. Empty when the file
	// declares only remote bases or no bases at all -- the common case (see
	// tests/fixtures/ecosystems/kustomize-deployable-overlay, whose sole base
	// is a remote git ref).
	Bases []string `json:"bases,omitempty"`
	// Attributes carries every producer field with no named struct field
	// above, preserving each value's JSON-native Go type.
	Attributes map[string]any `json:"-"`
}
