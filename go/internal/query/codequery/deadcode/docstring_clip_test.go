// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func docstringBucketRow(id, doc string) map[string]any {
	row := map[string]any{
		"entity_id": id, "name": id, "labels": []string{"Function"}, "language": "javascript",
		"metadata": map[string]any{"docstring": doc, "method_kind": "getter"},
	}
	entitysemantics.AttachSemanticSummary(row)
	return row
}

// TestClipDeadCodeInvestigationDocstringsCoversEveryBucket pins the #7234
// follow-up: the investigation reply carries rows in three buckets, so a clip
// that missed one would still overrun the response budget. Each clipped row
// gets the markers and its derived fields are rebuilt from the clipped value.
func TestClipDeadCodeInvestigationDocstringsCoversEveryBucket(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("d", 4*querycontract.DocstringClipBytes)
	short := "fits the clip"
	scan := DeadCodeInvestigationScan{
		CleanupReady: []map[string]any{docstringBucketRow("ready", long)},
		Ambiguous:    []map[string]any{docstringBucketRow("ambiguous", long)},
		Suppressed:   []map[string]any{docstringBucketRow("suppressed", long), docstringBucketRow("short", short)},
	}

	if got, want := clipDeadCodeInvestigationDocstrings(&scan), 3; got != want {
		t.Fatalf("clipped rows = %d, want %d (the short row must not count)", got, want)
	}
	for name, bucket := range map[string][]map[string]any{
		"cleanup_ready": scan.CleanupReady, "ambiguous": scan.Ambiguous, "suppressed": scan.Suppressed,
	} {
		row := bucket[0]
		if row[querycontract.DocstringClippedKey] != true {
			t.Fatalf("%s row docstring_clipped = %v, want true", name, row[querycontract.DocstringClippedKey])
		}
		if summary, _ := row["semantic_summary"].(string); strings.Contains(summary, strings.Repeat("d", querycontract.DocstringClipBytes+1)) {
			t.Fatalf("%s row semantic_summary still echoes more than the clipped docstring", name)
		}
	}
	shortRow := scan.Suppressed[1]
	if _, marked := shortRow[querycontract.DocstringClippedKey]; marked {
		t.Fatalf("a docstring that fits must carry no clip marker, got %v", shortRow)
	}
}
