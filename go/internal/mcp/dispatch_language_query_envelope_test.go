// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestDispatchToolExecuteLanguageQueryReturnsEnvelopeShapedResult is the #5761
// P2-2 review-fix regression. dispatch.go's dispatchToolWithOptions sets
// "Accept: application/eshu.envelope+json" unconditionally on every internal
// request it builds, so once handleLanguageQuery started calling WriteSuccess
// with a real truth envelope (this diff's own F1 fix), execute_language_query
// silently started taking the parseCanonicalEnvelope branch in
// dispatchToolWithOptions -- which changes what server.go's "tools/call"
// handler sends back over MCP: summarizeToolText + the
// "eshu://tool-result/envelope" resource + StructuredContent, instead of the
// old summarizePlainToolText + "eshu://tool-result/payload" shape. Nothing
// pinned this shape change for this tool. This proves dispatchTool returns a
// canonical envelope (Envelope != nil, Envelope.Truth != nil) carrying the
// route's data fields, mirroring
// dispatch_context_envelope_test.go's TestDispatchToolWorkloadContextReturnsHardenedEnvelope
// pattern for a different tool.
func TestDispatchToolExecuteLanguageQueryReturnsEnvelopeShapedResult(t *testing.T) {
	t.Parallel()

	handler := &query.LanguageQueryHandler{
		Neo4j:   languageQueryEnvelopeGraphReader{},
		Profile: query.ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"execute_language_query",
		map[string]any{"language": "go", "entity_type": "function", "query": "Foo"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool(execute_language_query) error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatalf("dispatchTool(execute_language_query) envelope is nil, want canonical envelope")
	}
	if result.Envelope.Truth == nil {
		t.Fatalf("dispatchTool(execute_language_query) envelope truth is nil, want truth envelope")
	}
	if got, want := result.Envelope.Truth.Basis, query.TruthBasisAuthoritativeGraph; got != want {
		t.Fatalf("envelope.Truth.Basis = %q, want %q", got, want)
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("dispatchTool(execute_language_query) data type = %T, want map[string]any", result.Envelope.Data)
	}
	if got, want := query.StringVal(data, "source_backend"), "graph"; got != want {
		t.Fatalf("data.source_backend = %q, want %q", got, want)
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("data.results = %#v, want one graph-backed function", data["results"])
	}
}

type languageQueryEnvelopeGraphReader struct{}

func (languageQueryEnvelopeGraphReader) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return []map[string]any{
		{
			"entity_id": "graph-1",
			"name":      "Foo",
			"labels":    []string{"Function"},
			"file_path": "src/foo.go",
			"repo_id":   "repo-1",
			"language":  "go",
		},
	}, nil
}

func (languageQueryEnvelopeGraphReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}
