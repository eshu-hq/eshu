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
}

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

// lineAt returns the 1-based line number containing byte offset in the
// buffer buildNewlineIndex was built from. offset is clamped implicitly: an
// offset at or beyond the end of the buffer resolves to the last line.
func (idx *newlineIndex) lineAt(offset int64) int {
	if idx == nil {
		return 0
	}
	// Count newline offsets strictly less than offset; that count is the
	// number of completed lines before offset, so +1 gives the 1-based line
	// containing offset.
	count := sort.Search(len(idx.offsets), func(i int) bool {
		return idx.offsets[i] >= offset
	})
	return count + 1
}
