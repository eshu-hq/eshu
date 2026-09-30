// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "unicode/utf8"

// SourceCacheClipBytes is the read-time byte ceiling for the source_cache body
// of one row on the symbol, structural-inventory, code-search, and entity-content
// search routes. The stored body is untouched; a clipped row carries the markers
// below and a source_handle whose drill-down (get_entity_content or
// get_file_lines) returns the full text. It is deliberately distinct from the
// write-time metadata.source_cache_truncated family in content/shape, which
// records a lossy cut made before the row was stored.
const SourceCacheClipBytes = 4096

// Row and response marker keys for the read-time source_cache clip.
const (
	// SourceCacheClippedKey is set to true on a row whose body was clipped.
	SourceCacheClippedKey = "source_cache_clipped"
	// SourceCacheClipBytesKey is the clip ceiling; it is set on a clipped row
	// and on every response that shapes source_cache rows.
	SourceCacheClipBytesKey = "source_cache_clip_bytes"
	// SourceCacheTotalBytesKey is the stored body length before the clip.
	SourceCacheTotalBytesKey = "source_cache_total_bytes"
	// SourceCacheClippedRowsKey is the response count of clipped rows.
	SourceCacheClippedRowsKey = "source_cache_clipped_rows"

	sourceCacheRowKey = "source_cache"
)

// BoundedSourceCache returns source unchanged when it fits limitBytes, and
// otherwise a UTF-8-safe prefix no longer than limitBytes plus the sparse row
// markers describing the clip. The marker map is nil when nothing was clipped.
func BoundedSourceCache(source string, limitBytes int) (string, map[string]any) {
	if limitBytes <= 0 || len(source) <= limitBytes {
		return source, nil
	}
	return truncateUTF8ByBytes(source, limitBytes), map[string]any{
		SourceCacheClippedKey:    true,
		SourceCacheClipBytesKey:  limitBytes,
		SourceCacheTotalBytesKey: len(source),
	}
}

// ClipRowSourceCache clips row["source_cache"] to SourceCacheClipBytes in place
// and adds the row markers when it clipped. It reports whether the row was
// clipped. Rows without a string source_cache are left untouched.
func ClipRowSourceCache(row map[string]any) bool {
	source, ok := row[sourceCacheRowKey].(string)
	if !ok {
		return false
	}
	clipped, markers := BoundedSourceCache(source, SourceCacheClipBytes)
	if markers == nil {
		return false
	}
	row[sourceCacheRowKey] = clipped
	for key, value := range markers {
		row[key] = value
	}
	return true
}

// ClipRowsSourceCache clips every row in place and returns how many were
// clipped. Call it after the page has been trimmed to its public limit so the
// count describes the rows actually returned.
func ClipRowsSourceCache(rows []map[string]any) int {
	clipped := 0
	for _, row := range rows {
		if ClipRowSourceCache(row) {
			clipped++
		}
	}
	return clipped
}

// AddSourceCacheClipMarkers writes the response-level clip markers beside the
// existing count/limit/truncated fields: the ceiling always, and the number of
// clipped rows (0 when none).
func AddSourceCacheClipMarkers(data map[string]any, clippedRows int) {
	data[SourceCacheClipBytesKey] = SourceCacheClipBytes
	data[SourceCacheClippedRowsKey] = clippedRows
}

// truncateUTF8ByBytes returns a prefix no longer than limit bytes without
// splitting a UTF-8 code point. It mirrors content/shape's write-time helper;
// query code does not import collector-side packages.
func truncateUTF8ByBytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	cut := 0
	for index := range value {
		if index > limit {
			break
		}
		cut = index
	}
	if cut == 0 {
		_, size := utf8.DecodeRuneInString(value)
		if size <= limit {
			cut = size
		}
	}
	return value[:cut]
}

// DocstringClipBytes is the read-time byte ceiling for one row's docstring on
// the routes that clip source_cache (#7234). The row shape echoes a docstring
// into metadata, semantic_summary, semantic_profile, the per-language
// semantics blocks, and story, so an unbounded docstring is repeated five or
// six times per row. Clipping the source value bounds every echo derived from
// it. The stored docstring is untouched; get_entity_content returns it whole.
const DocstringClipBytes = 512

// Row and response marker keys for the read-time docstring clip. They mirror
// the source_cache markers and are additive: a row that fits carries none.
const (
	// DocstringClippedKey is set to true on a row whose docstring was clipped.
	DocstringClippedKey = "docstring_clipped"
	// DocstringClipBytesKey is the clip ceiling; it is set on a clipped row and
	// on every response that shapes docstring-bearing rows.
	DocstringClipBytesKey = "docstring_clip_bytes"
	// DocstringTotalBytesKey is the stored docstring length before the clip.
	DocstringTotalBytesKey = "docstring_total_bytes"
	// DocstringClippedRowsKey is the response count of clipped rows.
	DocstringClippedRowsKey = "docstring_clipped_rows"

	docstringKey = "docstring"
	metadataKey  = "metadata"
)

// ClipRowsDocstring clips each row's docstring to DocstringClipBytes in place
// and returns how many rows it clipped. It reads row["metadata"]["docstring"],
// the shape every row on these routes carries: content-store rows have it
// directly and graph rows get it through GraphResultMetadata. A clipped row's
// metadata is replaced by a clipped copy, because the store may hand every
// caller the same map, and the row gains the markers above.
//
// rederive, when non-nil, runs once on each clipped row after the clip so the
// fields derived from the docstring (semantic_summary, semantic_profile, story
// and the language blocks) are rebuilt from the clipped value instead of
// carrying the full one. It is not called for a row that already fit. Call the
// function after the page is trimmed to its public limit so the count matches
// the rows returned.
func ClipRowsDocstring(rows []map[string]any, rederive func(map[string]any)) int {
	clipped := 0
	for _, row := range rows {
		if clipRowDocstring(row) {
			clipped++
			if rederive != nil {
				rederive(row)
			}
		}
	}
	return clipped
}

// clipRowDocstring clips one row and reports whether it changed the row.
func clipRowDocstring(row map[string]any) bool {
	metadata, ok := row[metadataKey].(map[string]any)
	if !ok {
		return false
	}
	doc, ok := metadata[docstringKey].(string)
	if !ok || len(doc) <= DocstringClipBytes {
		return false
	}
	copied := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}
	copied[docstringKey] = truncateUTF8ByBytes(doc, DocstringClipBytes)
	row[metadataKey] = copied
	row[DocstringClippedKey] = true
	row[DocstringClipBytesKey] = DocstringClipBytes
	row[DocstringTotalBytesKey] = len(doc)
	return true
}

// AddDocstringClipMarkers writes the response-level docstring clip markers
// beside the source_cache ones: the ceiling always, and the number of clipped
// rows (0 when none).
func AddDocstringClipMarkers(data map[string]any, clippedRows int) {
	data[DocstringClipBytesKey] = DocstringClipBytes
	data[DocstringClippedRowsKey] = clippedRows
}
