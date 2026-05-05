package shape

import (
	"strings"
	"unicode/utf8"
)

const (
	sourceCacheTruncatedMetadataKey     = "source_cache_truncated"
	sourceCacheOriginalBytesMetadataKey = "source_cache_original_bytes"
	sourceCacheLimitBytesMetadataKey    = "source_cache_limit_bytes"
)

var entitySourceCacheByteLimits = map[string]int{
	// Variable declarations often include large generated assignments. Full-file
	// content remains indexed for exact search; entity source_cache is a snippet.
	"Variable": 4096,
}

// entitySourceCache returns the best source snippet for one entity using parser
// source first, then falling back to the owning file line range.
func entitySourceCache(label string, item Entity, body string, startLine int, endLine int) string {
	if isCodeSourceLabel(label) && strings.TrimSpace(item.Source) != "" {
		return withTrailingNewline(item.Source, label)
	}

	lines := splitLines(body)
	if len(lines) > 0 && startLine >= 1 {
		startIndex := startLine - 1
		if startIndex < len(lines) {
			endIndex := endLine
			if endIndex > len(lines) {
				endIndex = len(lines)
			}
			if endIndex > startIndex {
				selected := strings.Join(lines[startIndex:endIndex], "\n")
				return withTrailingNewline(selected, label)
			}
		}
	}

	if strings.TrimSpace(item.Source) != "" {
		return item.Source
	}
	return ""
}

// limitEntitySourceCache bounds oversized low-signal snippets while preserving
// exact full-file search in content_files and recording metadata for clients.
func limitEntitySourceCache(label string, sourceCache string, metadata map[string]any) (string, map[string]any) {
	limit, ok := entitySourceCacheByteLimits[label]
	if !ok || limit <= 0 || len(sourceCache) <= limit {
		return sourceCache, metadata
	}

	if metadata == nil {
		metadata = make(map[string]any, 3)
	}
	metadata[sourceCacheTruncatedMetadataKey] = true
	metadata[sourceCacheOriginalBytesMetadataKey] = len(sourceCache)
	metadata[sourceCacheLimitBytesMetadataKey] = limit

	return truncateUTF8ByBytes(sourceCache, limit), metadata
}

// truncateUTF8ByBytes returns a prefix no longer than limit bytes without
// splitting a UTF-8 code point, so Postgres receives valid text.
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
