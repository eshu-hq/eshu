// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import "sort"

// newlineIndex maps a byte offset in one source buffer to a 1-based source
// line number. Build it once per file with buildNewlineIndex and reuse it for
// every offset->line lookup on that buffer; lineAt is a binary search so
// repeated lookups stay cheap even for large package-lock.json/composer.lock
// files (issue #4873 already rejected an O(n)-per-lookup rescan for the
// unrelated key-order scan, and the same rule applies here).
type newlineIndex struct {
	// offsets holds the byte offset of every '\n' in the source buffer, in
	// ascending order.
	offsets []int64

	// translate, when non-nil, maps a caller-supplied offset (into whatever
	// buffer the caller is walking, e.g. a normalized JSON buffer) to the
	// corresponding offset in the buffer offsets was built from, before
	// lineAt runs its binary search. This lets an index built over the real
	// on-disk source still answer offset->line queries made in terms of a
	// derived buffer's offsets (issue #5358: normalizeJSONSource can remove
	// or shift '\n' bytes -- leading blank lines, a `{{ }}` template banner,
	// or JSONC block comments -- so an index built from the normalized buffer
	// itself would mis-map every line after a stripped region). nil means the
	// caller's offsets already are offsets, unchanged.
	translate offsetTranslator
}

// offsetTranslator maps a byte offset in a derived buffer back to the
// corresponding byte offset in the reference buffer a newlineIndex was built
// over.
type offsetTranslator func(offset int64) int64

// buildNewlineIndex scans data once and records every newline byte offset.
// CRLF line endings are handled by counting only the '\n' byte; a lone '\r'
// (old Mac-style endings) is not treated as a line break, matching every
// other line-counting path in this codebase.
func buildNewlineIndex(data []byte) *newlineIndex {
	offsets := make([]int64, 0)
	for i, b := range data {
		if b == '\n' {
			offsets = append(offsets, int64(i))
		}
	}
	return &newlineIndex{offsets: offsets}
}

// buildTranslatedNewlineIndex builds a newlineIndex over source (the
// reference buffer real line numbers are counted against) whose lineAt
// translates every queried offset through translate first. Use this instead
// of buildNewlineIndex(derivedBuffer) whenever offsets being queried are
// positions in a buffer normalizeJSONSource derived from source: the derived
// buffer's own '\n' bytes are not a faithful stand-in for source's lines
// (issue #5358).
func buildTranslatedNewlineIndex(source []byte, translate offsetTranslator) *newlineIndex {
	idx := buildNewlineIndex(source)
	idx.translate = translate
	return idx
}

// lineAt returns the 1-based line number containing byte offset in the
// buffer buildNewlineIndex was built from (or, when translate is set, the
// derived buffer translate maps offsets from). offset is clamped implicitly:
// an offset at or beyond the end of the buffer resolves to the last line.
func (idx *newlineIndex) lineAt(offset int64) int {
	if idx == nil {
		return 0
	}
	if idx.translate != nil {
		offset = idx.translate(offset)
	}
	// Count newline offsets strictly less than offset; that count is the
	// number of completed lines before offset, so +1 gives the 1-based line
	// containing offset.
	count := sort.Search(len(idx.offsets), func(i int) bool {
		return idx.offsets[i] >= offset
	})
	return count + 1
}
