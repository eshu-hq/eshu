// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"strings"
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
//
// Driven through the mounted route (#6642) rather than the unexported
// enrichLanguageResultsWithContentMetadata method directly: merged is exactly
// what decides whether queryByLanguageWithSemanticFilter reports
// TruthBasisHybrid instead of TruthBasisAuthoritativeGraph
// (language_queries.go), so the wire truth envelope's basis is the same
// observable proof the direct-call assertion was.
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

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-1",
				"name":       "Logged",
				"labels":     []string{"Class"},
				"file_path":  "src/models.py",
				"repo_id":    "repo-1",
				"language":   "python",
				"start_line": int64(4),
				"end_line":   int64(8),
				"metaclass":  "MetaLogger",
			},
		}},
		Content: NewContentReader(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"python","entity_type":"class","query":"Logged","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	envelope := decodeLanguageQueryEnvelope(t, rec)
	if envelope.Truth == nil {
		t.Fatalf("truth envelope missing, body = %s", rec.Body.String())
	}
	if envelope.Truth.Basis != TruthBasisHybrid {
		t.Fatalf("truth.basis = %q, want %q (a matched, non-empty content payload was found, even though the graph value shadows it)", envelope.Truth.Basis, TruthBasisHybrid)
	}

	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any (body = %s)", envelope.Data, rec.Body.String())
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("data.results = %#v, want exactly one result", data["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata = %#v, want map[string]any", result["metadata"])
	}
	if got, want := metadata["metaclass"], "MetaLogger"; got != want {
		t.Fatalf("metadata[metaclass] = %#v, want %q (the graph value must win over the shadowed content value)", got, want)
	}
}
