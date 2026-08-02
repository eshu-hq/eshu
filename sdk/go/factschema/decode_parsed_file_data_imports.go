// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"fmt"

	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// DecodeParsedFileDataImports decodes the "imports" inner slice of a
// parsed_file_data map into a typed []codegraphv1.Import — the per-file import
// statements every language parser writes.
//
// An absent or null key decodes to a nil slice with no error, matching the
// tolerant mapSlice(nil) read the reducer's existing import consumers use (a
// file with no imports yields no rows). A present value that is not a slice of
// JSON objects, or an element whose named fields do not coerce, returns an
// error so a strict caller can observe the malformed bucket instead of reading
// it as "this file imports nothing".
//
// Pass factschema.WithoutAttributesRemainder when only the named fields are
// read: this bucket is decoded once per parsed file across an entire
// repository generation, and the per-entry Attributes remainder map is
// allocated and immediately discarded for such a caller.
func DecodeParsedFileDataImports(parsedFileData map[string]any, opts ...DecodeOption) ([]codegraphv1.Import, error) {
	raw, present := parsedFileData["imports"]
	if !present || raw == nil {
		return nil, nil
	}
	elems, ok := asObjectSlice(raw)
	if !ok {
		return nil, fmt.Errorf("factschema: imports: want slice of JSON objects, got %T", raw)
	}
	cfg := resolveDecodeConfig(opts)
	entries := make([]codegraphv1.Import, 0, len(elems))
	for i, elem := range elems {
		var entry codegraphv1.Import
		if err := decodeMapIntoWith(elem, &entry, cfg); err != nil {
			return nil, fmt.Errorf("factschema: imports[%d]: %w", i, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
