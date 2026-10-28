// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

type phpConfigStore struct {
	content.FakePortContentStore
	byName    map[string][]querycontract.EntityContent
	anyCalls  int
	lastName  string
	repoCalls []string
}

func (s *phpConfigStore) SearchEntitiesByExactNameAnyRepo(_ context.Context, _, name string, _ int) ([]querycontract.EntityContent, error) {
	s.anyCalls++
	return s.byName[name], nil
}

func (s *phpConfigStore) SearchEntitiesByExactName(_ context.Context, repoID, _, name string, _ int) ([]querycontract.EntityContent, error) {
	s.repoCalls = append(s.repoCalls, repoID)
	var rows []querycontract.EntityContent
	for _, row := range s.byName[name] {
		if row.RepoID == repoID {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (s *phpConfigStore) SearchPHPConfigEntities(_ context.Context, repoIDs []string, name string, after *querycontract.PHPConfigCursor, limit int) ([]querycontract.EntityContent, error) {
	s.lastName = name
	if repoIDs == nil {
		s.anyCalls++
	} else {
		s.repoCalls = append(s.repoCalls, repoIDs...)
	}
	rows := make([]querycontract.EntityContent, 0)
	for _, row := range s.byName[name] {
		if repoIDs != nil && !slices.Contains(repoIDs, row.RepoID) {
			continue
		}
		if after != nil {
			key := row.RepoID + "|" + row.RelativePath + "|" + fmt.Sprintf("%09d", row.StartLine) + "|" + row.EntityID
			cursorKey := after.RepoID + "|" + after.RelativePath + "|" + fmt.Sprintf("%09d", after.StartLine) + "|" + after.EntityID
			if key <= cursorKey {
				continue
			}
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b querycontract.EntityContent) int {
		ak := a.RepoID + "|" + a.RelativePath + "|" + fmt.Sprintf("%09d", a.StartLine) + "|" + a.EntityID
		bk := b.RepoID + "|" + b.RelativePath + "|" + fmt.Sprintf("%09d", b.StartLine) + "|" + b.EntityID
		return strings.Compare(ak, bk)
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func TestSearchPHPConfigByModeAndDirective(t *testing.T) {
	t.Parallel()
	store := &phpConfigStore{byName: map[string][]querycontract.EntityContent{
		"php_server_mode:mod_php":    {{RepoID: "repo-apache", RelativePath: "apache2/site.conf", EntityName: "php_server_mode:mod_php", StartLine: 3, SourceCache: "LoadModule php_module /secret", Metadata: map[string]any{"value": "mod_php"}}},
		"php_directive:memory_limit": {{RepoID: "repo-apache", RelativePath: "php.ini", EntityName: "php_directive:memory_limit", StartLine: 4, Metadata: map[string]any{"value": "256M"}}},
	}}
	handler := &ContentHandler{Content: store, Profile: querycontract.ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	for _, tc := range []struct{ body, wantRepo string }{{`{"mode":"mod_php"}`, "repo-apache"}, {`{"directive":"memory_limit"}`, "repo-apache"}} {
		req := httptest.NewRequest(http.MethodPost, "/api/v0/content/php-config/search", strings.NewReader(tc.body))
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var envelope struct {
			Data struct {
				Results []querycontract.EntityContent `json:"results"`
			} `json:"data"`
			Truth *querycontract.TruthEnvelope `json:"truth"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if len(envelope.Data.Results) != 1 || envelope.Data.Results[0].RepoID != tc.wantRepo || envelope.Data.Results[0].SourceCache != "" || strings.Contains(rec.Body.String(), "source_cache") || envelope.Truth == nil {
			t.Fatalf("response=%s", rec.Body.String())
		}
	}
}

func TestPHPConfigSearchScopesBeforeRead(t *testing.T) {
	t.Parallel()
	store := &phpConfigStore{byName: map[string][]querycontract.EntityContent{"php_server_mode:mod_php": {{RepoID: "repo-a", EntityName: "php_server_mode:mod_php"}, {RepoID: "repo-b", EntityName: "php_server_mode:mod_php"}}}}
	handler := &ContentHandler{Content: store}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/content/php-config/search", strings.NewReader(`{"mode":"mod_php"}`))
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{Mode: auth.AuthModeScoped, AllowedRepositoryIDs: []string{"repo-a"}}))
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	handler.Mount(mux)
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 || store.anyCalls != 0 || len(store.repoCalls) != 1 || store.repoCalls[0] != "repo-a" {
		t.Fatalf("status=%d broad=%d repos=%v body=%s", rec.Code, store.anyCalls, store.repoCalls, rec.Body.String())
	}
}

func TestPHPConfigSearchPaginatesAcrossRepositories(t *testing.T) {
	t.Parallel()
	store := &phpConfigStore{byName: map[string][]querycontract.EntityContent{
		"php_server_mode:mod_php": {
			{EntityID: "e-a", RepoID: "repo-a", RelativePath: "Dockerfile", StartLine: 1, EntityName: "php_server_mode:mod_php"},
			{EntityID: "e-b", RepoID: "repo-b", RelativePath: "apache2/site.conf", StartLine: 3, EntityName: "php_server_mode:mod_php"},
			{EntityID: "e-c", RepoID: "repo-c", RelativePath: "Dockerfile", StartLine: 1, EntityName: "php_server_mode:mod_php"},
		},
	}}
	handler := &ContentHandler{Content: store}
	mux := http.NewServeMux()
	handler.Mount(mux)
	fetch := func(body string) (string, bool, []querycontract.EntityContent) {
		req := httptest.NewRequest(http.MethodPost, "/api/v0/content/php-config/search", strings.NewReader(body))
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var response struct {
			Data struct {
				Results    []querycontract.EntityContent `json:"results"`
				Truncated  bool                          `json:"truncated"`
				NextCursor string                        `json:"next_cursor"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response.Data.NextCursor, response.Data.Truncated, response.Data.Results
	}
	cursor, more, first := fetch(`{"mode":"mod_php","limit":1}`)
	if !more || cursor == "" || len(first) != 1 || first[0].RepoID != "repo-a" {
		t.Fatalf("first=%#v cursor=%q more=%t calls=%d name=%s", first, cursor, more, store.anyCalls, store.lastName)
	}
	cursor, more, second := fetch(`{"mode":"mod_php","limit":1,"cursor":"` + cursor + `"}`)
	if !more || cursor == "" || len(second) != 1 || second[0].RepoID != "repo-b" {
		t.Fatalf("second=%#v cursor=%q more=%t", second, cursor, more)
	}
	cursor, more, third := fetch(`{"mode":"mod_php","limit":1,"cursor":"` + cursor + `"}`)
	if more || cursor != "" || len(third) != 1 || third[0].RepoID != "repo-c" {
		t.Fatalf("third=%#v cursor=%q more=%t", third, cursor, more)
	}
}

func TestPHPConfigCursorCannotChangeFilter(t *testing.T) {
	t.Parallel()
	store := &phpConfigStore{byName: map[string][]querycontract.EntityContent{
		"php_directive:memory_limit": {
			{EntityID: "e-a", RepoID: "repo-a", RelativePath: "php.ini", StartLine: 1},
			{EntityID: "e-b", RepoID: "repo-b", RelativePath: "php.ini", StartLine: 1},
		},
	}}
	mux := http.NewServeMux()
	(&ContentHandler{Content: store}).Mount(mux)
	first := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/content/php-config/search", strings.NewReader(`{"directive":"memory_limit","limit":1}`))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	mux.ServeHTTP(first, req)
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	var response struct {
		Data struct {
			NextCursor string `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.NextCursor == "" {
		t.Fatal("first page omitted next_cursor")
	}
	second := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v0/content/php-config/search", strings.NewReader(`{"directive":"session.upload_progress.enabled","cursor":"`+response.Data.NextCursor+`"}`))
	mux.ServeHTTP(second, req)
	if second.Code != http.StatusBadRequest {
		t.Fatalf("changed filter status=%d body=%s", second.Code, second.Body.String())
	}
}

func TestPHPConfigScopedPageFiltersBeforeLimit(t *testing.T) {
	t.Parallel()
	store := &phpConfigStore{byName: map[string][]querycontract.EntityContent{
		"php_server_mode:mod_php": {
			{EntityID: "e-a", RepoID: "repo-a", RelativePath: "Dockerfile", StartLine: 1},
			{EntityID: "e-b", RepoID: "repo-b", RelativePath: "Dockerfile", StartLine: 1},
			{EntityID: "e-c", RepoID: "repo-c", RelativePath: "Dockerfile", StartLine: 1},
		},
	}}
	mux := http.NewServeMux()
	(&ContentHandler{Content: store}).Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/content/php-config/search", strings.NewReader(`{"mode":"mod_php","limit":1}`))
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{Mode: auth.AuthModeScoped, AllowedRepositoryIDs: []string{"repo-b", "repo-c"}}))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || store.anyCalls != 0 || len(store.repoCalls) != 2 {
		t.Fatalf("status=%d broad=%d repos=%v body=%s", rec.Code, store.anyCalls, store.repoCalls, rec.Body.String())
	}
	var response struct {
		Data struct {
			Results    []querycontract.EntityContent `json:"results"`
			Truncated  bool                          `json:"truncated"`
			NextCursor string                        `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Results) != 1 || response.Data.Results[0].RepoID != "repo-b" || !response.Data.Truncated || response.Data.NextCursor == "" {
		t.Fatalf("scoped page=%s", rec.Body.String())
	}
}
