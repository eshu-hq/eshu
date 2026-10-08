// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// limitHonoringNameSearcher answers entity-name searches from canned rows and
// honors the requested Limit the way the Postgres store does, so an offset
// test sees the real window the handler asks for.
type limitHonoringNameSearcher struct {
	content.FakePortContentStore
	Searches []entity.EntityNameSearch
	Rows     []querycontract.EntityContent
}

func (s *limitHonoringNameSearcher) SearchEntityNames(_ context.Context, search entity.EntityNameSearch) ([]querycontract.EntityContent, error) {
	s.Searches = append(s.Searches, search)
	limit := search.Limit
	if limit > len(s.Rows) {
		limit = len(s.Rows)
	}
	return append([]querycontract.EntityContent(nil), s.Rows[:limit]...), nil
}

func numberedEntityRows(count int) []querycontract.EntityContent {
	rows := make([]querycontract.EntityContent, 0, count)
	for index := 0; index < count; index++ {
		rows = append(rows, querycontract.EntityContent{
			EntityID: fmt.Sprintf("entity-%03d", index), EntityName: "decode", EntityType: "Function",
		})
	}
	return rows
}

func searchWithBody(t *testing.T, handler *CodeHandler, body string) (int, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v0/code/search", bytes.NewBufferString(body))
	recorder := httptest.NewRecorder()
	handler.handleSearch(recorder, request)
	var response map[string]any
	if recorder.Code == http.StatusOK {
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
		}
		response = envelope.Data
		if response == nil {
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
		}
	}
	return recorder.Code, response
}

func resultIDs(t *testing.T, response map[string]any) []string {
	t.Helper()
	rows, _ := response["results"].([]any)
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.(map[string]any)["entity_id"].(string))
	}
	return ids
}

// TestGlobalCodeSearchOffsetReturnsTheNextWindow proves find_code can page: an
// offset skips rows inside the ranked window, the store is asked for
// offset+limit+1 rows, and the page reports its offset and whether rows remain.
func TestGlobalCodeSearchOffsetReturnsTheNextWindow(t *testing.T) {
	t.Parallel()

	store := &limitHonoringNameSearcher{Rows: numberedEntityRows(10)}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}

	status, response := searchWithBody(t, handler, `{"query":"decode","exact":true,"limit":3,"offset":4}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got, want := fmt.Sprint(resultIDs(t, response)), "[entity-004 entity-005 entity-006]"; got != want {
		t.Fatalf("page ids = %s, want %s", got, want)
	}
	if len(store.Searches) != 1 || store.Searches[0].Limit != 8 {
		t.Fatalf("store searches = %#v, want one probe of offset+limit+1 = 8", store.Searches)
	}
	if response["offset"] != float64(4) || response["limit"] != float64(3) || response["count"] != float64(3) || response["truncated"] != true {
		t.Fatalf("page metadata = offset:%v limit:%v count:%v truncated:%v", response["offset"], response["limit"], response["count"], response["truncated"])
	}

	last := &limitHonoringNameSearcher{Rows: numberedEntityRows(10)}
	_, tail := searchWithBody(t, &CodeHandler{Content: last, Profile: ProfileLocalAuthoritative}, `{"query":"decode","exact":true,"limit":3,"offset":8}`)
	if got, want := fmt.Sprint(resultIDs(t, tail)), "[entity-008 entity-009]"; got != want {
		t.Fatalf("tail ids = %s, want %s", got, want)
	}
	if tail["truncated"] != false {
		t.Fatalf("tail truncated = %v, want false", tail["truncated"])
	}
}

// TestGlobalCodeSearchOffsetStaysInsideTheRankedWindow proves the page never
// asks the store for more than the 200-row ranked window: the effective limit
// shrinks so offset+limit stays within it, and an offset at or past the window
// is a 400.
func TestGlobalCodeSearchOffsetStaysInsideTheRankedWindow(t *testing.T) {
	t.Parallel()

	store := &limitHonoringNameSearcher{Rows: numberedEntityRows(260)}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}

	status, response := searchWithBody(t, handler, `{"query":"decode","exact":true,"limit":200,"offset":60}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := len(resultIDs(t, response)); got != 140 {
		t.Fatalf("rows = %d, want 140 (the rest of the 200-row window)", got)
	}
	if len(store.Searches) != 1 || store.Searches[0].Limit != entity.EntityNameSearchProbeLimit {
		t.Fatalf("store searches = %#v, want one probe of %d", store.Searches, entity.EntityNameSearchProbeLimit)
	}
	if response["limit"] != float64(140) || response["truncated"] != true {
		t.Fatalf("limit/truncated = %v/%v, want 140/true (rows exist past the window)", response["limit"], response["truncated"])
	}

	status, _ = searchWithBody(t, handler, `{"query":"decode","exact":true,"limit":10,"offset":200}`)
	if status != http.StatusBadRequest {
		t.Fatalf("offset 200 status = %d, want 400", status)
	}
}

// TestGlobalCodeSearchWithoutOffsetKeepsItsResponseShape proves a request that
// carries no offset gets the same payload keys as before the offset existed.
func TestGlobalCodeSearchWithoutOffsetKeepsItsResponseShape(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: &limitHonoringNameSearcher{Rows: numberedEntityRows(3)}, Profile: ProfileLocalAuthoritative}
	_, response := searchWithBody(t, handler, `{"query":"decode","exact":true,"limit":2}`)
	if _, present := response["offset"]; present {
		t.Fatalf("response carries offset %v without a requested offset", response["offset"])
	}
}
