// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestGlobalCodeSearchReportsExactAndOverflowPages(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		rows          []EntityContent
		wantTruncated bool
	}{
		{
			name: "exact limit",
			rows: []EntityContent{{EntityID: "entity-a", EntityName: "Server", EntityType: "Function"}},
		},
		{
			name: "over limit",
			rows: []EntityContent{
				{EntityID: "entity-a", EntityName: "Server", EntityType: "Function"},
				{EntityID: "entity-b", EntityName: "Server", EntityType: "Function"},
			},
			wantTruncated: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := &recordingEntityNameSearcher{Rows: tc.rows}
			handler := &CodeHandler{Content: content, Profile: ProfileLocalAuthoritative}
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v0/code/search",
				bytes.NewBufferString(`{"query":"Server","exact":true,"limit":1}`),
			)
			recorder := httptest.NewRecorder()

			handler.handleSearch(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
			}
			if len(content.Searches) != 1 || content.Searches[0].Limit != 2 {
				t.Fatalf("content searches = %#v, want one limit+1 probe", content.Searches)
			}
			var response map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			results, _ := response["results"].([]any)
			matches, _ := response["matches"].([]any)
			if len(results) != 1 || len(matches) != 1 {
				t.Fatalf("results/matches lengths = %d/%d, want 1/1", len(results), len(matches))
			}
			if response["count"] != float64(1) || response["limit"] != float64(1) ||
				response["truncated"] != tc.wantTruncated {
				t.Fatalf("page metadata = count:%#v limit:%#v truncated:%#v", response["count"], response["limit"], response["truncated"])
			}
		})
	}
}

func TestGlobalCodeSearchMaximumPublicLimitUsesOneRowProbe(t *testing.T) {
	t.Parallel()

	content := &recordingEntityNameSearcher{}
	handler := &CodeHandler{Content: content}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/search",
		bytes.NewBufferString(`{"query":"Server","exact":true,"limit":999}`),
	)
	recorder := httptest.NewRecorder()

	handler.handleSearch(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if len(content.Searches) != 1 || content.Searches[0].Limit != querycontract.EntityNameSearchMaxLimit+1 {
		t.Fatalf("content searches = %#v, want internal limit %d", content.Searches, querycontract.EntityNameSearchMaxLimit+1)
	}
}

// recordingEntityNameSearcher records the entity-name searches a test's
// handler performs and answers them from canned rows. Twin of the same-named
// fake in package query (entity_name_search_test.go): a _test.go symbol is
// not importable across the package boundary (#6060).
type recordingEntityNameSearcher struct {
	querytestutil.FakePortContentStore
	Searches []querycontract.EntityNameSearch
	Rows     []querycontract.EntityContent
}

func (s *recordingEntityNameSearcher) SearchEntityNames(_ context.Context, search querycontract.EntityNameSearch) ([]querycontract.EntityContent, error) {
	s.Searches = append(s.Searches, search)
	return append([]querycontract.EntityContent(nil), s.Rows...), nil
}
