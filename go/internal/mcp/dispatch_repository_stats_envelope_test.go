// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestDispatchToolRepositoryStatsSingletonReturnsHardenedEnvelope proves the
// singleton get_repository_stats tool exposes the canonical truth envelope plus
// the additive result_limits drilldown block and explicit partial_reasons slot
// through MCP dispatch, matching the HTTP surface, with a content-index truth
// basis for content-backed counts.
func TestDispatchToolRepositoryStatsSingletonReturnsHardenedEnvelope(t *testing.T) {
	t.Parallel()

	handler := &query.RepositoryHandler{
		Neo4j: repositoryStatsEnvelopeGraphReader{repoID: "repo-stats"},
		Content: repositoryStatsEnvelopeContentStore{
			coverage: query.RepositoryContentCoverage{
				Available:   true,
				FileCount:   42,
				EntityCount: 7,
				Languages: []query.RepositoryLanguageCount{
					{Language: "go", FileCount: 30},
				},
				EntityTypes: []query.RepositoryEntityTypeCount{
					{EntityType: "Function", Count: 5},
				},
			},
		},
		Profile: query.ProfileProduction,
	}

	envelope := dispatchRepositoryStatsEnvelope(t, handler, "get_repository_stats", map[string]any{"repo_id": "repo-stats"})
	if got, want := envelope.Truth.Basis, query.TruthBasisContentIndex; got != want {
		t.Fatalf("truth.basis = %q, want %q", got, want)
	}
	data := repositoryStatsEnvelopeData(t, envelope)
	requireRepositoryStatsLimits(t, data, "get_repository_coverage", "/api/v0/repositories/repo-stats/stats")
}

// TestDispatchToolRepositoryStatsInventoryReturnsHardenedEnvelope proves the
// inventory (empty-selector) form of get_repository_stats exposes the same
// hardened result_limits/partial_reasons shape through MCP dispatch.
func TestDispatchToolRepositoryStatsInventoryReturnsHardenedEnvelope(t *testing.T) {
	t.Parallel()

	handler := &query.RepositoryHandler{
		Neo4j:   repositoryStatsEnvelopeGraphReader{repoID: "repo-stats"},
		Profile: query.ProfileProduction,
	}

	envelope := dispatchRepositoryStatsEnvelope(t, handler, "get_repository_stats", map[string]any{})
	if envelope.Truth == nil {
		t.Fatal("inventory envelope truth is nil, want truth envelope")
	}
	data := repositoryStatsEnvelopeData(t, envelope)
	requireRepositoryStatsLimits(t, data, "get_repository_stats", "/api/v0/repositories")
}

func dispatchRepositoryStatsEnvelope(
	t *testing.T,
	handler *query.RepositoryHandler,
	tool string,
	args map[string]any,
) *query.ResponseEnvelope {
	t.Helper()

	mux := http.NewServeMux()
	handler.Mount(mux)
	result, err := dispatchTool(
		context.Background(),
		mux,
		tool,
		args,
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool(%q) error = %v, want nil", tool, err)
	}
	if result.Envelope == nil {
		t.Fatalf("dispatchTool(%q) envelope is nil, want canonical envelope", tool)
	}
	if result.Envelope.Truth == nil {
		t.Fatalf("dispatchTool(%q) envelope truth is nil, want truth envelope", tool)
	}
	return result.Envelope
}

func repositoryStatsEnvelopeData(t *testing.T, envelope *query.ResponseEnvelope) map[string]any {
	t.Helper()

	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", envelope.Data)
	}
	return data
}

func requireRepositoryStatsLimits(t *testing.T, data map[string]any, wantTool, wantPath string) {
	t.Helper()

	limits := mcpMapValue(data, "result_limits")
	if len(limits) == 0 {
		t.Fatalf("data.result_limits missing, want bounded drilldown block; data keys = %v", mapKeys(data))
	}
	if limits["limit"] == nil {
		t.Fatal("result_limits.limit missing, want bounded limit")
	}
	if got, want := query.StringVal(limits, "ordering"), "deterministic"; got != want {
		t.Fatalf("result_limits.ordering = %q, want %q", got, want)
	}
	if _, ok := limits["truncated"].(bool); !ok {
		t.Fatalf("result_limits.truncated type = %T, want bool", limits["truncated"])
	}
	if got, want := query.StringVal(limits, "drilldown_tool"), wantTool; got != want {
		t.Fatalf("result_limits.drilldown_tool = %q, want %q", got, want)
	}
	if got, want := query.StringVal(limits, "context_path"), wantPath; got != want {
		t.Fatalf("result_limits.context_path = %q, want %q", got, want)
	}
	if _, ok := data["partial_reasons"]; !ok {
		t.Fatal("data.partial_reasons missing, want explicit partial reason slot")
	}
}

// repositoryStatsEnvelopeGraphReader returns a single repository identity row
// for the canonical Repository{id} lookup and an empty page for the inventory
// list query so the bounded payloads stay minimal.
type repositoryStatsEnvelopeGraphReader struct {
	repoID string
}

func (r repositoryStatsEnvelopeGraphReader) RunSingle(
	_ context.Context,
	cypher string,
	_ map[string]any,
) (map[string]any, error) {
	if strings.Contains(cypher, "count(r) AS total") {
		return map[string]any{"total": int64(0)}, nil
	}
	return map[string]any{
		"id":         r.repoID,
		"name":       "stats-service",
		"path":       "/repos/stats-service",
		"local_path": "/repos/stats-service",
		"has_remote": false,
	}, nil
}

func (repositoryStatsEnvelopeGraphReader) Run(
	_ context.Context,
	_ string,
	_ map[string]any,
) ([]map[string]any, error) {
	return nil, nil
}

// repositoryStatsEnvelopeContentStore serves a single repository's content
// coverage so the singleton stats payload reports content-index counts.
type repositoryStatsEnvelopeContentStore struct {
	query.ContentStore
	coverage query.RepositoryContentCoverage
}

func (s repositoryStatsEnvelopeContentStore) RepositoryCoverage(
	context.Context,
	string,
) (query.RepositoryContentCoverage, error) {
	return s.coverage, nil
}

func (s repositoryStatsEnvelopeContentStore) ResolveRepository(
	context.Context,
	string,
) (*query.RepositoryCatalogEntry, error) {
	return nil, nil
}
