// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

func TestCodeSearchContentFiltersBeforeBoundedPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		rows       []EntityContent
		sourceOnly bool
	}{
		{
			name: "language",
			body: `{"query":"Handle","repo_id":"repo-a","language":"go","limit":1}`,
			rows: []EntityContent{
				{EntityID: "wrong-a", EntityName: "HandleOne", RepoID: "repo-a", Language: "java", RelativePath: "a.java", SourceCache: "Handle"},
				{EntityID: "wrong-b", EntityName: "HandleTwo", RepoID: "repo-a", Language: "java", RelativePath: "b.java", SourceCache: "Handle"},
				{EntityID: "wanted", EntityName: "HandleThree", RepoID: "repo-a", Language: "go", RelativePath: "c.go", SourceCache: "Handle"},
			},
		},
		{
			name:       "source language",
			body:       `{"query":"needle","repo_id":"repo-a","language":"go","limit":1}`,
			sourceOnly: true,
			rows: []EntityContent{
				{EntityID: "wrong-a", EntityName: "OtherOne", RepoID: "repo-a", Language: "java", RelativePath: "a.java", SourceCache: "needle"},
				{EntityID: "wrong-b", EntityName: "OtherTwo", RepoID: "repo-a", Language: "java", RelativePath: "b.java", SourceCache: "needle"},
				{EntityID: "wanted", EntityName: "OtherThree", RepoID: "repo-a", Language: "go", RelativePath: "c.go", SourceCache: "needle"},
			},
		},
		{
			name: "exact name",
			body: `{"query":"Handle","repo_id":"repo-a","exact":true,"limit":1}`,
			rows: []EntityContent{
				{EntityID: "wrong-a", EntityName: "HandleOne", RepoID: "repo-a", Language: "go", RelativePath: "a.go", SourceCache: "Handle"},
				{EntityID: "wrong-b", EntityName: "HandleTwo", RepoID: "repo-a", Language: "go", RelativePath: "b.go", SourceCache: "Handle"},
				{EntityID: "wanted", EntityName: "Handle", RepoID: "repo-a", Language: "go", RelativePath: "c.go", SourceCache: "Handle"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &recordingCodeSearchContentStore{byRepo: map[string][]EntityContent{"repo-a": tt.rows}}
			var content ContentStore = store
			if tt.sourceOnly {
				content = sourceOnlyCodeSearchStore{recordingCodeSearchContentStore: store}
			}
			handler := &CodeHandler{
				Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					return nil, nil
				}},
				Content: content,
				Profile: ProfileLocalAuthoritative,
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/search", bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()

			handler.handleSearch(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
			}
			results := decodeCodeSearchAuthzBody(t, rec)["results"].([]any)
			if len(results) != 1 || results[0].(map[string]any)["entity_id"] != "wanted" {
				t.Fatalf("results = %#v, want wanted entity after earlier nonmatching rows", results)
			}
		})
	}
}

type sourceOnlyCodeSearchStore struct {
	*recordingCodeSearchContentStore
}

func (s sourceOnlyCodeSearchStore) SearchEntitiesByName(context.Context, string, string, string, int) ([]EntityContent, error) {
	return nil, nil
}

func (s *recordingCodeSearchContentStore) SearchCodeCandidates(
	ctx context.Context,
	repoID, pattern, language string,
	limit int,
	exact bool,
) (nameMatches, sourceMatches []EntityContent, err error) {
	fixture := content.FakePortContentStore{Entities: s.byRepo[repoID]}
	return fixture.SearchCodeCandidates(ctx, repoID, pattern, language, limit, exact)
}
