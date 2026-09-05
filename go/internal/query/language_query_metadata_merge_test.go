// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

// TestEnrichLanguageResultsWithContentMetadataReportsMergedEvenWhenGraphShadowsAllContentValues
// pins the #5761 P3-1 review-fix doc correction on
// enrichLanguageResultsWithContentMetadata (language_query_metadata.go):
// merged reports true whenever a matched row's content metadata was
// non-empty, not "iff it actually wrote a content value that survives into
// the final row." Here the content row's only key ("metaclass") is also
// present as a non-nil value in the graph row's existing metadata, so
// mergeGraphFirstMetadata's existing-value overlay makes the graph value win
// and the final metadata is byte-identical to the graph-only answer --
// content contributed nothing visible. merged is still true, because the
// boolean tracks "a matched, non-empty content payload was found and
// merged," not "the visible answer changed." The direction stays safe (a
// hybrid/derived truth basis when a plain graph read would have been equally
// accurate), never the reverse. Split into its own file rather than added to
// language_query_metadata_test.go, which the addition would have pushed past
// the repo's 500-line file cap.
func TestEnrichLanguageResultsWithContentMetadataReportsMergedEvenWhenGraphShadowsAllContentValues(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/models.py", "Class", "Logged",
					int64(4), int64(8), "python", "class Logged(metaclass=FallbackMeta): pass", []byte(`{"metaclass":"FallbackMeta"}`),
				},
			},
		},
	})

	handler := &LanguageQueryHandler{Content: NewContentReader(db)}
	graphResults := []map[string]any{
		{
			"entity_id":  "graph-1",
			"name":       "Logged",
			"labels":     []string{"Class"},
			"file_path":  "src/models.py",
			"repo_id":    "repo-1",
			"language":   "python",
			"start_line": 4,
			"end_line":   8,
			"metadata": map[string]any{
				"metaclass": "MetaLogger",
			},
		},
	}

	got, merged, err := handler.enrichLanguageResultsWithContentMetadata(
		context.Background(),
		graphResults,
		"python",
		"Class",
		"Logged",
		"repo-1",
		10,
		unscopedLanguageQueryGrant(),
	)
	if err != nil {
		t.Fatalf("enrichLanguageResultsWithContentMetadata() error = %v, want nil", err)
	}
	if !merged {
		t.Fatalf("merged = %v, want true (a matched, non-empty content payload was found, even though the graph value shadows it)", merged)
	}

	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", got[0]["metadata"])
	}
	if gotValue, want := metadata["metaclass"], "MetaLogger"; gotValue != want {
		t.Fatalf("metadata[metaclass] = %#v, want %#v (the graph value must win over the shadowed content value)", gotValue, want)
	}
}
