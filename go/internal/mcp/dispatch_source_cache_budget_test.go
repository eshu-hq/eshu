// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/contentread"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// The source_cache budget fixtures reproduce the measured #7171 shape at
// fixture size: rows whose stored source body is larger than any response can
// carry at the default row count. A typical row carries a 12 KiB body and a
// ~1.5 KB of non-source row overhead (the row shape echoes the docstring five times); three rows carry the 61,625 B body measured in
// production. Only a read-time per-row clip keeps the default-args reply under
// the dispatch budget, so these fail on a tree without the clip.
const (
	sourceCacheBudgetRepoID     = "repo-source-cache"
	sourceCacheBudgetTypicalLen = 12 * 1024
	sourceCacheBudgetHugeLen    = 61625
	sourceCacheBudgetHugeRows   = 3
	sourceCacheBudgetRows       = 40
	sourceCacheBudgetDocstring  = 100
)

// sourceCacheBudgetStore serves the same oversized entity rows to symbol
// search, structural inventory, and entity content search.
type sourceCacheBudgetStore struct {
	content.FakePortContentStore
	entities []querycontract.EntityContent
}

// newSourceCacheBudgetStore returns the oversized-row fixture. The fixture
// store's repository catalog is left to the embedded fake, which resolves the
// repo_id selector for the code handler.
func newSourceCacheBudgetStore() *sourceCacheBudgetStore {
	entities := make([]querycontract.EntityContent, 0, sourceCacheBudgetRows)
	for index := 0; index < sourceCacheBudgetRows; index++ {
		size := sourceCacheBudgetTypicalLen
		if index < sourceCacheBudgetHugeRows {
			size = sourceCacheBudgetHugeLen
		}
		entities = append(entities, querycontract.EntityContent{
			EntityID:     fmt.Sprintf("entity-%03d", index),
			RepoID:       sourceCacheBudgetRepoID,
			RelativePath: fmt.Sprintf("src/handler%03d.ts", index),
			EntityType:   "Function",
			EntityName:   "handleRequest",
			StartLine:    10,
			EndLine:      400,
			Language:     "typescript",
			SourceCache:  strings.Repeat("a", size),
			Metadata:     map[string]any{"docstring": strings.Repeat("d", sourceCacheBudgetDocstring)},
		})
	}
	return newSourceCacheStore(entities)
}

func newSourceCacheStore(entities []querycontract.EntityContent) *sourceCacheBudgetStore {
	store := &sourceCacheBudgetStore{entities: entities}
	store.Repositories = []querycontract.RepositoryCatalogEntry{{ID: sourceCacheBudgetRepoID, Name: "source-cache-repo"}}
	return store
}

func (s *sourceCacheBudgetStore) page(limit int) []querycontract.EntityContent {
	if limit > len(s.entities) {
		limit = len(s.entities)
	}
	return s.entities[:limit]
}

func (s *sourceCacheBudgetStore) SearchSymbols(
	_ context.Context,
	req codequery.SymbolSearchRequest,
) ([]querycontract.EntityContent, error) {
	return s.page(req.Limit), nil
}

func (s *sourceCacheBudgetStore) InspectStructuralInventory(
	_ context.Context,
	req codequery.StructuralInventoryRequest,
) ([]querycontract.EntityContent, error) {
	return s.page(req.Limit), nil
}

func (s *sourceCacheBudgetStore) CountStructuralInventoryByFile(
	context.Context,
	codequery.StructuralInventoryRequest,
) ([]codequery.StructuralInventoryFileCount, error) {
	return nil, nil
}

func (s *sourceCacheBudgetStore) SearchEntityContent(
	_ context.Context,
	_, _ string,
	limit int,
) ([]querycontract.EntityContent, error) {
	return s.page(limit), nil
}

func (s *sourceCacheBudgetStore) SearchEntitiesByName(
	_ context.Context,
	_, _, _ string,
	limit int,
) ([]querycontract.EntityContent, error) {
	return s.page(limit), nil
}

func (s *sourceCacheBudgetStore) SearchEntityContentAnyRepo(
	_ context.Context,
	_ string,
	limit int,
) ([]querycontract.EntityContent, error) {
	return s.page(limit), nil
}

func sourceCacheBudgetMux(store *sourceCacheBudgetStore) http.Handler {
	mux := http.NewServeMux()
	(&codequery.CodeHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j:   graph.FakeGraphReader{},
		Content: store,
	}).Mount(mux)
	(&contentread.ContentHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Content: store,
	}).Mount(mux)
	return mux
}

// TestSourceCacheToolsDefaultResponseStaysWithinBudget dispatches the three
// row-returning source tools with their default arguments over rows that carry
// an oversized source_cache, and requires the reply to fit the response budget
// with the clip reported on the response and on the clipped rows.
func TestSourceCacheToolsDefaultResponseStaysWithinBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tool string
		args map[string]any
		// rows is the number of rows the default limit must return.
		rows int
	}{
		{tool: "find_symbol", args: map[string]any{"symbol": "handleRequest", "repo_id": sourceCacheBudgetRepoID}, rows: 20},
		{tool: "inspect_code_inventory", args: map[string]any{"repo_id": sourceCacheBudgetRepoID, "inventory_kind": "entity", "entity_kind": "function"}, rows: 20},
		{tool: "search_entity_content", args: map[string]any{"query": "handleRequest", "repo_id": sourceCacheBudgetRepoID}, rows: 10},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			t.Parallel()

			result := requireDefaultResponseWithinBudget(t, tt.tool, sourceCacheBudgetMux(newSourceCacheBudgetStore()), tt.args)
			data, ok := result.Envelope.Data.(map[string]any)
			if !ok {
				t.Fatalf("%s data type = %T, want map[string]any", tt.tool, result.Envelope.Data)
			}
			rows, _ := data["results"].([]any)
			if len(rows) != tt.rows {
				t.Fatalf("%s returned %d rows, want %d default rows", tt.tool, len(rows), tt.rows)
			}
			if got := numberValue(data["source_cache_clip_bytes"]); got != 4096 {
				t.Fatalf("%s source_cache_clip_bytes = %v, want 4096", tt.tool, data["source_cache_clip_bytes"])
			}
			if got := numberValue(data["source_cache_clipped_rows"]); got != tt.rows {
				t.Fatalf("%s source_cache_clipped_rows = %v, want %d (every row is over the clip)", tt.tool, data["source_cache_clipped_rows"], tt.rows)
			}
			first, _ := rows[0].(map[string]any)
			if first["source_cache_clipped"] != true {
				t.Fatalf("%s first row source_cache_clipped = %v, want true", tt.tool, first["source_cache_clipped"])
			}
			if got := numberValue(first["source_cache_total_bytes"]); got != sourceCacheBudgetHugeLen {
				t.Fatalf("%s first row source_cache_total_bytes = %v, want %d", tt.tool, first["source_cache_total_bytes"], sourceCacheBudgetHugeLen)
			}
		})
	}
}

func numberValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	}
	return -1
}
