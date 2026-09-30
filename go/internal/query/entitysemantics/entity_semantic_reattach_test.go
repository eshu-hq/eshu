// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entitysemantics

import (
	"fmt"
	"strings"
	"testing"
)

func docstringRow(doc string) map[string]any {
	return map[string]any{
		"name": "getTab", "entity_name": "getTab", "entity_type": "Function", "language": "javascript",
		"metadata": map[string]any{"docstring": doc, "method_kind": "getter"},
	}
}

// TestReattachSemanticSummaryEchoesTheChangedMetadata pins the #7234 contract:
// after the docstring is clipped, the derived fields carry the clipped value,
// and a derived field the new metadata no longer produces is dropped.
func TestReattachSemanticSummaryEchoesTheChangedMetadata(t *testing.T) {
	t.Parallel()

	full := strings.Repeat("d", 2000)
	row := docstringRow(full)
	AttachSemanticSummary(row)
	if !strings.Contains(row["semantic_summary"].(string), full) {
		t.Fatal("precondition: the summary must echo the full docstring before the clip")
	}

	row["metadata"] = map[string]any{"docstring": full[:100], "method_kind": "getter"}
	row["story"] = "stale"
	ReattachSemanticSummary(row)

	for _, key := range []string{"semantic_summary", "semantic_profile", "javascript_semantics", "story"} {
		value, ok := row[key]
		if !ok {
			t.Fatalf("%s missing after reattach", key)
		}
		if strings.Contains(anyString(value), full[:101]) {
			t.Fatalf("%s still carries more than the clipped docstring: %v", key, value)
		}
	}
	if row["story"] == "stale" {
		t.Fatal("story kept its stale value; reattach must rebuild it")
	}

	// With no metadata there is nothing to derive from: the row is left as it
	// was rather than stripped of fields another step set.
	before := row["semantic_summary"]
	row["metadata"] = map[string]any{}
	ReattachSemanticSummary(row)
	if row["semantic_summary"] != before {
		t.Fatalf("semantic_summary = %v after reattach over empty metadata, want it untouched (%v)", row["semantic_summary"], before)
	}
	ReattachSemanticSummary(nil)
}

func anyString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return strings.TrimSpace(strings.ReplaceAll(fmtAny(value), "\n", " "))
}

// BenchmarkReattachSemanticSummary measures the re-derive step a clipped row
// pays once, on a JavaScript row with a clipped docstring.
func BenchmarkReattachSemanticSummary(b *testing.B) {
	row := docstringRow(strings.Repeat("d", 512))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ReattachSemanticSummary(row)
	}
}

func fmtAny(value any) string { return fmt.Sprint(value) }
