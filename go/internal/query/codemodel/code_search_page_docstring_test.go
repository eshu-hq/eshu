// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestCodeSearchPagePayloadClipsDocstringAfterTrim proves find_code rows share
// the read-time docstring bound (#7234): only rows inside the returned page are
// clipped and counted, the row reports the clip, the response reports the
// ceiling and the count, and a docstring that fits is left alone.
func TestCodeSearchPagePayloadClipsDocstringAfterTrim(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("d", 4000)
	rows := []map[string]any{
		{
			"entity_id": "e1", "name": "getTab", "labels": []string{"Function"}, "language": "javascript",
			"metadata": map[string]any{"docstring": long, "method_kind": "getter"},
			// A find_code row is already enriched when it reaches the page: the
			// derived fields echo the stored docstring until the clip rebuilds them.
			"semantic_summary": "Function getTab is documented as " + long + ".",
		},
		{"entity_id": "e2", "metadata": map[string]any{"docstring": "short"}},
		{"entity_id": "e3-trimmed", "metadata": map[string]any{"docstring": long}},
	}

	payload := CodeSearchPagePayload("content", "postgres", "q", "repo", rows, 2)

	if got := payload[querycontract.DocstringClipBytesKey]; got != querycontract.DocstringClipBytes {
		t.Fatalf("docstring_clip_bytes = %v, want %d", got, querycontract.DocstringClipBytes)
	}
	if got := payload[querycontract.DocstringClippedRowsKey]; got != 1 {
		t.Fatalf("docstring_clipped_rows = %v, want 1 (the trimmed row must not count)", got)
	}
	page := payload["results"].([]map[string]any)
	if len(page) != 2 {
		t.Fatalf("page = %d rows, want 2", len(page))
	}
	if page[0][querycontract.DocstringClippedKey] != true || page[0][querycontract.DocstringTotalBytesKey] != 4000 {
		t.Fatalf("long row markers = %v, want clipped with total 4000", page[0])
	}
	if got := len(page[0]["metadata"].(map[string]any)["docstring"].(string)); got != querycontract.DocstringClipBytes {
		t.Fatalf("long row docstring = %d bytes, want %d", got, querycontract.DocstringClipBytes)
	}
	summary, _ := page[0]["semantic_summary"].(string)
	if strings.Contains(summary, strings.Repeat("d", querycontract.DocstringClipBytes+1)) {
		t.Fatalf("semantic_summary still echoes more than %d docstring bytes: %d bytes", querycontract.DocstringClipBytes, len(summary))
	}
	if _, present := page[1][querycontract.DocstringClippedKey]; present {
		t.Fatalf("short row carries docstring_clipped, want markers only on clipped rows")
	}
}
