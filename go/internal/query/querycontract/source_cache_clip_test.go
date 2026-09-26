// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundedSourceCacheLeavesFittingBodyUntouched(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"", "func f() {}", strings.Repeat("a", 4096)} {
		got, markers := BoundedSourceCache(body, 4096)
		if got != body {
			t.Fatalf("BoundedSourceCache(len=%d) changed a body that fits", len(body))
		}
		if markers != nil {
			t.Fatalf("BoundedSourceCache(len=%d) markers = %v, want nil when nothing was clipped", len(body), markers)
		}
	}
}

func TestBoundedSourceCacheClipsAndReportsStoredLength(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("a", 61625)
	got, markers := BoundedSourceCache(body, 4096)
	if len(got) != 4096 {
		t.Fatalf("clipped length = %d, want 4096", len(got))
	}
	if markers[SourceCacheClippedKey] != true || markers[SourceCacheClipBytesKey] != 4096 || markers[SourceCacheTotalBytesKey] != 61625 {
		t.Fatalf("markers = %v, want clipped=true clip_bytes=4096 total_bytes=61625", markers)
	}
}

func TestBoundedSourceCacheNeverSplitsACodePoint(t *testing.T) {
	t.Parallel()

	// Two-, three-, and four-byte runes at every alignment against the limit.
	for _, glyph := range []string{"é", "世", "😀"} {
		for shift := 0; shift < 4; shift++ {
			body := strings.Repeat("x", shift) + strings.Repeat(glyph, 3000)
			got, markers := BoundedSourceCache(body, 4096)
			if !utf8.ValidString(got) {
				t.Fatalf("glyph %q shift %d: clipped body is not valid UTF-8", glyph, shift)
			}
			if len(got) > 4096 {
				t.Fatalf("glyph %q shift %d: clipped length %d exceeds 4096", glyph, shift, len(got))
			}
			if len(got) < 4096-(utf8.RuneLen([]rune(glyph)[0])-1) {
				t.Fatalf("glyph %q shift %d: clipped length %d stepped back further than one code point", glyph, shift, len(got))
			}
			if markers[SourceCacheTotalBytesKey] != len(body) {
				t.Fatalf("glyph %q shift %d: total bytes %v, want %d", glyph, shift, markers[SourceCacheTotalBytesKey], len(body))
			}
		}
	}
}

func TestClipRowSourceCacheOnlyMarksClippedRows(t *testing.T) {
	t.Parallel()

	small := map[string]any{"source_cache": "small"}
	if ClipRowSourceCache(small) {
		t.Fatal("ClipRowSourceCache(small) = true, want false")
	}
	if len(small) != 1 {
		t.Fatalf("small row gained keys: %v", small)
	}
	absent := map[string]any{"name": "no body"}
	if ClipRowSourceCache(absent) || len(absent) != 1 {
		t.Fatalf("row without source_cache changed: %v", absent)
	}
	big := map[string]any{"source_cache": strings.Repeat("a", 5000)}
	if !ClipRowSourceCache(big) {
		t.Fatal("ClipRowSourceCache(big) = false, want true")
	}
	if len(big["source_cache"].(string)) != 4096 || big[SourceCacheClippedKey] != true {
		t.Fatalf("big row = %v, want a 4096-byte body and clipped=true", big)
	}
}

func TestClipRowsSourceCacheCountsClippedRowsAndMarkersAreAlwaysPresent(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"source_cache": strings.Repeat("a", 5000)},
		{"source_cache": "fits"},
		{"source_cache": strings.Repeat("b", 9000)},
	}
	clipped := ClipRowsSourceCache(rows)
	if clipped != 2 {
		t.Fatalf("ClipRowsSourceCache() = %d, want 2", clipped)
	}
	data := map[string]any{}
	AddSourceCacheClipMarkers(data, clipped)
	if data[SourceCacheClipBytesKey] != 4096 || data[SourceCacheClippedRowsKey] != 2 {
		t.Fatalf("response markers = %v, want clip_bytes=4096 clipped_rows=2", data)
	}
	none := map[string]any{}
	AddSourceCacheClipMarkers(none, 0)
	if none[SourceCacheClipBytesKey] != 4096 || none[SourceCacheClippedRowsKey] != 0 {
		t.Fatalf("no-clip response markers = %v, want clip_bytes=4096 clipped_rows=0 always present", none)
	}
}

func TestEntityContentSearchRowCarriesFieldsAndSourceHandle(t *testing.T) {
	t.Parallel()

	row := EntityContentSearchRow(EntityContent{
		EntityID: "e1", RepoID: "r1", RelativePath: "a/b.go", EntityType: "Function",
		EntityName: "F", StartLine: 4, EndLine: 9, SourceCache: "body",
	})
	handle, ok := row["source_handle"].(map[string]any)
	if !ok || handle["repo_id"] != "r1" || handle["file_path"] != "a/b.go" || handle["start_line"] != 4 || handle["end_line"] != 9 {
		t.Fatalf("source_handle = %#v, want {r1, a/b.go, 4, 9}", row["source_handle"])
	}
	if row["entity_id"] != "e1" || row["source_cache"] != "body" {
		t.Fatalf("row = %v, want entity_id and source_cache carried", row)
	}
	for _, key := range []string{"repo_name", "language", "metadata", "search_backend"} {
		if _, present := row[key]; present {
			t.Fatalf("row carries empty optional %s, want it omitted like the struct's omitempty tags", key)
		}
	}
}

// BenchmarkClipRowsSourceCache measures the per-page cost of the clip on the
// worst realistic page: 20 rows whose stored body is the 61,625 B production
// maximum. The cut scans at most SourceCacheClipBytes bytes per row regardless
// of the stored length.
func BenchmarkClipRowsSourceCache(b *testing.B) {
	body := strings.Repeat("é", 30812)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rows := make([]map[string]any, 20)
		for j := range rows {
			rows[j] = map[string]any{"source_cache": body}
		}
		if got := ClipRowsSourceCache(rows); got != 20 {
			b.Fatalf("clipped = %d, want 20", got)
		}
	}
}
