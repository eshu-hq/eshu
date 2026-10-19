// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// stageTimingContentStore delays every content read the entity-search seams
// issue, so the tests below can assert the pg stage brackets the store
// delay. It embeds the port so only the exercised reads need bodies.
type stageTimingContentStore struct {
	querycontract.ContentStore
	delay time.Duration
}

func (s stageTimingContentStore) SearchEntityContent(_ context.Context, _, _ string, _ int) ([]querycontract.EntityContent, error) {
	time.Sleep(s.delay)
	return nil, nil
}

func (s stageTimingContentStore) SearchEntitiesUnscoped(_ context.Context, _ string, _, _ int, _ querycontract.SearchCursor) (querycontract.EntitySearchPage, error) {
	time.Sleep(s.delay)
	return querycontract.EntitySearchPage{}, nil
}

// TestSearchEntitiesByScopeAttributesPGStage proves the scoped
// POST /api/v0/content/entities/search seam records its content read on
// the pg stage (issue #7733 item 2).
func TestSearchEntitiesByScopeAttributesPGStage(t *testing.T) {
	t.Parallel()
	ctx, stages := querycontract.WithRequestStages(context.Background())
	h := &ContentHandler{Content: stageTimingContentStore{delay: 100 * time.Millisecond}}
	req := contentSearchRequest{RepoID: "repo1", Query: "handler", Limit: 10}
	if _, _, err := h.searchEntitiesByScope(ctx, req); err != nil {
		t.Fatalf("searchEntitiesByScope error = %v", err)
	}
	if got := stages.PG(); got < 50*time.Millisecond {
		t.Fatalf("pg stage = %v, want >= 50ms (100ms injected search delay)", got)
	}
	if got := stages.Graph(); got != 0 {
		t.Fatalf("graph stage = %v, want 0 (no graph read on this route)", got)
	}
}

// TestSearchEntitiesUnscopedAttributesPGStage proves the unscoped
// POST /api/v0/content/entities/search seam records its bounded-walk read
// on the pg stage.
func TestSearchEntitiesUnscopedAttributesPGStage(t *testing.T) {
	t.Parallel()
	stageCtx, stages := querycontract.WithRequestStages(context.Background())
	store := stageTimingContentStore{delay: 100 * time.Millisecond}
	h := &ContentHandler{Content: store}
	searcher, ok := querycontract.ContentStore(store).(querycontract.UnscopedEntitySearcher)
	if !ok {
		t.Fatal("stageTimingContentStore does not implement UnscopedEntitySearcher")
	}
	req := contentSearchRequest{Query: "handler", Limit: 10}
	r := httptest.NewRequest(http.MethodPost, "/api/v0/content/entities/search", nil).WithContext(stageCtx)
	w := httptest.NewRecorder()
	h.searchEntitiesUnscoped(w, r, req, searcher)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if got := stages.PG(); got < 50*time.Millisecond {
		t.Fatalf("pg stage = %v, want >= 50ms (100ms injected unscoped-search delay)", got)
	}
}
