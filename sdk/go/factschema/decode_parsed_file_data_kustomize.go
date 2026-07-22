// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"fmt"

	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// DecodeParsedFileDataKustomizeOverlays decodes the "kustomize_overlays"
// inner slice of a parsed_file_data map into a typed
// []codegraphv1.KustomizeOverlay. An absent key decodes to a nil slice with
// no error, and a malformed element is skipped rather than failing the whole
// decode (decodeParsedFileDataTolerantSlice), matching the tolerance every
// other #5445 accessor in this file family gives a real
// go/internal/relationships / go/internal/storage/cypher call site. An error
// is returned only when the key is present but not any recognized slice
// shape at all.
func DecodeParsedFileDataKustomizeOverlays(parsedFileData map[string]any) ([]codegraphv1.KustomizeOverlay, error) {
	raw, present := parsedFileData["kustomize_overlays"]
	if !present || raw == nil {
		return nil, nil
	}
	overlays, ok := decodeParsedFileDataTolerantSlice[codegraphv1.KustomizeOverlay](raw)
	if !ok {
		return nil, fmt.Errorf("factschema: kustomize_overlays: want slice of JSON objects, got %T", raw)
	}
	return overlays, nil
}
