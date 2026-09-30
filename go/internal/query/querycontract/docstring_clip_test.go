// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"strings"
	"testing"
)

func TestClipRowsDocstringClipsMetadataCopyAndMarksRow(t *testing.T) {
	shared := map[string]any{"docstring": strings.Repeat("x", 2000), "async": true}
	row := map[string]any{"entity_id": "e1", "metadata": shared}

	if got := ClipRowsDocstring([]map[string]any{row}, nil); got != 1 {
		t.Fatalf("ClipRowsDocstring() = %d, want 1", got)
	}
	metadata := row["metadata"].(map[string]any)
	if got := len(metadata["docstring"].(string)); got != DocstringClipBytes {
		t.Fatalf("clipped docstring = %d bytes, want %d", got, DocstringClipBytes)
	}
	if metadata["async"] != true {
		t.Fatalf("clip dropped a sibling metadata key: %v", metadata)
	}
	if row[DocstringClippedKey] != true || row[DocstringClipBytesKey] != DocstringClipBytes || row[DocstringTotalBytesKey] != 2000 {
		t.Fatalf("row markers = %v, want clipped=true clip_bytes=%d total_bytes=2000", row, DocstringClipBytes)
	}
	// The store may hand every caller the same metadata map; the clip must
	// replace the row's map rather than mutate the shared one.
	if got := len(shared["docstring"].(string)); got != 2000 {
		t.Fatalf("clip mutated the shared metadata map: docstring = %d bytes, want 2000", got)
	}
}

func TestClipRowsDocstringLeavesFittingAndAbsentDocstringsUntouched(t *testing.T) {
	fits := map[string]any{"metadata": map[string]any{"docstring": strings.Repeat("y", DocstringClipBytes)}}
	none := map[string]any{"metadata": map[string]any{"async": true}}
	bare := map[string]any{"entity_id": "e3"}
	notString := map[string]any{"metadata": map[string]any{"docstring": 7}}

	if got := ClipRowsDocstring([]map[string]any{fits, none, bare, notString}, nil); got != 0 {
		t.Fatalf("ClipRowsDocstring() = %d, want 0", got)
	}
	for name, row := range map[string]map[string]any{"fits": fits, "none": none, "bare": bare, "notString": notString} {
		if _, ok := row[DocstringClippedKey]; ok {
			t.Fatalf("%s row carries clip markers %v, want none on an unclipped row", name, row)
		}
	}
}

func TestClipRowsDocstringNeverSplitsACodePoint(t *testing.T) {
	// "é" is two bytes; 511 ASCII bytes then é straddles the 512-byte ceiling.
	row := map[string]any{"metadata": map[string]any{"docstring": strings.Repeat("a", DocstringClipBytes-1) + "éé"}}
	ClipRowsDocstring([]map[string]any{row}, nil)
	got := row["metadata"].(map[string]any)["docstring"].(string)
	if len(got) > DocstringClipBytes || strings.ContainsRune(got, '�') {
		t.Fatalf("clipped docstring = %d bytes %q, want <= %d and valid UTF-8", len(got), got, DocstringClipBytes)
	}
}

func TestClipRowsDocstringClipsTopLevelGraphDocstring(t *testing.T) {
	row := map[string]any{"docstring": strings.Repeat("g", 900)}
	if got := ClipRowsDocstring([]map[string]any{row}, nil); got != 1 {
		t.Fatalf("ClipRowsDocstring() = %d, want 1 for a graph row's top-level docstring", got)
	}
	if got := len(row["docstring"].(string)); got != DocstringClipBytes || row[DocstringTotalBytesKey] != 900 {
		t.Fatalf("top-level docstring = %d bytes, total marker = %v, want %d and 900", got, row[DocstringTotalBytesKey], DocstringClipBytes)
	}
}

func TestClipRowsDocstringRederivesOnlyClippedRows(t *testing.T) {
	long := map[string]any{"entity_id": "long", "metadata": map[string]any{"docstring": strings.Repeat("z", 1500)}}
	short := map[string]any{"entity_id": "short", "metadata": map[string]any{"docstring": "ok"}}

	var seen []string
	ClipRowsDocstring([]map[string]any{long, short}, func(row map[string]any) {
		seen = append(seen, row["entity_id"].(string))
		if got := len(row["metadata"].(map[string]any)["docstring"].(string)); got != DocstringClipBytes {
			t.Fatalf("rederive saw an unclipped docstring (%d bytes); it must run after the clip", got)
		}
	})
	if len(seen) != 1 || seen[0] != "long" {
		t.Fatalf("rederive ran for %v, want only the clipped row", seen)
	}
}

func TestAddDocstringClipMarkersAlwaysPresent(t *testing.T) {
	data := map[string]any{}
	AddDocstringClipMarkers(data, 0)
	if data[DocstringClipBytesKey] != DocstringClipBytes || data[DocstringClippedRowsKey] != 0 {
		t.Fatalf("response markers = %v, want clip_bytes=%d clipped_rows=0", data, DocstringClipBytes)
	}
}

// BenchmarkClipRowsDocstring measures the per-page cost of the docstring clip
// on the default 20-row page where every row carries a 16 KiB docstring: the
// worst case, since every row copies its metadata map and cuts the string.
func BenchmarkClipRowsDocstring(b *testing.B) {
	doc := strings.Repeat("d", 16*1024)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		rows := make([]map[string]any, 20)
		for r := range rows {
			rows[r] = map[string]any{"entity_id": "e", "metadata": map[string]any{"docstring": doc, "async": true}}
		}
		b.StartTimer()
		if got := ClipRowsDocstring(rows, nil); got != 20 {
			b.Fatalf("clipped = %d, want 20", got)
		}
	}
}
